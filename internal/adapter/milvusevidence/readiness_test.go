package milvusevidence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"drama-agent/internal/adapter/localembedding"
	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/project"
)

func TestCompleteIndexStillRequiresRunnableLocalEncoder(t *testing.T) {
	for _, mode := range []string{"non-executable", "missing-dependency"} {
		t.Run(mode, func(t *testing.T) {
			folder := t.TempDir()
			python := filepath.Join(folder, "python")
			for _, name := range localembedding.ModelFiles {
				if err := os.WriteFile(filepath.Join(folder, name), []byte("fictional model files; never loaded"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			permission := os.FileMode(0700)
			if mode == "non-executable" {
				permission = 0600
			}
			if err := os.WriteFile(python, []byte("#!/bin/sh\necho 'ModuleNotFoundError: no module named onnxruntime' >&2\nexit 1\n"), permission); err != nil {
				t.Fatal(err)
			}
			encoder, err := localembedding.New(python, folder)
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var data any
				switch {
				case strings.HasSuffix(r.URL.Path, "/has"):
					data = map[string]any{"has": true}
				case strings.HasSuffix(r.URL.Path, "/describe"):
					data = map[string]any{"description": description, "fields": []any{map[string]any{"name": "vector", "type": "FloatVector", "params": []any{map[string]string{"key": "dim", "value": "512"}}}}}
				case strings.HasSuffix(r.URL.Path, "/query"):
					data = []any{map[string]any{"count(*)": 1}}
				default:
					t.Error("complete index should not be written", r.URL.Path)
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			index, err := New(server.URL, "fictional_complete_index", encoder)
			if err != nil {
				t.Fatal(err)
			}
			doc := ports.EvidenceDocument{ProjectID: "fictional", Source: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "s", Text: "fictional original"}}}}
			if err = index.Ensure(context.Background(), doc); err == nil || !strings.Contains(err.Error(), "EVIDENCE_UNAVAILABLE") {
				t.Fatal("existing index concealed unusable encoder", err)
			}
			vector := make([]float32, 512)
			vector[0] = 1
			result, _ := json.Marshal(map[string]any{"vectors": [][]float32{vector}})
			// A fixture executable replaces only this test's broken worker. Its
			// output never enters a production index or becomes a fallback.
			if err = os.WriteFile(python, append([]byte("#!/bin/sh\ncat >/dev/null\ncat <<'VECTOR'\n"), append(result, []byte("\nVECTOR\n")...)...), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(python, 0700); err != nil {
				t.Fatal(err)
			}
			if err = index.Ensure(context.Background(), doc); err != nil {
				t.Fatal("repaired runtime cannot use the same complete index", err)
			}
			calls := requests.Load()
			if calls != 3 {
				t.Fatal("readiness check touched cached index before recovery", calls)
			}
			if err = os.Chmod(python, 0600); err != nil {
				t.Fatal(err)
			}
			if err = index.Ensure(context.Background(), doc); err == nil || requests.Load() != calls {
				t.Fatal("cached adapter concealed later runtime failure", err)
			}
		})
	}
}
