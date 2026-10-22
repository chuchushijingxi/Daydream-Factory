package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

func TestPostgresAtomicIsolationConflictAndProcessLock(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set DRAMA_TEST_DATABASE_URL for real PG contract")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := openRuntimePG(t, ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p := d.Project{ID: identity.New("store_contract"), Revision: 1, Title: "原始", Assets: []d.Asset{{ID: "a", Kind: "character", Name: "祖父", SourceIDs: []string{"s"}}}, Runs: []d.Run{}, ScriptSource: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "s", Text: "原文"}}}, Original: []byte("原文")}
	p = fixtureProject(t, s, p)
	if err = s.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer s.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.projects WHERE id=$1`, p.ID)
	if err = s.Create(ctx, p); !errors.Is(err, fault.ErrConflict) {
		t.Fatal("duplicate identity not reported as conflict", err)
	}
	a, _ := s.Snapshot(ctx, p.ID)
	a.Assets[0].Name = "未提交"
	b, _ := s.Snapshot(ctx, p.ID)
	if b.Assets[0].Name != "祖父" {
		t.Fatal("snapshot leaked mutation")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, title := range []string{"甲", "乙"} {
		wg.Add(1)
		go func(title string) {
			defer wg.Done()
			q := d.Clone(p)
			q.Title = title
			q.Revision = 2
			results <- s.Commit(ctx, q, 1)
		}(title)
	}
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, fault.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal("CAS admitted two writers")
	}
	stored, _ := s.Snapshot(ctx, p.ID)
	if string(stored.Original) != "原文" || stored.Assets[0].Name != "祖父" {
		t.Fatal("atomic payload lost source/catalog")
	}
	release, acquired, err := s.Acquire(ctx, p.ID)
	if err != nil || !acquired {
		t.Fatal(err)
	}
	other, err := openRuntimePG(t, ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	_, acquired, err = other.Acquire(ctx, p.ID)
	if err != nil || acquired {
		t.Fatal("second process could duplicate request")
	}
	release.Release()
	release, acquired, err = other.Acquire(ctx, p.ID)
	if err != nil || !acquired {
		t.Fatal("lock was not released")
	}
	release.Release()
}

func TestLocksDoNotExhaustTransactionalPool(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("real PG opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := openRuntimePG(t, ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 10; i++ {
		release, ok, err := s.Acquire(ctx, identity.New("lock_contract"))
		if err != nil || !ok {
			t.Fatal(err)
		}
		defer release.Release()
	}
	if _, err = s.ListSummaries(ctx); err != nil {
		t.Fatal("lock sessions consumed transactional pool", err)
	}
}
