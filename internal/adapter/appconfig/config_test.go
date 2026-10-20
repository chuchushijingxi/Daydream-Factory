package appconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
)

const minimal = `database:
  url: postgres://fictional:fictional@127.0.0.1:55432/test?sslmode=disable
nsq:
  address: 127.0.0.1:54150
storage:
  endpoint: http://127.0.0.1:59000
  bucket: fictional-bucket
  accessKey: fictional-access
  secretKey: fictional-secret
`

func example(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../config/app.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func write(t *testing.T, raw string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "app.local.yaml")
	if err := os.WriteFile(name, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}
func TestConnectionsAloneUseSharedDefaults(t *testing.T) {
	t.Setenv("DRAMA_DATABASE_URL", "postgres://obsolete:ignored@127.0.0.1:1/ignored")
	t.Setenv("DRAMA_NSQ_ADDRESS", "127.0.0.1:1")
	t.Setenv("DRAMA_READ_ONLY", "1")
	name := write(t, minimal)
	cfg, err := appconfig.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	defaults := appconfig.Defaults()
	if cfg.ReadOnly || strings.Contains(cfg.Database.URL, "obsolete") || cfg.NSQ.Address != "127.0.0.1:54150" {
		t.Fatal("file connections overridden by environment")
	}
	if cfg.Tasks != defaults.Tasks || cfg.Database.MaxConnections != defaults.Database.MaxConnections || cfg.NSQ.Topic != defaults.NSQ.Topic || cfg.NSQ.Channel != defaults.NSQ.Channel || cfg.NSQ.Concurrency != defaults.NSQ.Concurrency {
		t.Fatal("omitted runtime settings lost their shared defaults")
	}
	if cfg.NSQ.Topic != "drama.tasks" || cfg.NSQ.Channel != "workers" {
		t.Fatal("unexpected default task queue names")
	}
	if cfg.Storage.Region != "us-east-1" || !cfg.Storage.PathStyle || cfg.Storage.ReadTimeout != defaults.Storage.ReadTimeout {
		t.Fatal("S3 defaults lost")
	}
	if cfg.Models.KeyFile != filepath.Clean(filepath.Join(filepath.Dir(name), "../.data/model-config.key")) || cfg.HTTP.DemoDir != filepath.Clean(filepath.Join(filepath.Dir(name), "../demo")) || cfg.Media.FFmpeg != "ffmpeg" {
		t.Fatal("default local resource paths changed")
	}
}
func TestOptionalOverridesPreserveOtherDefaults(t *testing.T) {
	cfg, err := appconfig.Load(write(t, minimal+"tasks:\n  queryConcurrency: 3\n  pollInterval: 1700ms\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tasks.QueryConcurrency != 3 || cfg.Tasks.PollInterval != 1700*time.Millisecond || cfg.Tasks.SubmitConcurrency != 8 || cfg.Tasks.SaveConcurrency != 4 || cfg.Tasks.Lease != 15*time.Second {
		t.Fatal("partial task configuration erased defaults")
	}
	raw := strings.Replace(example(t), "pathStyle: true", "pathStyle: false\n  sessionToken: fictional-token", 1)
	raw = strings.Replace(raw, "channel: workers", "channel: isolated_workers", 1)
	raw = strings.Replace(raw, "topic: drama.tasks", "topic: studio.tasks", 1)
	cfg, err = appconfig.Load(write(t, raw))
	if err != nil || cfg.Storage.PathStyle || cfg.Storage.SessionToken != "fictional-token" || cfg.NSQ.Channel != "isolated_workers" || cfg.NSQ.Topic != "studio.tasks" {
		t.Fatal("explicit connection options ignored", err)
	}
}
func TestRejectInvalidConfigurationWithoutLeakingValues(t *testing.T) {
	for _, tc := range []struct{ name, old, new string }{
		{"unknown", "queryConcurrency: 16", "queryConcurrencyTypo: 16"},
		{"duplicate", "queryConcurrency: 16", "queryConcurrency: 16\n  queryConcurrency: 3"},
		{"missingDatabase", "  url: postgres://drama:local-postgres@127.0.0.1:55432/drama_agent?sslmode=disable\n", ""},
		{"missingNSQ", "  address: 127.0.0.1:54150\n", ""},
		{"missingCredential", "  secretKey: local-minio-secret\n", ""},
		{"badDuration", "pollInterval: 1s", "pollInterval: PRIVATE_SECRET_SENTINEL"},
		{"zero", "queryConcurrency: 16", "queryConcurrency: 0"},
		{"negative", "saveConcurrency: 4", "saveConcurrency: -1"},
		{"shortPoll", "pollInterval: 1s", "pollInterval: 100us"},
		{"badDatabase", "url: postgres://drama:local-postgres@127.0.0.1:55432/drama_agent?sslmode=disable", "url: PRIVATE_SECRET_SENTINEL"},
		{"badS3", "endpoint: http://127.0.0.1:59000", "endpoint: https://PRIVATE_SECRET_SENTINEL:password@host/path"},
		{"transientMQ", "channel: workers", "channel: workers#ephemeral"},
		{"removedLeaseKnob", "pollInterval: 1s", "pollInterval: 1s\n  lease: 15s"},
		{"removedPoolKnob", "database:", "database:\n  maxConnections: 8"},
		{"removedReadOnlyKnob", "database:", "readOnly: true\ndatabase:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := appconfig.Load(write(t, strings.Replace(example(t), tc.old, tc.new, 1)))
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if strings.Contains(err.Error(), "PRIVATE_SECRET_SENTINEL") || strings.Contains(err.Error(), "password") {
				t.Fatal("configuration value leaked")
			}
		})
	}
	for _, raw := range []string{"", example(t) + "\n---\nsecret: PRIVATE_SECRET_SENTINEL", strings.Repeat("x", (1<<20)+1)} {
		if _, err := appconfig.Load(write(t, raw)); err == nil {
			t.Fatal("invalid file accepted")
		}
	}
	name := write(t, example(t))
	if err := os.Chmod(name, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := appconfig.Load(name); err == nil {
		t.Fatal("public credential file accepted")
	}
	if _, err := appconfig.Load(t.TempDir()); err == nil {
		t.Fatal("directory accepted")
	}
}
