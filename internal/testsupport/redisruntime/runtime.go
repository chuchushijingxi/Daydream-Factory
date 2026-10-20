// Package redisruntime connects free fixtures to an explicitly isolated Redis.
package redisruntime

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/redisstate"
	"drama-agent/internal/application/ports"
)

func Connect(t testing.TB) *redisstate.Store {
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" && os.Getenv("DRAMA_TEST_CONFIG_FILE") != "" {
		if cfg, err := appconfig.Load(os.Getenv("DRAMA_TEST_CONFIG_FILE")); err == nil {
			database = cfg.Database.URL
		}
	}
	return connectURL(t, database)
}
func connectURL(t testing.TB, database string) *redisstate.Store {
	t.Helper()
	address := os.Getenv("DRAMA_TEST_REDIS_ADDRESS")
	if address == "" {
		t.Skip("isolated Redis opt-in required")
	}
	settings := appconfig.Defaults().Redis
	// Free Mock workloads test task concurrency independently of paid-provider RPM.
	settings.ProviderRequestsPerMinute = 1000000
	settings.Lease = 3 * time.Second
	settings.Address = address
	settings.Namespace = os.Getenv("DRAMA_TEST_REDIS_NAMESPACE")
	if settings.Namespace == "" {
		settings.Namespace = "drama:runtime-test"
	}
	if !strings.HasPrefix(settings.Namespace, "drama:") || !strings.Contains(settings.Namespace, "test") {
		t.Fatal("Redis fixture requires a drama test namespace")
	}
	if parsed, err := url.Parse(database); err == nil && parsed.Path != "" {
		settings.Namespace += ":" + strings.TrimPrefix(parsed.Path, "/")
	}
	s, err := redisstate.Open(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func ConnectDatabase(t testing.TB, database string) *redisstate.Store { return connectURL(t, database) }

func Attach(t testing.TB, store interface{ UseRuntime(ports.RuntimeState) }) *redisstate.Store {
	return AttachURL(t, store, os.Getenv("DRAMA_TEST_DATABASE_URL"))
}
func AttachURL(t testing.TB, store interface{ UseRuntime(ports.RuntimeState) }, database string) *redisstate.Store {
	t.Helper()
	s := connectURL(t, database)
	store.UseRuntime(s)
	if projector, ok := store.(interface{ RunRuntimeEvents(context.Context) }); ok {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); projector.RunRuntimeEvents(ctx) }()
		t.Cleanup(func() { cancel(); <-done })
	}
	return s
}

func SeedDigest(t testing.TB, id, audience, digest string, role ports.Role, until time.Time) {
	t.Helper()
	if _, err := Connect(t).PutSession(context.Background(), id, audience, digest, role, until); err != nil {
		t.Fatal(err)
	}
}
