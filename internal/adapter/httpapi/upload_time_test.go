package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/scriptimport"
	app "drama-agent/internal/application/project"
)

func TestUploadTimePersistsAndProjectsExposeUnknownHonestly(t *testing.T) {
	store := openProjectContentPG(t)
	service := &app.Service{Store: store, Importer: scriptimport.Importer{}}
	before := time.Now().UTC()
	p, err := service.ForActor(legacyHTTPActor).Create(context.Background(), "fictional upload time", "fiction.txt", []byte("A fictional person enters a room."), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	if p.UploadedAt == nil || p.UploadedAt.Before(before) || p.UploadedAt.After(time.Now().UTC()) {
		t.Fatalf("invalid upload time: %v", p.UploadedAt)
	}
	uploadedAt := *p.UploadedAt
	p, err = service.Upload(context.Background(), p.ID, p.Revision, "replacement.txt", []byte("A fictional person leaves a room."))
	if err != nil || p.UploadedAt == nil || !p.UploadedAt.Equal(uploadedAt) {
		t.Fatalf("source replacement changed project upload time: %v", err)
	}
	reopened, err := store.Snapshot(context.Background(), p.ID)
	if err != nil || reopened.UploadedAt == nil || !reopened.UploadedAt.Equal(uploadedAt) {
		t.Fatal("timestamp not durable", err)
	}
	host, handler := httpapi.New(service, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	host.ReadOnly = true
	for _, path := range []string{"/api/projects", "/api/projects/" + p.ID} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+path, nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var view struct {
			ID         string
			UploadedAt *time.Time
		}
		if path == "/api/projects" {
			var views []struct {
				ID         string
				UploadedAt *time.Time
			}
			if err = json.Unmarshal(w.Body.Bytes(), &views); err != nil {
				t.Fatal(err)
			}
			for _, row := range views {
				if row.ID == p.ID {
					view = row
				}
			}
		} else if err = json.Unmarshal(w.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if view.UploadedAt == nil || !view.UploadedAt.Equal(uploadedAt) {
			t.Fatalf("API lost upload time: %s", w.Body.String())
		}
	}
	// A test row representing absent historical metadata is not backfilled on read.
	p.UploadedAt = nil
	p.Revision++
	if err = store.Commit(context.Background(), p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/projects/"+p.ID, nil))
	var view map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if v, present := view["uploadedAt"]; !present || v != nil {
		t.Fatal("missing time fabricated", view)
	}
	final, _ := store.Snapshot(context.Background(), p.ID)
	if final.UploadedAt != nil || final.Revision != p.Revision {
		t.Fatal("read changed data")
	}
}
