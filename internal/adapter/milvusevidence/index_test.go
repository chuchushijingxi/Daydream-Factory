package milvusevidence

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/adapter/localembedding"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

type vectorFixture struct{ invalid bool }

func (*vectorFixture) Identity() string { return "fixture-vector" }
func (*vectorFixture) Dimension() int   { return 2 }
func (e *vectorFixture) Embed(_ context.Context, texts []string, _ bool) ([][]float32, error) {
	out := [][]float32{}
	for range texts {
		v := []float32{1, 0}
		if e.invalid {
			v[0] = float32(math.NaN())
		}
		out = append(out, v)
	}
	return out, nil
}

func TestIndexScopeAndLosslessChunks(t *testing.T) {
	s, err := New("http://127.0.0.1:19530", "fixture", &vectorFixture{})
	if err != nil {
		t.Fatal(err)
	}
	doc := ports.EvidenceDocument{ProjectID: `p" or projectId != "`, Source: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "one", Text: strings.Repeat("完整原文🙂", 200)}, {ID: "blank", Text: ""}}}}
	scope, chunks, err := s.document(doc)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, c := range chunks {
		if c.SpanID == "one" {
			joined += c.Text
		}
		if len(c.Text) > 384 {
			t.Fatal("chunk over limit")
		}
	}
	if joined != doc.Source.Spans[0].Text || chunks[len(chunks)-1].SpanID != "blank" {
		t.Fatal("indexing dropped bytes/paragraph")
	}
	condition := filter(doc, scope)
	quoted, _ := json.Marshal(doc.ProjectID)
	if !strings.Contains(condition, string(quoted)) {
		t.Fatal("unescaped filter")
	}
	doc.Source.Version++
	next, _, _ := s.document(doc)
	if next == scope {
		t.Fatal("version reused index")
	}
	doc.Source.Version--
	doc.Source.Spans[0].Text += "changed"
	next, _, _ = s.document(doc)
	if next == scope {
		t.Fatal("changed bytes reused index")
	}
	s.embedder = &vectorFixture{invalid: true}
	if _, err = s.vectors(context.Background(), []string{"x"}, false); err == nil {
		t.Fatal("nonfinite embedding adopted")
	}
}

func TestIndexTransportAndSchemaFailureAreExplicit(t *testing.T) {
	for _, mode := range []string{"http", "json", "missingCode", "foreignCollection"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "http" {
					http.Error(w, "offline", 503)
					return
				}
				if mode == "json" {
					w.Write([]byte("broken"))
					return
				}
				if mode == "missingCode" {
					w.Write([]byte(`{"data":{}}`))
					return
				}
				if strings.HasSuffix(r.URL.Path, "/has") {
					w.Write([]byte(`{"code":0,"data":{"has":true}}`))
					return
				}
				w.Write([]byte(`{"code":0,"data":{"description":"another application","fields":[]}}`))
			}))
			defer srv.Close()
			s, err := New(srv.URL, "fixture", &vectorFixture{})
			if err != nil {
				t.Fatal(err)
			}
			err = s.Ensure(context.Background(), ports.EvidenceDocument{ProjectID: "p", Source: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "s", Text: "text"}}}})
			if err == nil {
				t.Fatal("index failure hidden")
			}
		})
	}
}

