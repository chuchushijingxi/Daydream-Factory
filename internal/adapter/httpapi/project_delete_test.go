package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	d "drama-agent/internal/domain/project"
)

func TestProjectDeletionHTTPPreservesLedgerAndIsolation(t *testing.T) {
	ctx := context.Background()
	store := openProjectContentPG(t)
	service := &app.Service{Store: store, Importer: scriptimport.Importer{}}
	p, err := service.ForActor(legacyHTTPActor).Create(ctx, "Fictional removable project", "fictional.txt", []byte("Full fictional script."), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	other, err := service.ForActor(legacyHTTPActor).Create(ctx, "Unselected fictional project", "other.txt", []byte("Unselected fictional original."), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, other)
	p.Runs = []d.Run{{ID: "history", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "failed", Calls: []d.Call{{ID: "known", CostMicroCNY: 123}, {ID: "unknown", ReservedMicroCNY: 456, Status: "unknown"}}}}
	p.Revision++
	if err = store.Commit(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	before := d.Clone(p)
	host, handler := httpapi.New(service, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(ctx)
	request := func(method, path string, body []byte, want int) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	path := "/api/projects/" + p.ID
	for _, body := range []string{`{}`, `{"revision":0}`, `{"revision":2,"force":true}`, `{"revision":2} {}`} {
		request("POST", path+"/delete", []byte(body), 400)
	}
	request("POST", path+"/delete", []byte(`{"revision":1}`), 409)
	raw := request("POST", path+"/delete", []byte(`{"revision":2}`), 200)
	var receipt struct {
		ID        string
		Revision  uint64
		DeletedAt string
	}
	if json.Unmarshal(raw, &receipt) != nil || receipt.ID != p.ID || receipt.Revision != 3 || receipt.DeletedAt == "" {
		t.Fatal("incomplete deletion receipt")
	}
	saved, err := store.Snapshot(ctx, p.ID)
	if err != nil || saved.DeletedAt == nil {
		t.Fatal("row removed", err)
	}
	check := d.Clone(saved)
	check.DeletedAt = nil
	check.Revision = before.Revision
	if !reflect.DeepEqual(check, before) {
		t.Fatal("delete changed history or reservations")
	}
	for _, suffix := range []string{"", "/source", "/calls"} {
		request("GET", path+suffix, nil, 404)
	}
	for _, suffix := range []string{"/delete", "/runs", "/visual-prompt-runs", "/cancel"} {
		request("POST", path+suffix, []byte(`{"revision":3}`), 404)
	}
	request("POST", path+"/settings", []byte(`{"revision":3,"settings":{"aspectRatio":"","llmModel":"","imageModel":"","videoModel":""}}`), 404)
	var rows []struct {
		ID       string
		Revision uint64
	}
	raw = request("GET", "/api/projects", nil, 200)
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == p.ID {
			t.Fatal("deleted row still listed")
		}
		if row.Revision == 0 {
			t.Fatal("missing list revision")
		}
	}
	untouched, _ := store.Snapshot(ctx, other.ID)
	if !reflect.DeepEqual(untouched, other) {
		t.Fatal("unselected project changed")
	}
	reopened := openProjectContentPG(t)
	again, err := reopened.Snapshot(ctx, p.ID)
	if err != nil || !reflect.DeepEqual(again, saved) {
		t.Fatal("restart lost deletion/ledger", err)
	}
	host.ReadOnly = true
	request("POST", "/api/projects/"+other.ID+"/delete", []byte(`{"revision":1}`), 403)
}

type deletionCommitFailure struct{ ports.ProjectStore }

func (s deletionCommitFailure) ForProjectOwner(owner string) ports.ProjectStore {
	return deletionCommitFailure{s.ProjectStore.(ports.OwnedProjects).ForProjectOwner(owner)}
}

func (s deletionCommitFailure) Commit(context.Context, d.Project, uint64) error {
	return fmt.Errorf("forced deletion commit failure")
}

func TestProjectDeletionCASRollbackAndRunningHTTP(t *testing.T) {
	ctx := context.Background()
	store := openProjectContentPG(t)
	s := &app.Service{Store: store, Importer: scriptimport.Importer{}}
	p, err := s.ForActor(legacyHTTPActor).Create(ctx, "Fictional concurrency", "race.txt", []byte("Fictional full text"), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(ctx)
	post := func(rev uint64) int {
		body, _ := json.Marshal(map[string]uint64{"revision": rev})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1:8787/api/projects/"+p.ID+"/delete", bytes.NewReader(body)))
		return w.Code
	}
	p.Runs = []d.Run{{ID: "running", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "running", Calls: []d.Call{{ID: "unconfirmed", ReservedMicroCNY: 987, Status: "pending"}}}}
	p.Revision++
	if err = store.Commit(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	if post(2) != 409 {
		t.Fatal("running task deleted")
	}
	saved, _ := store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(saved, p) {
		t.Fatal("running rejection changed state")
	}
	p.Runs[0].Status = "cancelled"
	p.Revision++
	if err = store.Commit(ctx, p, 2); err != nil {
		t.Fatal(err)
	}
	s.Store = deletionCommitFailure{store}
	if post(3) != 500 {
		t.Fatal("failed commit reported success")
	}
	saved, _ = store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(saved, p) {
		t.Fatal("rollback failed")
	}
	s.Store = store
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- post(3) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for code := range results {
		if code == 200 {
			success++
		} else if code != 409 && code != 404 {
			t.Fatal(code)
		}
	}
	if success != 1 {
		t.Fatal("concurrent deletion committed twice")
	}
	saved, _ = store.Snapshot(ctx, p.ID)
	if saved.Revision != 4 || saved.DeletedAt == nil || saved.Runs[0].Calls[0].ReservedMicroCNY != 987 {
		t.Fatal("concurrent delete lost reservation")
	}
}
