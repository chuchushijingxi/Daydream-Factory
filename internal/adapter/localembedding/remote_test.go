package localembedding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/domain/fault"
)

func embeddingService(t *testing.T, respond func(w http.ResponseWriter, texts []string, query bool)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/identity":
			_ = json.NewEncoder(w).Encode(map[string]any{"identity": identityPrefix + "abc", "dimension": 512})
		case "/v1/embed":
			var in struct {
				Texts []string `json:"texts"`
				Query bool     `json:"query"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			respond(w, in.Texts, in.Query)
		default:
			http.NotFound(w, r)
		}
	}))
}

func vectors(n, dim int) [][]float32 {
	out := make([][]float32, n)
	for i := range out {
		out[i] = make([]float32, dim)
	}
	return out
}

func TestRemoteEmbedderUsesTheServiceIdentityAndVectors(t *testing.T) {
	var gotQuery bool
	server := embeddingService(t, func(w http.ResponseWriter, texts []string, query bool) {
		gotQuery = query
		_ = json.NewEncoder(w).Encode(map[string]any{"vectors": vectors(len(texts), 512)})
	})
	defer server.Close()
	e, err := NewRemote(server.URL+"/", appconfig.Evidence{EmbeddingTimeout: 5 * time.Second})
	if err != nil || e.Identity() != identityPrefix+"abc" || e.Dimension() != 512 {
		t.Fatal(e, err)
	}
	out, err := e.Embed(context.Background(), []string{"甲", "乙"}, true)
	if err != nil || len(out) != 2 || len(out[0]) != 512 || !gotQuery {
		t.Fatal(len(out), err)
	}
	if _, err = e.Embed(context.Background(), nil, false); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("empty batch", err)
	}
}

func TestRemoteEmbedderMapsServiceOutcomes(t *testing.T) {
	var reply func(w http.ResponseWriter, n int)
	server := embeddingService(t, func(w http.ResponseWriter, texts []string, _ bool) { reply(w, len(texts)) })
	defer server.Close()
	e, err := NewRemote(server.URL, appconfig.Evidence{})
	if err != nil {
		t.Fatal(err)
	}
	var f *fault.Error
	reply = func(w http.ResponseWriter, _ int) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "EMBEDDING_CAPACITY_EXCEEDED"})
	}
	if _, err = e.Embed(context.Background(), []string{"长文本"}, false); !errors.As(err, &f) || f.Code != "EVIDENCE_CAPACITY_EXCEEDED" {
		t.Fatal("capacity", err)
	}
	reply = func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "INVALID_INPUT"})
	}
	if _, err = e.Embed(context.Background(), []string{"x"}, false); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("invalid", err)
	}
	reply = func(w http.ResponseWriter, n int) {
		_ = json.NewEncoder(w).Encode(map[string]any{"vectors": vectors(n, 384)})
	}
	if _, err = e.Embed(context.Background(), []string{"x"}, false); !errors.As(err, &f) || f.Code != "EVIDENCE_INDEX_INVALID" {
		t.Fatal("dimension mismatch", err)
	}
}

func TestRemoteEmbedderRejectsAnUnavailableOrForeignService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"identity": "other-model", "dimension": 512})
	}))
	if _, err := NewRemote(server.URL, appconfig.Evidence{}); err == nil {
		t.Fatal("foreign model identity accepted")
	}
	endpoint := server.URL
	server.Close()
	if _, err := NewRemote(endpoint, appconfig.Evidence{}); err == nil {
		t.Fatal("unreachable service accepted")
	}
}
