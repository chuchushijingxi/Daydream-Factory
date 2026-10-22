package postgres

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

func TestMediaProjectReadPreservesDomainFactsAndOriginalHistory(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("isolated PG required")
	}
	ctx := context.Background()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p := d.Project{ID: identity.New("media_read"), OwnerID: identity.New("media_owner"), Revision: 1, Original: []byte("完整原件"), ScriptSource: d.ScriptSource{Version: 3}, Assets: []d.Asset{{ID: "asset", Name: "角色", Kind: "character", ImageFileID: "image"}}, VisualPrompts: []d.VisualPrompt{{SourceVersion: 3}}, Runs: []d.Run{{ID: "first", Contract: d.VisualContract, SourceVersion: 3, Status: d.RunCompleted, Targets: []d.VisualTarget{{AssetID: "asset"}}, Calls: []d.Call{{ID: "call", Response: "完整模型原答", CostMicroCNY: 123}}}, {ID: "second", Status: d.RunRunning}}}
	p = fixtureProject(t, store, p)
	if err = store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.projects WHERE id=$1`, p.ID)
	reader := store.ForProjectSpace(p.OwnerID).(ports.ProjectMediaReader)
	full, err := store.Snapshot(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	read, err := reader.MediaProject(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := d.Clone(full)
	expected.Original = nil
	for i := range expected.Runs {
		expected.Runs[i].Calls = nil
	}
	if !reflect.DeepEqual(read, expected) {
		t.Fatal("media read lost current domain facts or order")
	}
	after, err := store.Snapshot(ctx, p.ID)
	if err != nil || !reflect.DeepEqual(full, after) {
		t.Fatal("read rewrote original/history", err)
	}
	if _, err = store.ForProjectSpace("other").(ports.ProjectMediaReader).MediaProject(ctx, p.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("cross-owner read", err)
	}
	if _, err = store.ForProjectSpace("").(ports.ProjectMediaReader).MediaProject(ctx, p.ID); err == nil {
		t.Fatal("anonymous read")
	}
	deleted := time.Now().UTC()
	p.DeletedAt = &deleted
	p.Revision++
	if err = store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	read, err = reader.MediaProject(ctx, p.ID)
	if err != nil || read.DeletedAt == nil {
		t.Fatal("deleted visibility fact lost", err)
	}
}
