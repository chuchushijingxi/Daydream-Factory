package mailmigration

import (
	"drama-agent/internal/adapter/appconfig"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyFileIsRetainedUntilVerifiedAndOtherConnectionsSurvive(t *testing.T) {
	raw, err := os.ReadFile("../../../config/app.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw) + "\nmail:\n  address: smtp.example.test:465\n  username: sender\n  password: PRIVATE_LEGACY_SENTINEL\n  from: sender@example.test\n  tls: tls\n"
	path := filepath.Join(t.TempDir(), "app.local.yaml")
	if err = os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = appconfig.Load(path); err == nil || strings.Contains(err.Error(), "PRIVATE_LEGACY_SENTINEL") {
		t.Fatal("runtime read obsolete YAML or leaked secret", err)
	}
	document, err := Read(path)
	if err != nil || document.Legacy == nil || document.Legacy.Password != "PRIVATE_LEGACY_SENTINEL" {
		t.Fatal("legacy read", err)
	}
	before, _ := os.ReadFile(path)
	if string(before) != source {
		t.Fatal("read mutated input before PG confirmation")
	}
	if err = document.Finalize(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if strings.Contains(string(after), "PRIVATE_LEGACY_SENTINEL") || strings.Contains(string(after), "mail:") {
		t.Fatal("legacy secret not removed")
	}
	parsed, err := appconfig.Load(path)
	if err != nil || parsed.Database.URL != document.Config.Database.URL || parsed.Storage.SecretKey != document.Config.Storage.SecretKey || parsed.Models.KeyFile != document.Config.Models.KeyFile {
		t.Fatal("other resources changed", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("configuration permissions changed")
	}
	// A concurrent edit must survive; this failure leaves the old section for recovery.
	os.WriteFile(path, []byte(source), 0600)
	document, err = Read(path)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte(source+"# another edit\n"), 0600)
	if err = document.Finalize(); err == nil {
		t.Fatal("concurrent file edit overwritten")
	}
	current, _ := os.ReadFile(path)
	if !strings.Contains(string(current), "another edit") {
		t.Fatal("edit lost")
	}
}
