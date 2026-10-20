package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"

	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/billing"
	d "drama-agent/internal/domain/project"
)

type recordReadStore struct {
	ports.ProjectStore
	project d.Project
}

func (s recordReadStore) Snapshot(context.Context, string) (d.Project, error) {
	return d.Clone(s.project), nil
}

func TestRemovedProcessSurfacePreservesStoredEvidence(t *testing.T) {
	p := d.Project{ID: "project", Revision: 4, ScriptSource: d.ScriptSource{Version: 2}, Runs: []d.Run{{ID: "first", SourceVersion: 1, Status: "failed", CreatedAt: time.Now(), Calls: []d.Call{{ID: "unknown", Status: "unknown", Response: "完整原答", ReservedMicroCNY: 900, Request: json.RawMessage(`{"messages":[{"role":"user","content":"完整原文"}]}`), Usage: billing.Usage{Known: false}}}}}}
	before := d.Clone(p)
	host, handler := httpapi.New(&app.Service{Store: recordReadStore{project: p}}, nil, "../../../demo", 0)
	handler = authenticatedFixture(host, handler)
	for _, path := range []string{"/api/projects/project/calls", "/demo/process.html", "/demo/process.mjs", "/demo/process-data.mjs", "/demo/process.css", "/demo/styles.css", "/demo/theme.js"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", "http://127.0.0.1"+path, nil))
		if recorder.Code != 404 {
			t.Fatalf("removed surface still served: %s %d", path, recorder.Code)
		}
	}
	stored, err := (recordReadStore{project: p}).Snapshot(context.Background(), p.ID)
	if err != nil || !reflect.DeepEqual(before, stored) {
		t.Fatal("removing UI changed original answers or reservations")
	}
}
