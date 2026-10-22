package redisstate

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/identity"
)

func projectEditorFixture(t *testing.T) *Store {
	t.Helper()
	address := os.Getenv("DRAMA_TEST_REDIS_ADDRESS")
	if address == "" {
		t.Skip("isolated project editing Redis opt-in")
	}
	if address != "127.0.0.1:16381" {
		t.Fatal("project editing test requires isolated Redis at 127.0.0.1:16381")
	}
	namespace := os.Getenv("DRAMA_TEST_REDIS_NAMESPACE")
	if namespace == "" {
		namespace = "drama:team-space-test:editing"
	}
	if !strings.HasPrefix(namespace, "drama:team-space-test:") {
		t.Fatal("isolated team-space test namespace required")
	}
	settings := appconfig.Defaults().Redis
	settings.Address = address
	settings.Namespace = namespace + ":" + identity.New("editing")
	s, err := Open(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		var cursor uint64
		for {
			keys, next, err := s.client.Scan(ctx, cursor, s.prefix+"*", 100).Result()
			if err != nil {
				break
			}
			if len(keys) > 0 {
				_ = s.client.Del(ctx, keys...).Err()
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		s.Close()
	})
	return s
}
func TestProjectEditingAtomicAcquireAndSessionOwnership(t *testing.T) {
	s := projectEditorFixture(t)
	other := second(t, s)
	ctx := context.Background()
	owners := make(chan ports.ProjectEditor, 24)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			node := s
			if i%2 == 1 {
				node = other
			}
			editor, err := node.AcquireProjectEditing(ctx, "project", string(rune('a'+i)), "session")
			if err != nil {
				t.Error(err)
				return
			}
			owners <- editor
		}(i)
	}
	wg.Wait()
	close(owners)
	winner := ""
	for owner := range owners {
		if winner == "" {
			winner = owner.UserID
		}
		if owner.UserID != winner {
			t.Fatal("multiple simultaneous editors", winner, owner)
		}
	}
	if winner == "" {
		t.Fatal("no editor")
	}
	ttl, err := s.client.PTTL(ctx, s.key("project-editor", "project")).Result()
	if err != nil || ttl > 90*time.Second || ttl < 85*time.Second {
		t.Fatal("wrong editing TTL", ttl, err)
	}
	if err = other.CheckProjectEditing(ctx, "project", winner, "session"); err != nil {
		t.Fatal(err)
	}
	if err = other.CheckProjectEditing(ctx, "project", winner, "another-session"); code(err) != "PROJECT_EDITING_BY_OTHER" {
		t.Fatal(err)
	}
	before, err := other.RenewProjectEditing(ctx, "project", winner, "another-session")
	if err != nil || before.UserID != winner || before.SessionID != "session" {
		t.Fatal(before, err)
	}
	if err = other.ReleaseProjectEditing(ctx, "project", winner, "another-session"); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckProjectEditing(ctx, "project", winner, "session"); err != nil {
		t.Fatal("other session released owner", err)
	}
	first, err := s.AcquireProjectEditing(ctx, "project", winner, "session")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.client.PExpire(ctx, s.key("project-editor", "project"), time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	renewed, err := other.RenewProjectEditing(ctx, "project", winner, "session")
	if err != nil || renewed.ExpiresAt.Before(first.ExpiresAt.Add(-time.Second)) {
		t.Fatal("owner could not renew", renewed, err)
	}
	if err = s.ReleaseProjectEditing(ctx, "project", winner, "session"); err != nil {
		t.Fatal(err)
	}
	if err = other.CheckProjectEditing(ctx, "project", winner, "session"); code(err) != "PROJECT_EDITING_BY_OTHER" {
		t.Fatal("released owner still writes", err)
	}
}
func TestProjectEditingExpiryDoesNotLetOldSessionRenewOrReleaseNewOwner(t *testing.T) {
	s := projectEditorFixture(t)
	other := second(t, s)
	ctx := context.Background()
	if _, err := s.AcquireProjectEditing(ctx, "project", "old", "old-session"); err != nil {
		t.Fatal(err)
	}
	if err := s.client.PExpire(ctx, s.key("project-editor", "project"), 20*time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(35 * time.Millisecond)
	owner, err := s.RenewProjectEditing(ctx, "project", "old", "old-session")
	if err != nil || owner.UserID != "" {
		t.Fatal("renew reacquired expired editor", owner, err)
	}
	if _, err = other.AcquireProjectEditing(ctx, "project", "new", "new-session"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReleaseProjectEditing(ctx, "project", "old", "old-session"); err != nil {
		t.Fatal(err)
	}
	if err = other.CheckProjectEditing(ctx, "project", "new", "new-session"); err != nil {
		t.Fatal("old release removed new editor", err)
	}
	if err = s.CheckProjectEditing(ctx, "other-project", "new", "new-session"); code(err) != "PROJECT_EDITING_BY_OTHER" {
		t.Fatal("editor session crossed project", err)
	}
	_ = s.client.Close()
	if _, err = s.AcquireProjectEditing(ctx, "project", "new", "new-session"); code(err) != "RUNTIME_UNAVAILABLE" {
		t.Fatal("Redis fault permitted acquire", err)
	}
	if _, err = s.RenewProjectEditing(ctx, "project", "new", "new-session"); code(err) != "RUNTIME_UNAVAILABLE" {
		t.Fatal("Redis fault permitted renew", err)
	}
	if err = s.CheckProjectEditing(ctx, "project", "new", "new-session"); code(err) != "RUNTIME_UNAVAILABLE" {
		t.Fatal("Redis fault permitted editing", err)
	}
	if err = s.ReleaseProjectEditing(ctx, "project", "new", "new-session"); code(err) != "RUNTIME_UNAVAILABLE" {
		t.Fatal("Redis fault hid failed release", err)
	}
}
