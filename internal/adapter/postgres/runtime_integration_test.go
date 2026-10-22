package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/redisstate"
	a "drama-agent/internal/application/accounts"
	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/task"
	"github.com/redis/go-redis/v9"
)

func runtimeFixture(t *testing.T) (*Store, *redisstate.Store, appconfig.Redis) {
	t.Helper()
	database, address := os.Getenv("DRAMA_TEST_DATABASE_URL"), os.Getenv("DRAMA_TEST_REDIS_ADDRESS")
	if database == "" || address == "" {
		t.Skip("isolated PG and Redis required")
	}
	ctx := context.Background()
	s, err := Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	settings := appconfig.Defaults().Redis
	settings.Address = address
	settings.Namespace = "drama:test:" + identity.New("projection")
	r, err := redisstate.Open(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	s.UseRuntime(r)
	t.Cleanup(r.Close)
	t.Cleanup(func() {
		client := redis.NewClient(&redis.Options{Addr: address})
		defer client.Close()
		var cursor uint64
		for {
			keys, next, err := client.Scan(ctx, cursor, "{"+settings.Namespace+"}:*", 100).Result()
			if err != nil {
				return
			}
			if len(keys) > 0 {
				_ = client.Del(ctx, keys...).Err()
			}
			cursor = next
			if cursor == 0 {
				return
			}
		}
	})
	return s, r, settings
}

func TestRuntimeOutboxSurvivesFailureAndRebuildsSchedule(t *testing.T) {
	s, r, settings := runtimeFixture(t)
	ctx := context.Background()
	tasks := NewTaskStore(s)
	id := identity.New("runtime_task")
	owner := fixtureSpace(t, s, "")
	task, err := tasks.CreateTask(ctx, d.Task{ID: id, SpaceID: owner, OwnerID: owner, CreatorID: owner, ScopeID: "runtime-test", CommandID: id, Status: d.Queued, State: d.State{Operation: "image.generate", Binding: d.Binding{Free: true}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.tasks WHERE id=$1`, id) })
	r.Close()
	if err = s.FlushRuntimeEvents(ctx); err == nil {
		t.Fatal("Redis outage falsely acknowledged delivery")
	}
	var pending bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.runtime_events WHERE key=$1)`, "task:"+id).Scan(&pending); err != nil || !pending {
		t.Fatal("committed task lost repair work", pending, err)
	}
	r, err = redisstate.Open(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s.UseRuntime(r)
	if err = s.FlushRuntimeEvents(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.EnsureTaskSchedule(ctx); err != nil {
		t.Fatal(err)
	}
	contains := func(rows []ports.TaskDue) bool {
		for _, row := range rows {
			if row.ID == id {
				return true
			}
		}
		return false
	}
	rows, err := tasks.DueTasks(ctx, nil, 1000)
	if err != nil || !contains(rows) {
		t.Fatal("accepted task not scheduled", err)
	}
	// Delete only this fixture's exact namespace, including its rebuild marker.
	client := redis.NewClient(&redis.Options{Addr: settings.Address})
	defer client.Close()
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, "{"+settings.Namespace+"}:*", 100).Result()
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) > 0 {
			if err = client.Del(ctx, keys...).Err(); err != nil {
				t.Fatal(err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	rows, err = tasks.DueTasks(ctx, nil, 1000)
	if err != nil || !contains(rows) {
		t.Fatal("cleared index did not rebuild durable task", err)
	}
	claimed, ok, err := tasks.ClaimTask(ctx, task.ID, "runtime-claim", time.Minute)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err = s.FlushRuntimeEvents(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err = tasks.DueTasks(ctx, nil, 1000)
	if err != nil || contains(rows) {
		t.Fatal("live PG lease remained due", err)
	}
	claimed.NextRunAt = nil
	claimed.Status = d.Cancelled
	if _, err = tasks.CommitTask(ctx, claimed, claimed.Revision, claimed.LeaseToken, true); err != nil {
		t.Fatal(err)
	}
	if err = s.FlushRuntimeEvents(ctx); err != nil {
		t.Fatal(err)
	}
	// With a ready index, an ordinary due scan has no PG read dependency.
	s.pool.Close()
	rows, err = tasks.DueTasks(ctx, nil, 1000)
	if err != nil || contains(rows) {
		t.Fatal("due scan depended on PG or resurrected terminal task", err)
	}
}

func TestRuntimeSessionAndReadCacheHitsDoNotQueryPG(t *testing.T) {
	s, r, _ := runtimeFixture(t)
	ctx := context.Background()
	id := identity.New("runtime_user")
	if _, err := s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,'runtime','user',$3)`, id, id+"@example.invalid", []byte("unused")); err != nil {
		t.Fatal(err)
	}
	secret := a.RandomToken()
	if _, err := r.PutSession(ctx, id, a.Workspace, a.Digest("workspace:"+secret), ports.RoleUser, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	initialUnread, err := s.NotificationUnread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	models := &modelconfig.Service{Store: s, Cache: r}
	if _, err := models.UserOptions(ctx); err != nil {
		t.Fatal(err)
	} // Projects is nil: public options must not scan references.
	email := id + "@example.invalid"
	for i := 0; i < 10; i++ {
		if _, err := r.Allow(ctx, "login:"+email, 10, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Allow(ctx, "email-minute:"+email+":register", 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	s.pool.Close()
	if actor, err := s.Authenticate(ctx, id+".workspace."+secret); err != nil || actor.UserID != id {
		t.Fatal("authentication read PG", err)
	}
	if count, err := s.NotificationUnread(ctx, id); err != nil || count != initialUnread {
		t.Fatal("cached unread read PG", count, err)
	}
	if _, err := models.Options(ctx); err != nil {
		t.Fatal("cached model options read PG", err)
	}
	if _, err := models.UserOptions(ctx); err != nil {
		t.Fatal("public candidates read PG or project references", err)
	}
	_, err = s.Login(ctx, a.LoginCredentials{Email: email, Audience: a.Workspace})
	var rejected *fault.Error
	if !errors.As(err, &rejected) || rejected.Code != "AUTH_RATE_LIMITED" {
		t.Fatal("login throttle depended on PG", err)
	}
	err = s.PutVerification(ctx, "unused", email, "register", "unused", time.Now().Add(time.Minute))
	if !errors.As(err, &rejected) || rejected.Code != "AUTH_RATE_LIMITED" {
		t.Fatal("verification throttle depended on PG", err)
	}
}
