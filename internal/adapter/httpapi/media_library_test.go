package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/medialibrary"
	app "drama-agent/internal/application/project"
)

type libraryStore struct{ owner string }

func (s *libraryStore) ListGeneratedMedia(_ context.Context, owner string, q medialibrary.Query) ([]medialibrary.Item, error) {
	s.owner = owner
	return []medialibrary.Item{{TaskID: "task", OutputID: "out", ProjectID: "project", Kind: "image", FileID: "image-file", CreatedAt: time.Now(), ModelID: "model"}}, nil
}
func (s *libraryStore) GeneratedMedia(_ context.Context, owner, task, output string) (medialibrary.Item, error) {
	s.owner = owner
	return medialibrary.Item{TaskID: task, OutputID: output, ProjectID: "project", Kind: "video", Prompt: "完整冻结提示词"}, nil
}
func TestMediaLibraryHTTPUsesSharedAuthenticationAndNarrowProjection(t *testing.T) {
	h, router := httpapi.New(&app.Service{}, nil, "", 0)
	store := &libraryStore{}
	h.MediaLibrary = &medialibrary.Service{Store: store}
	out := httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest("GET", "http://127.0.0.1/api/media-library", nil))
	if out.Code != 503 {
		t.Fatal("missing authenticator", out.Code)
	}
	h.Authenticator = fixtureAuthenticator{}
	out = httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest("GET", "http://127.0.0.1/api/media-library", nil))
	if out.Code != 401 {
		t.Fatal("anonymous access", out.Code)
	}
	router = authenticatedFixture(h, router)
	for _, path := range []string{"/api/media-library?kind=image&limit=1", "/api/media-library/task/outputs/out"} {
		out = httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil))
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
		if store.owner != "http-fixture" || strings.Contains(out.Body.String(), "fileId") || strings.Contains(out.Body.String(), "credential") {
			t.Fatal("private projection or owner", store.owner, out.Body.String())
		}
		if !strings.Contains(out.Body.String(), "/api/projects/project/media-tasks/task/outputs/out/file") {
			t.Fatal("unprotected URL", out.Body.String())
		}
		var body map[string]any
		if json.Unmarshal(out.Body.Bytes(), &body) != nil {
			t.Fatal("not JSON")
		}
	}
	for _, path := range []string{"/api/media-library?limit=0", "/api/media-library?limit=101", "/api/media-library?limit=no", "/api/media-library?kind=audio", "/api/media-library?cursor=bad"} {
		out = httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest("GET", "http://127.0.0.1"+path, nil))
		if out.Code != 400 {
			t.Fatal(path, out.Code, out.Body.String())
		}
	}
}
