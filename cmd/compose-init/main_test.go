package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshot(t *testing.T, target string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(target, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = string(raw)
	}
	return out
}

func mustInitialize(t *testing.T, root string) {
	t.Helper()
	if _, err := initialize(root); err != nil {
		t.Fatal(err)
	}
}

func TestFreshConfigurationIsPrivateAndRepeatable(t *testing.T) {
	root := t.TempDir()
	mustInitialize(t, root)
	target := filepath.Join(root, "config")
	original := snapshot(t, target)
	if len(original) != 5 || original["minio-user"] != "drama" || original["redis-password"] == "" {
		t.Fatal(original)
	}
	// 新部署默认开启演示收银台，开箱可体验充值全流程。
	if !strings.HasPrefix(original["app.local.yaml"], "environment: development\npayment:\n  demo:\n    enabled: true\n") {
		t.Fatal(original["app.local.yaml"])
	}
	for name := range original {
		info, _ := os.Stat(filepath.Join(target, name))
		if info.Mode().Perm() != 0o600 {
			t.Fatal(name, info.Mode())
		}
	}
	mustInitialize(t, root)
	again := snapshot(t, target)
	for name, value := range original {
		if again[name] != value {
			t.Fatal("rerun changed", name)
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 1 {
		t.Fatal("staging directory left behind", entries)
	}
}

func TestExistingEnvironmentOnlyAddsRedis(t *testing.T) {
	root := t.TempDir()
	mustInitialize(t, root)
	target := filepath.Join(root, "config")
	config := filepath.Join(target, "app.local.yaml")
	raw, _ := os.ReadFile(config)
	legacy := strings.Split(string(raw), "\nredis:\n")[0] + "\n"
	if err := os.WriteFile(config, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(target, "redis-password"))
	before := snapshot(t, target)
	mustInitialize(t, root)
	after := snapshot(t, target)
	if !strings.HasPrefix(after["app.local.yaml"], legacy) || !strings.Contains(after["app.local.yaml"], "\nredis:\n") {
		t.Fatal(after["app.local.yaml"])
	}
	for _, name := range []string{"postgres-password", "minio-password"} {
		if before[name] != after[name] {
			t.Fatal("secret changed", name)
		}
	}
}

func TestInterruptedRedisUpgradeReusesItsSecret(t *testing.T) {
	root := t.TempDir()
	mustInitialize(t, root)
	target := filepath.Join(root, "config")
	config := filepath.Join(target, "app.local.yaml")
	raw, _ := os.ReadFile(config)
	_ = os.WriteFile(config, []byte(strings.Split(string(raw), "\nredis:\n")[0]+"\n"), 0o600)
	secret, _ := os.ReadFile(filepath.Join(target, "redis-password"))
	mustInitialize(t, root)
	if again, _ := os.ReadFile(filepath.Join(target, "redis-password")); string(again) != string(secret) {
		t.Fatal("Redis secret regenerated")
	}
	if updated, _ := os.ReadFile(config); !strings.Contains(string(updated), "password: "+string(secret)+"\n") {
		t.Fatal("config does not use the reused secret")
	}
}

func TestInsecureExistingConfigIsRejected(t *testing.T) {
	root := t.TempDir()
	mustInitialize(t, root)
	if err := os.Chmod(filepath.Join(root, "config", "redis-password"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := initialize(root); err == nil {
		t.Fatal("world-readable secret accepted")
	}
}

func TestProductionSwitchClosesDemoAndKeepsSecrets(t *testing.T) {
	root := t.TempDir()
	mustInitialize(t, root)
	target := filepath.Join(root, "config")
	before := snapshot(t, target)
	for range 2 {
		if _, err := disableDemo(root); err != nil {
			t.Fatal(err)
		}
	}
	after := snapshot(t, target)
	config := after["app.local.yaml"]
	if !strings.HasPrefix(config, "environment: production\npayment:\n  demo:\n    enabled: false\n") ||
		strings.Replace(strings.Replace(config, "production", "development", 1), "enabled: false", "enabled: true", 1) != before["app.local.yaml"] {
		t.Fatal(config)
	}
	if info, _ := os.Stat(filepath.Join(target, "app.local.yaml")); info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
	if _, err := disableDemo(t.TempDir()); err == nil {
		t.Fatal("missing configuration accepted")
	}
}
