package postgres

import (
	"context"
	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/testsupport/redisruntime"
	"testing"
)

func openRuntimePG(t *testing.T, ctx context.Context, url string, settings ...appconfig.Database) (*Store, error) {
	t.Helper()
	store, err := Open(ctx, url, settings...)
	if err == nil {
		redisruntime.AttachURL(t, store, url)
	}
	return store, err
}
