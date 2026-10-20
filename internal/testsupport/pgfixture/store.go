package pgfixture

import (
	"context"
	"testing"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/testsupport/redisruntime"
)

func Open(t testing.TB, ctx context.Context, url string, settings ...appconfig.Database) (*postgres.Store, error) {
	t.Helper()
	store, err := postgres.Open(ctx, url, settings...)
	if err == nil {
		redisruntime.AttachURL(t, store, url)
	}
	return store, err
}
