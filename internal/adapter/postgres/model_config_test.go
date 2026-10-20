package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
)

func TestModelCipherPrivatePersistentAndAuthenticated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if _, err := OpenModelCipher(path, false); err == nil {
		t.Fatal("missing original key was recreated")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing key check wrote a file")
	}
	cipher, e := OpenModelCipher(path, true)
	if e != nil {
		t.Fatal(e)
	}
	key := "dummy-api-key"
	encrypted, e := cipher.Seal(key)
	if e != nil || bytes.Contains(encrypted, []byte(key)) {
		t.Fatal("credential not encrypted", e)
	}
	second, e := OpenModelCipher(path, true)
	if e != nil {
		t.Fatal(e)
	}
	opened, e := second.Open(encrypted)
	if e != nil || opened != key {
		t.Fatal("key not persistent", e)
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, e = second.Open(encrypted); e == nil {
		t.Fatal("tampered credential accepted")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("key file not private")
	}
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = OpenModelCipher(path, true); e == nil {
		t.Fatal("insecure key permissions accepted")
	}
}

func TestModelCipherConcurrentInitialization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	var wg sync.WaitGroup
	values := make(chan *ModelCipher, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c, e := OpenModelCipher(path, true); values <- c; errs <- e }()
	}
	wg.Wait()
	close(values)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	first, e := OpenModelCipher(path, true)
	if e != nil {
		t.Fatal(e)
	}
	sealed, _ := first.Seal("dummy")
	for c := range values {
		if plain, e := c.Open(sealed); e != nil || plain != "dummy" {
			t.Fatal("concurrent keys differ")
		}
	}
}

func TestPostgresModelConfigurationCASRestartAndSharedLock(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("isolated PG opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, e := openRuntimePG(t, ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	before, e := s.ModelConfigSnapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		current, err := s.ModelConfigSnapshot(context.Background())
		if err == nil {
			before.Revision = current.Revision + 1
			if err = s.ModelConfigCommit(context.Background(), before, current.Revision); err != nil {
				t.Error(err)
			}
		}
	}()
	cipher, e := OpenModelCipher(filepath.Join(t.TempDir(), "key"), true)
	if e != nil {
		t.Fatal(e)
	}
	secret, _ := cipher.Seal("dummy-isolated")
	next := md.DefaultModelConfig()
	next.Revision = before.Revision + 1
	next.Connections[0].BaseURL = "https://example.invalid"
	next.Connections[0].Secret = secret
	next.Connections[0].Models[0].Enabled = true
	next.Connections[0].Models[1].Enabled = true
	if e = s.ModelConfigCommit(ctx, next, before.Revision); e != nil {
		t.Fatal(e)
	}
	if e = s.ModelConfigCommit(ctx, next, before.Revision); !errors.Is(e, fault.ErrConflict) {
		t.Fatal(e)
	}
	second, e := openRuntimePG(t, ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	actual, e := second.ModelConfigSnapshot(ctx)
	if e != nil || actual.Revision != next.Revision || actual.Connections[0].BaseURL != next.Connections[0].BaseURL || !actual.Connections[0].Models[1].Enabled || !actual.Connections[0].Models[0].Enabled || !bytes.Equal(actual.Connections[0].Secret, secret) {
		t.Fatal("PG restart readback mismatch", e)
	}
	unlock, e := s.AcquireModelConfig(ctx)
	if e != nil {
		t.Fatal(e)
	}
	locked, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	if release, e := second.AcquireModelConfig(locked); e == nil {
		release.Release()
		t.Fatal("configuration lock was not shared")
	}
	unlock.Release()
	release, e := second.AcquireModelConfig(ctx)
	if e != nil {
		t.Fatal(e)
	}
	release.Release()
}

func TestModelRegistryUpgradePreservesSecretAndStableIDs(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("isolated PG opt-in")
	}
	ctx := context.Background()
	s, e := openRuntimePG(t, ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, endpoint := range []string{md.DeepSeekBaseURL, "https://gateway.example.test/v1"} {
		namespace := identity.New("config_upgrade_test")
		if _, e = s.pool.Exec(ctx, "CREATE SCHEMA "+namespace+"; CREATE TABLE "+namespace+".model_config (id text PRIMARY KEY CHECK(id='deepseek'),revision bigint,base_url text,enabled boolean,pro_enabled boolean,credential bytea)"); e != nil {
			t.Fatal(e)
		}
		func() {
			defer s.pool.Exec(ctx, "DROP SCHEMA "+namespace+" CASCADE")
			secret := []byte{1, 2, 3, 4}
			if _, e = s.pool.Exec(ctx, "INSERT INTO "+namespace+".model_config VALUES('deepseek',7,$1,true,false,$2)", endpoint, secret); e != nil {
				t.Fatal(e)
			}
			upgraded := strings.ReplaceAll(schemaSQL, "drama_catalog_v1", namespace)
			for i := 0; i < 2; i++ {
				if _, e = s.pool.Exec(ctx, upgraded); e != nil {
					t.Fatal(e)
				}
			}
			var revision uint64
			var raw []byte
			if e = s.pool.QueryRow(ctx, "SELECT revision,connections FROM "+namespace+".model_config WHERE id='workspace'").Scan(&revision, &raw); e != nil {
				t.Fatal(e)
			}
			var connections []md.ModelConnection
			if e = json.Unmarshal(raw, &connections); e != nil {
				t.Fatal(e)
			}
			c := connections[0]
			if revision != 7 || c.BaseURL != endpoint || !bytes.Equal(c.Secret, secret) || c.Models[0].ID != md.DeepSeekModelID || !c.Models[0].Enabled || c.Models[1].ID != md.DeepSeekProModelID || c.Models[1].Enabled {
				t.Fatal("configuration lost during migration", revision, c)
			}
			if endpoint == md.DeepSeekBaseURL && !c.Models[0].Executable() {
				t.Fatal("verified official profile lost")
			}
			if endpoint != md.DeepSeekBaseURL && (c.Models[0].Pricing != nil || c.Models[0].ContextWindow != 0 || c.Models[0].Executable()) {
				t.Fatal("gateway inherited official pricing/capacity")
			}
		}()
	}
}