func TestPartialIndexIsNotSearchableAndRepairUsesDeterministicIDs(t *testing.T) {
	var mu sync.Mutex
	rows := map[string]bool{}
	upserts, searches := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var body struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/has"):
			data = map[string]any{"has": true}
		case strings.HasSuffix(r.URL.Path, "/describe"):
			data = map[string]any{"description": description, "fields": []any{map[string]any{"name": "vector", "type": "FloatVector", "params": []any{map[string]string{"key": "dim", "value": "2"}}}}}
		case strings.HasSuffix(r.URL.Path, "/query"):
			data = []any{map[string]any{"count(*)": len(rows)}}
		case strings.HasSuffix(r.URL.Path, "/upsert"):
			upserts++
			if upserts == 2 {
				http.Error(w, "injected partial index interruption", 503)
				return
			}
			for _, row := range body.Data {
				rows[row.ID] = true
			}
			data = map[string]any{"upsertCount": len(body.Data)}
		case strings.HasSuffix(r.URL.Path, "/search"):
			searches++
			data = []any{}
		default:
			t.Error("unexpected index operation", r.URL.Path)
			http.Error(w, "bad", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer srv.Close()
	e := &vectorFixture{}
	s, err := New(srv.URL, "partial_fixture", e)
	if err != nil {
		t.Fatal(err)
	}
	doc := ports.EvidenceDocument{ProjectID: "fiction", Source: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "long", Text: strings.Repeat("完整原文", 2000)}}}}
	_, chunks, _ := s.document(doc)
	if err = s.Ensure(context.Background(), doc); err == nil {
		t.Fatal("partial upsert pretended ready")
	}
	mu.Lock()
	count := len(rows)
	mu.Unlock()
	if count != 32 || count >= len(chunks) {
		t.Fatal("partial fixture did not stop after first batch", count)
	}
	e.invalid = true
	if _, _, err = s.Search(context.Background(), doc, "query", 5, 0); err == nil {
		t.Fatal("partial/failed index was searched")
	}
	mu.Lock()
	calls := searches
	mu.Unlock()
	if calls != 0 {
		t.Fatal("search ran before readiness")
	}
	e.invalid = false
	if err = s.Ensure(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	if err = s.Ensure(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	count = len(rows)
	mu.Unlock()
	if count != len(chunks) {
		t.Fatal("repair duplicated or lost chunks", count, len(chunks))
	}
}

func TestActualMilvusLocalBGEIsolationRebuildAndPagination(t *testing.T) {
	endpoint := os.Getenv("DRAMA_TEST_MILVUS_URL")
	if endpoint == "" || os.Getenv("DRAMA_TEST_LOCAL_EMBEDDING") != "1" {
		t.Skip("explicit free local integration opt-in")
	}
	e, err := localembedding.New("../../../.data/evidence-embedding/runtime/bin/python", "../../../.data/evidence-embedding/model")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(endpoint, identity.New("drama_evidence_test"), e)
	if err != nil {
		t.Fatal(err)
	}
	var has struct {
		Has bool `json:"has"`
	}
	if err = s.call(context.Background(), "collections/has", map[string]any{}, &has); err != nil || has.Has {
		t.Fatal("test collection not exclusively new", err)
	}
	t.Cleanup(func() {
		if err := s.call(context.Background(), "collections/drop", map[string]any{}, nil); err != nil {
			t.Error(err)
		}
	})
	doc := ports.EvidenceDocument{ProjectID: "fictional-A", Source: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "s1", Text: "她摘下帽子，眉毛左侧的旧伤清晰可见。"}, {ID: "s2", Text: "她穿着黑色长裙赴宴。"}, {ID: "s3", Text: "庭院里，园丁正在给花盆浇水。"}}}}
	if err = s.Ensure(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	hits, more, err := s.Search(context.Background(), doc, "人物左眉的疤痕", 1, 0)
	if err != nil || len(hits) != 1 || hits[0].SpanID != "s1" || !more {
		t.Fatal("semantic search/first page", hits, more, err)
	}
	next, _, err := s.Search(context.Background(), doc, "人物左眉的疤痕", 1, 1)
	if err != nil || len(next) != 1 || next[0].SpanID == hits[0].SpanID {
		t.Fatal("pagination", next, err)
	}
	other := doc
	other.ProjectID = "fictional-B"
	other.Source.Spans = []d.SourceSpan{{ID: "foreign", Text: "左眉有疤，但这是另一个项目的人物。"}}
	if err = s.Ensure(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	hits, _, err = s.Search(context.Background(), doc, "人物左眉的疤痕", 5, 0)
	if err != nil || len(hits) != 3 {
		t.Fatal("project isolation", hits, err)
	}
	for _, h := range hits {
		if h.SpanID == "foreign" {
			t.Fatal("foreign source leaked")
		}
	}
	updated := doc
	updated.Source.Version = 2
	updated.Source.Spans = []d.SourceSpan{{ID: "new", Text: "新版本中她穿着银灰外套。"}}
	if err = s.Ensure(context.Background(), updated); err != nil {
		t.Fatal(err)
	}
	hits, _, err = s.Search(context.Background(), updated, "她的衣服", 5, 0)
	if err != nil || len(hits) != 1 || hits[0].SpanID != "new" {
		t.Fatal("source version isolation", hits, err)
	}
	// Delete only this test source's derived rows and rebuild from the same PG
	// material. No production collection or source/project row is touched.
	scope, chunks, _ := s.document(doc)
	if err = s.call(context.Background(), "entities/delete", map[string]any{"filter": filter(doc, scope)}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.Ensure(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	count, err := s.count(context.Background(), filter(doc, scope))
	if err != nil || count != len(chunks) {
		t.Fatal("lost index cannot rebuild", count, err)
	}
	t.Logf("owned test collection %s; learned BGE512, source/project isolation, rebuild and pagination verified", s.collection)
}

func TestEndpointStaysOnThisMachineOrTheComposeNetwork(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:19530", "http://[::1]:19530", "http://milvus:19530", "http://milvus-index:19530/"} {
		if _, err := New(endpoint, "fixture", &vectorFixture{}); err != nil {
			t.Fatal(endpoint, err)
		}
	}
	for _, endpoint := range []string{
		"https://milvus:19530", "http://milvus.example.com:19530", "http://10.0.0.8:19530", "http://8.8.8.8:19530",
		"http://Milvus:19530", "http://-milvus:19530", "http://user@milvus:19530", "http://milvus:19530/v2", "http://milvus:19530?x=1",
	} {
		if _, err := New(endpoint, "fixture", &vectorFixture{}); err == nil {
			t.Fatal("accepted", endpoint)
		}
	}
}
