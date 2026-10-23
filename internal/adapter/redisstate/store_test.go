package redisstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	a "drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	project "drama-agent/internal/domain/project"
	d "drama-agent/internal/domain/task"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	address := os.Getenv("DRAMA_TEST_REDIS_ADDRESS")
	if address == "" {
		t.Skip("isolated Redis opt-in")
	}
	settings := appconfig.Defaults().Redis
	settings.Address = address
	settings.Namespace = "drama:test:" + identity.New("runtime")
	settings.Lease = 3 * time.Second
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
func second(t *testing.T, s *Store) *Store {
	t.Helper()
	other, err := Open(context.Background(), s.settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	return other
}
func code(err error) string {
	var f *fault.Error
	if errors.As(err, &f) {
		return f.Code
	}
	return ""
}

func TestSessionsAcrossNodesReplaceRevokeAndExpire(t *testing.T) {
	s := fixture(t)
	other := second(t, s)
	ctx := context.Background()
	id := "user"
	secret := a.RandomToken()
	newSecret := a.RandomToken()
	until := time.Now().Add(time.Hour)
	for _, audience := range []string{a.Workspace, a.Operations} {
		if _, err := s.PutSession(ctx, id, audience, a.Digest(audience+":"+secret), ports.RoleOperator, until); err != nil {
			t.Fatal(err)
		}
	}
	old := id + ".workspace." + secret
	if actor, err := other.Authenticate(ctx, old); err != nil || actor.UserID != id {
		t.Fatal(actor, err)
	}
	if replaced, err := other.PutSession(ctx, id, a.Workspace, a.Digest("workspace:"+newSecret), ports.RoleOperator, until); err != nil || !replaced {
		t.Fatal(replaced, err)
	}
	if _, err := s.Authenticate(ctx, old); code(err) != "SESSION_REPLACED" {
		t.Fatal(err)
	}
	if deleted, err := s.DeleteSession(ctx, old); err != nil || deleted {
		t.Fatal("old logout revoked replacement", err)
	}
	current := id + ".workspace." + newSecret
	if _, err := s.Authenticate(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, id+".operations."+secret); err != nil {
		t.Fatal("other audience lost", err)
	}
	if _, err := s.DeleteUserSessions(ctx, id); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{current, id + ".operations." + secret} {
		if _, err := other.Authenticate(ctx, token); code(err) != "SESSION_REVOKED" {
			t.Fatal(err)
		}
	}
	if _, err := s.PutSession(ctx, id, a.Workspace, a.Digest("workspace:"+secret), ports.RoleUser, time.Now().Add(30*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := other.Authenticate(ctx, old); code(err) != "SESSION_EXPIRED" {
		t.Fatal(err)
	}
	if err := s.SetSessionsDisabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Authenticate(ctx, old); code(err) != "ACCOUNT_DISABLED" {
		t.Fatal(err)
	}
}

func TestRateLimitAtomicAcrossNodesAndDoesNotExtendWindow(t *testing.T) {
	s := fixture(t)
	other := second(t, s)
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			node := s
			if i%2 == 1 {
				node = other
			}
			ok, err := node.Allow(context.Background(), "login:one", 10, 200*time.Millisecond)
			if err != nil {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 10 {
		t.Fatal("quota admitted", accepted.Load())
	}
	time.Sleep(220 * time.Millisecond)
	if ok, err := s.Allow(context.Background(), "login:one", 10, time.Second); err != nil || !ok {
		t.Fatal("window did not reset", err)
	}
}

func TestLeaseRenewLossAndOwnerConditionalRelease(t *testing.T) {
	s := fixture(t)
	other := second(t, s)
	ctx := context.Background()
	lease, ok, err := s.Lock(ctx, "project", false)
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer lease.Release()
	time.Sleep(3300 * time.Millisecond)
	if _, ok, err := other.Lock(ctx, "project", false); err != nil || ok {
		t.Fatal("renewable lease expired", err)
	}
	if err := s.client.Set(ctx, s.key("lock", "project"), "new-owner", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Context.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("ownership loss did not cancel execution")
	}
	if lease.Check(ctx) == nil {
		t.Fatal("stale owner retained authority")
	}
	lease.Release()
	if owner, err := s.client.Get(ctx, s.key("lock", "project")).Result(); err != nil || owner != "new-owner" {
		t.Fatal("old release removed newer owner", owner, err)
	}
}

func TestVendorCapacitySharesCredentialAcrossConnections(t *testing.T) {
	s := fixture(t)
	other := second(t, s)
	ctx := context.Background()
	binding := d.Binding{Protocol: "provider", Endpoint: "https://example.invalid/v1", Credential: []byte("test-secret"), ConnectionID: "first"}
	lease, ok, err := s.AcquireTaskCapacity(ctx, binding, "submit", 1)
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer lease.Release()
	binding.ConnectionID = "second"
	if _, ok, err := other.AcquireTaskCapacity(ctx, binding, "submit", 1); err != nil || ok {
		t.Fatal("connection alias bypassed account quota", err)
	}
	lease.Release()
	next, ok, err := other.AcquireTaskCapacity(ctx, binding, "submit", 1)
	if err != nil || !ok {
		t.Fatal(err)
	}
	next.Release()
}

func TestCacheInvalidationFencesInflightRefillAndCoalesces(t *testing.T) {
	s := fixture(t)
	other := second(t, s)
	ctx := context.Background()
	started, finish := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := s.ReadCached(ctx, "notifications:one", time.Minute, func(context.Context) ([]byte, error) { close(started); <-finish; return []byte("old"), nil })
		done <- err
	}()
	<-started
	if err := other.Invalidate(ctx, "notifications"); err != nil {
		t.Fatal(err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var loads atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, err := other.ReadCached(ctx, "notifications:one", time.Minute, func(context.Context) ([]byte, error) { loads.Add(1); return []byte("new"), nil })
			if err != nil || string(raw) != "new" {
				t.Error("stale refill adopted", string(raw), err)
			}
		}()
	}
	wg.Wait()
	if loads.Load() != 1 {
		t.Fatal("cache stampede", loads.Load())
	}
	if err := s.Invalidate(ctx, "notifications"); err != nil {
		t.Fatal(err)
	}
	raw, err := other.ReadCached(ctx, "notifications:one", time.Minute, func(context.Context) ([]byte, error) { return []byte("newer"), nil })
	if err != nil || string(raw) != "newer" {
		t.Fatal("family invalidation missed cache hit", string(raw), err)
	}
}

func TestPubSubSharedAcrossNodesAndCloseReconciles(t *testing.T) {
	s := fixture(t)
	other := second(t, s)
	ctx := context.Background()
	updates, closeWatch, err := s.Watch(ctx, "notifications:one")
	if err != nil {
		t.Fatal(err)
	}
	defer closeWatch()
	if err := other.Publish(ctx, "notifications:*"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("cross-node broadcast missing")
	}
	s.Close()
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("closed runtime kept watch alive")
		}
	case <-time.After(time.Second):
		t.Fatal("closed runtime kept watch alive")
	}
}

func TestDueIndexCursorAndOldVersionCannotResurrectTerminal(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	at := time.Now().Add(-time.Second).Truncate(time.Microsecond)
	for _, id := range []string{"a", "b", "c"} {
		if err := s.ScheduleTask(ctx, id, 1, &at); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.ScheduledTasks(ctx, nil, 2)
	if err != nil || len(rows) != 2 || rows[0].ID != "a" || rows[1].ID != "b" {
		t.Fatal(rows, err)
	}
	rows, err = s.ScheduledTasks(ctx, &ports.TaskCursor{At: rows[1].At, ID: rows[1].ID}, 2)
	if err != nil || len(rows) != 1 || rows[0].ID != "c" {
		t.Fatal(rows, err)
	}
	if err := s.ScheduleTask(ctx, "b", 3, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ScheduleTask(ctx, "b", 2, &at); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ScheduledTasks(ctx, nil, 10)
	if err != nil || len(rows) != 2 {
		t.Fatal("stale projection resurrected task", rows, err)
	}
}

func TestRedisFailureRejectsAuthenticationAndNewCapacity(t *testing.T) {
	s := fixture(t)
	_ = s.client.Close()
	ctx := context.Background()
	if _, err := s.Authenticate(ctx, "u.workspace."+a.RandomToken()); code(err) != "RUNTIME_UNAVAILABLE" {
		t.Fatal(err)
	}
	if ok, err := s.Allow(ctx, "request", 1, time.Minute); ok || code(err) != "RUNTIME_UNAVAILABLE" {
		t.Fatal(ok, err)
	}
	if _, ok, err := s.AcquireTaskCapacity(ctx, d.Binding{}, "submit", 1); ok || code(err) != "RUNTIME_UNAVAILABLE" {
		t.Fatal(ok, err)
	}
}

type countingModel struct{ calls atomic.Int64 }

func (m *countingModel) ContainsToolReceipt(json.RawMessage, project.Action, project.Call) bool {
	return false
}
func (m *countingModel) Capacity() (int, int) { return 1000, 100 }
func (m *countingModel) Generate(context.Context, ports.Request, func(json.RawMessage) error) (ports.Response, error) {
	m.calls.Add(1)
	return ports.Response{}, nil
}

func TestCapacityFailureCarriesKnownNotSentEvidence(t *testing.T) {
	s := fixture(t)
	_ = s.client.Close()
	model := &countingModel{}
	_, err := s.LimitModel(model, "provider", "https://example.invalid", "test-secret").Generate(context.Background(), ports.Request{}, nil)
	var unsent interface{ RequestNotSent() bool }
	if err == nil || !errors.As(err, &unsent) || !unsent.RequestNotSent() || model.calls.Load() != 0 {
		t.Fatal("capacity rejection became unknown provider submission", err, model.calls.Load())
	}
}

func TestRedisRestartKeepsSessionsOnlyWithDurableRevocations(t *testing.T) {
	container := os.Getenv("DRAMA_TEST_REDIS_RESTART_CONTAINER")
	if container == "" {
		t.Skip("explicit isolated Redis restart opt-in")
	}
	if !strings.HasPrefix(container, "drama-redis-migration-test-") {
		t.Fatal("restart target is not this migration's isolated Redis")
	}
	label, err := exec.Command("docker", "inspect", "--format", `{{index .Config.Labels "drama-agent.component"}}`, container).Output()
	if err != nil || strings.TrimSpace(string(label)) != "redis-migration-test" {
		t.Fatal("unverified Redis restart target", err)
	}
	s := fixture(t)
	ctx := context.Background()
	login := func(user string) string {
		secret := a.RandomToken()
		if _, err := s.PutSession(ctx, user, a.Workspace, a.Digest("workspace:"+secret), ports.RoleUser, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		return user + ".workspace." + secret
	}
	kept, loggedOut, disabled := login("restart-kept"), login("restart-logout"), login("restart-disabled")
	if deleted, err := s.DeleteSession(ctx, loggedOut); err != nil || !deleted {
		t.Fatal("logout", deleted, err)
	}
	if err = s.SetSessionsDisabled(ctx, "restart-disabled", true); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkScheduleReady(ctx); err != nil {
		t.Fatal(err)
	}
	durable := s.durable.Load()
	if output, err := exec.Command("docker", "restart", container).CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err = s.Authenticate(ctx, kept)
		if code(err) != "RUNTIME_UNAVAILABLE" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Redis did not reconnect", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if durable {
		// AOF keeps the login; fsynced revocations stay revoked across the restart.
		if err != nil {
			t.Fatal("durable session lost on Redis restart", err)
		}
		if _, err = s.Authenticate(ctx, loggedOut); code(err) != "SESSION_REVOKED" {
			t.Fatal("logged-out session revived", err)
		}
		if _, err = s.Authenticate(ctx, disabled); code(err) != "ACCOUNT_DISABLED" {
			t.Fatal("disabled account revived", err)
		}
	} else if code(err) != "SESSION_REVOKED" {
		t.Fatal("without AOF a persisted session must not survive a Redis restart", err)
	}
	if _, err = s.Authenticate(ctx, login("restart-new")); err != nil {
		t.Fatal("new login after restart rejected", err)
	}
	if ready, err := s.ScheduleReady(ctx); err != nil || ready {
		t.Fatal("AOF marker suppressed rebuild after Redis restart", ready, err)
	}
}
