package postgres

import (
	"context"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProjectReadKeepsMeteringAndRecoverySnapshot(t *testing.T) {
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
	p := d.Project{ID: identity.New("read_view"), OwnerID: identity.New("owner"), Revision: 1, Original: []byte("原件"), ScriptSource: d.ScriptSource{Version: 3, Spans: []d.SourceSpan{{ID: "a", Text: "完整原文🙂"}}}, Runs: []d.Run{{ID: "run", Status: d.RunRunning, Calls: []d.Call{{ID: "known", Request: json.RawMessage(`{"private":"request"}`), Response: strings.Repeat("原答", 50000), ProviderState: "signed state", Actions: []d.Action{{ID: "action", Name: "save"}}, Usage: billing.Usage{Known: true, Input: 123, Output: 99}, CostMicroCNY: 77}, {ID: "unknown", Status: "pending", ReservedMicroCNY: 300}}}}}
	p = fixtureProject(t, store, p)
	if err = store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.projects WHERE id=$1`, p.ID)
	full, err := store.Snapshot(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	owned := store.ForProjectSpace(p.OwnerID)
	read, err := owned.(ports.ProjectReader).ReadProject(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := d.Clone(full)
	expected.Original = nil
	for i := range expected.Runs {
		for j := range expected.Runs[i].Calls {
			c := &expected.Runs[i].Calls[j]
			c.Request = nil
			c.Response = ""
			c.Text = ""
			c.Reasoning = ""
			c.ProviderState = ""
			c.Actions = nil
			c.Before = nil
			c.After = nil
		}
	}
	if !reflect.DeepEqual(expected, read) || read.UsedMicroCNY() != 377 {
		t.Fatal("current facts or metering lost", read.UsedMicroCNY())
	}
	before, _ := json.Marshal(full)
	small, _ := json.Marshal(read)
	if len(small) >= len(before)/10 {
		t.Fatal("history body still read", len(before), len(small))
	}
	t.Logf("full=%d bytes projected=%d bytes; calls=%d, cost/reserve=%d", len(before), len(small), len(read.Runs[0].Calls), read.UsedMicroCNY())
	after, err := store.Snapshot(ctx, p.ID)
	if err != nil || !reflect.DeepEqual(full, after) {
		t.Fatal("projection changed recovery evidence", err)
	}
	for _, owner := range []string{"other", ""} {
		if _, err = store.ForProjectSpace(owner).(ports.ProjectReader).ReadProject(ctx, p.ID); err == nil {
			t.Fatal("owner boundary", owner)
		}
	}
	deleted := time.Now().UTC()
	p.DeletedAt = &deleted
	p.Revision++
	if err = store.Commit(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	service := app.Service{Store: owned}
	if _, err = service.ReadProject(ctx, p.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("deleted visible", err)
	}
	if snapshot, err := store.Snapshot(ctx, p.ID); err != nil || snapshot.Runs[0].Calls[0].ProviderState != "signed state" {
		t.Fatal("deleted recovery lost", err)
	}
}
