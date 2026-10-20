package project

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

func deletionFixture(t *testing.T) (*Service, d.Project) {
	t.Helper()
	p := d.Project{ID: identity.New("delete_test"), Revision: 1, Title: "Fictional deletion", ScriptSource: d.ScriptSource{Version: 1, Filename: "fixture.txt", Spans: []d.SourceSpan{{ID: "s", Text: "Complete fictional text"}}}, Original: []byte("Complete fictional text"), Assets: contentAssets(), Runs: []d.Run{{ID: "cancelled", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "cancelled", Calls: []d.Call{{ID: "paid", CostMicroCNY: 123, Usage: billing.Usage{Known: true, Input: 10, Output: 20}}, {ID: "pending", Status: "unknown", ReservedMicroCNY: 456}}}}}
	store := newMemory()
	if err := store.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return &Service{Store: store}, p
}

func TestProjectDeletionRetainsRecordsAndLateAccountingWithoutModels(t *testing.T) {
	s, p := deletionFixture(t)
	ctx := context.Background()
	removed, err := s.DeleteProject(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if removed.DeletedAt == nil || removed.Revision != 2 {
		t.Fatal("missing atomic deletion receipt")
	}
	check := d.Clone(removed)
	check.DeletedAt = nil
	check.Revision = 1
	if !reflect.DeepEqual(check, p) {
		t.Fatal("delete changed original, results or ledger")
	}
	if _, err = s.Project(ctx, p.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("deleted project visible", err)
	}
	if _, err = s.DeleteProject(ctx, p.ID, 2); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("duplicate changed deletion", err)
	}
	for _, command := range []func() error{func() error { _, e := s.Start(ctx, p.ID, 2); return e }, func() error { _, e := s.Cancel(ctx, p.ID, 2); return e }, func() error { _, e := s.SaveSettings(ctx, p.ID, 2, d.ProjectSettings{}); return e }, func() error { _, e := s.SaveStyle(ctx, p.ID, 2, "style", "v"); return e }, func() error { _, e := s.StartVisual(ctx, p.ID, 2); return e }, func() error { _, e := s.DeleteAsset(ctx, p.ID, 2, "c"); return e }} {
		if e := command(); !errors.Is(e, fault.ErrNotFound) {
			t.Fatal("deleted write reopened", e)
		}
	}
	// Existing accounting uses the raw row. Removal must not release an unknown
	// reservation or discard a cancelled call's late charge. No model is executed.
	err = s.update(ctx, p.ID, func(latest *d.Project) error {
		latest.Runs[0].Calls[1].Status = "received"
		latest.Runs[0].Calls[1].Response = "stored late reply"
		latest.Runs[0].Calls[1].CostMicroCNY = 77
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Store.Snapshot(ctx, p.ID)
	if err != nil || saved.DeletedAt == nil || saved.Runs[0].Status != "cancelled" || saved.Runs[0].Calls[1].ReservedMicroCNY != 456 || saved.Runs[0].Calls[1].CostMicroCNY != 77 {
		t.Fatal("late accounting lost deleted ledger", err)
	}
	rows, _ := s.Store.(*memory).List(ctx)
	if len(rows) != 1 {
		t.Fatal("store lost historical project")
	}
}

func TestProjectDeletionRunningConflictAndConcurrentCAS(t *testing.T) {
	s, p := deletionFixture(t)
	ctx := context.Background()
	if _, e := s.DeleteProject(ctx, p.ID, 0); !errors.Is(e, fault.ErrConflict) {
		t.Fatal(e)
	}
	running := d.Clone(p)
	running.Runs[0].Status = "running"
	running.Revision++
	if e := s.Store.Commit(ctx, running, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DeleteProject(ctx, p.ID, 2); e == nil {
		t.Fatal("deleted running task")
	}
	saved, _ := s.Store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(saved, running) {
		t.Fatal("rejection partially wrote")
	}
	s, p = deletionFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.DeleteProject(ctx, p.ID, p.Revision); results <- e }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, fault.ErrConflict) && !errors.Is(e, fault.ErrNotFound) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("duplicate commit", success)
	}
}
