package httpapi_test

import (
	"net/http/httptest"
	"testing"

	"drama-agent/internal/adapter/httpapi"
	app "drama-agent/internal/application/project"
)

func TestRemovedProcessRoutesReturnNotFound(t *testing.T) {
	host, handler := httpapi.New(&app.Service{}, nil, "../../../demo", 0)
	handler = authenticatedFixture(host, handler)
	for _, path := range []string{"/api/projects/project/calls", "/demo/process.html", "/demo/process.mjs", "/demo/process-data.mjs", "/demo/process.css", "/demo/styles.css", "/demo/theme.js"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", "http://127.0.0.1"+path, nil))
		if recorder.Code != 404 {
			t.Fatalf("removed surface still served: %s %d", path, recorder.Code)
		}
	}
}
