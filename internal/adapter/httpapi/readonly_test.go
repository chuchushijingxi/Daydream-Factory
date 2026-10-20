package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"drama-agent/internal/adapter/httpapi"
	app "drama-agent/internal/application/project"
)

func TestReadOnlyServiceRejectsWritesBeforeAccessingStoreOrModel(t *testing.T) {
	// Nil ports make an accidental write, launch or cancellation fail this test.
	host, handler := httpapi.New(&app.Service{}, nil, "../../../demo", 0)
	handler = authenticatedFixture(host, handler)
	host.ReadOnly = true
	if err := host.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	host.Launch("p")
	for _, path := range []string{
		"/api/upload",
		"/api/projects", "/api/projects/p/source", "/api/projects/p/settings",
		"/api/projects/p/style", "/api/projects/p/assets", "/api/projects/p/assets/a/delete",
		"/api/projects/p/assets/a/looks", "/api/projects/p/assets/a/looks/l/delete",
		"/api/projects/p/runs", "/api/projects/p/outline-runs",
		"/api/projects/p/visual-prompt-runs", "/api/projects/p/cancel",
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(`{"revision":1}`))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			var result map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 403 || result["code"] != "SERVICE_READ_ONLY" {
				t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
			}
		}
	}
	for _, path := range []string{"/api/status", "/api/models", "/demo/"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+path, nil))
		if w.Code != 200 {
			t.Fatalf("read %s: %d %s", path, w.Code, w.Body.String())
		}
		if path == "/api/status" {
			var status struct{ ReadOnly, Configured bool }
			if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil || !status.ReadOnly || status.Configured {
				t.Fatalf("status: %s", w.Body.String())
			}
		}
	}
}
