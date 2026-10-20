package httpapi_test

import (
	"bytes"
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/uploadfiles"

	"github.com/jackc/pgx/v5"
)

type protocolModel struct{ calls atomic.Int32 }

func (*protocolModel) Capacity() (int, int) { return 1000000, 393216 }

func (m *protocolModel) Generate(ctx context.Context, req ports.Request, observe func(json.RawMessage) error) (ports.Response, error) {
	wire, _ := json.Marshal(req)
	if err := observe(wire); err != nil {
		return ports.Response{}, err
	}
	if reply, ok := modelregistry.FreeEpisodeIndexReply(req); ok {
		return reply, nil
	}
	if reply, ok := modelregistry.FreeVisualReply(req); ok {
		return reply, nil
	}
	m.calls.Add(1)
	var input struct {
		ScriptSource []d.SourceSpan `json:"source"`
		Assets       []d.Asset      `json:"assets"`
	}
	if err := json.Unmarshal([]byte(req.Input), &input); err != nil {
		return ports.Response{}, err
	}
	if len(input.Assets) == 0 {
		input.Assets = []d.Asset{{Kind: "character", Name: "祖父", Description: "姓名未说明", SourceIDs: []string{input.ScriptSource[0].ID}}}
	}
	text, _ := json.Marshal(app.CatalogChangeSet{Assets: catalogInputs(input.Assets)})
	return ports.Response{Actions: []d.Action{{ID: "save", Name: "save_catalog", Arguments: string(text), Status: "pending"}, {ID: "finish", Name: "finish_batch", Arguments: `{}`, Status: "pending"}}, Raw: json.RawMessage(`{"protocolFixture":true}`), FinishReason: "tool_calls", Usage: billing.Usage{Known: true, Input: 50, Output: 20}}, nil
}

// Uses a local protocol fixture, not a semantic pass. The same real PostgreSQL
// row is checked through upload, execution, read-back and UI response fields.
func TestUploadExecutionReadBackAndHTTPBoundaries(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("real PG opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := pgfixture.Open(t, ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	model := &protocolModel{}
	service := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Authorize: freeAuthorization, Store: store, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: model}}
	files := uploadfiles.New()
	service.Files = files
	host, handler := httpapi.New(service, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	host.Files = files
	server := httptest.NewServer(handler)
	defer server.Close()
	defer host.Shutdown(ctx)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	file, _ := writer.CreateFormFile("file", "sample.txt")
	io.WriteString(file, "祖父在工具间，姓名未说明。\n")
	writer.Close()
	uploaded, err := http.Post(server.URL+"/api/upload", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	var fileReceipt httpapi.FileView
	if uploaded.StatusCode != 201 || json.NewDecoder(uploaded.Body).Decode(&fileReceipt) != nil {
		t.Fatal("file upload failed")
	}
	uploaded.Body.Close()
	createJSON, _ := json.Marshal(map[string]any{"fileId": fileReceipt.FileID, "title": "API一致性", "billingBasis": quoteUploadedContent(t, server.URL, fileReceipt.FileID, d.ProjectSettings{})})
	response, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(createJSON))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var created struct {
		ID       string `json:"id"`
		Revision uint64 `json:"revision"`
	}
	if response.StatusCode != 201 || json.NewDecoder(response.Body).Decode(&created) != nil {
		t.Fatal("upload failed")
	}
	defer func() {
		conn, e := pgx.Connect(context.Background(), url)
		if e == nil {
			defer conn.Close(context.Background())
			conn.Exec(context.Background(), "DELETE FROM drama_catalog_v1.projects WHERE id=$1", created.ID)
		}
	}()
	if err = host.WaitUntilIdle(ctx); err != nil {
		t.Fatal(err)
	}
	indexed, err := store.Snapshot(ctx, created.ID)
	if err != nil || !indexed.IndexReady() {
		t.Fatal("automatic free index failed", err)
	}
	created.Revision = indexed.Revision
	post := func(payload string, origin string, hostHeader string) int {
		req, _ := http.NewRequest("POST", server.URL+"/api/projects/"+created.ID+"/runs", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if hostHeader != "" {
			req.Host = hostHeader
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if post(`{"revision":1}`, "https://foreign.invalid", "") != 403 || post(`{"revision":1}`, "", "foreign.invalid") != 403 {
		t.Fatal("external page can mutate local project")
	}
	if post(`{"revision":999}`, "", "") != 409 || post(`{"revision":1,"issue":true}`, "", "") != 400 {
		t.Fatal("stale/unknown commands accepted")
	}
	if post(`{"revision":`+strconv.FormatUint(created.Revision, 10)+`}`, "", "") != 409 {
		t.Fatal("one-start parsing already finished; duplicate generation must be rejected")
	}
	if err = host.WaitUntilIdle(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Snapshot(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, original, readErr := files.ReadOriginal(ctx, saved.SourceFileID)
	if readErr != nil || saved.SourceFileID == "" || saved.LatestRun().Status != "completed" || model.calls.Load() != 2 || string(original) != "祖父在工具间，姓名未说明。\n" {
		t.Fatal("production pipeline did not persist")
	}
	res, err := http.Get(server.URL + "/api/projects/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var projection struct {
		Assets []d.Asset `json:"assets"`
		Run    struct {
			Status    string
			CallCount int
		} `json:"run"`
	}
	if json.Unmarshal(raw, &projection) != nil || projection.Run.Status != "completed" || projection.Run.CallCount != 4 {
		t.Fatal("bad UI projection")
	}
	if len(projection.Assets) != len(saved.Assets) || projection.Assets[0].Description != saved.Assets[0].Description || bytes.Contains(raw, []byte("request")) || bytes.Contains(raw, []byte("original")) {
		t.Fatal("read/display mismatch or history leak")
	}
	for _, suffix := range []string{"source", "calls"} {
		res, e := http.Get(server.URL + "/api/projects/" + created.ID + "/" + suffix)
		if e != nil {
			t.Fatal(e)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		expected := 200
		if suffix == "calls" {
			expected = 404
		}
		if res.StatusCode != expected || (suffix == "source" && !json.Valid(data)) {
			t.Fatal("source/retired receipt boundary failed", suffix)
		}
	}
	if post(`{"revision":1}`, "", "") != 409 {
		t.Fatal("old revision became writable")
	}
	res, err = http.Get(server.URL + "/demo/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("page not connected")
	}
	// The completed merged run includes visual design and must lock the source.
	if _, err = service.Upload(ctx, created.ID, saved.Revision, "new.txt", []byte("新的原文")); err == nil {
		t.Fatal("merged completion allowed source replacement")
	} else if e, ok := err.(*fault.Error); !ok || e.Code != "SOURCE_LOCKED" {
		t.Fatal(err)
	}
	res, err = http.Get(server.URL + "/api/projects/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var unchanged struct {
		Run    struct{ ID, Status string }
		Assets []d.Asset
		Source struct{ Version uint64 }
	}
	if json.NewDecoder(res.Body).Decode(&unchanged) != nil || unchanged.Run.ID != saved.LatestRun().ID || unchanged.Run.Status != "completed" || unchanged.Source.Version != saved.ScriptSource.Version || len(unchanged.Assets) != len(saved.Assets) {
		t.Fatal("denied source change lost merged result")
	}
	res.Body.Close()
}

type slowFirstModel struct {
	protocolModel
	entered chan struct{}
	release chan struct{}
	once    atomic.Bool
}

func (m *slowFirstModel) Generate(ctx context.Context, r ports.Request, o func(json.RawMessage) error) (ports.Response, error) {
	if m.once.CompareAndSwap(false, true) {
		close(m.entered)
		<-m.release
	}
	return m.protocolModel.Generate(ctx, r, o)
}

func TestCancelThenStartWhileOldResponseSettles(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("real PG opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := pgfixture.Open(t, ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model := &slowFirstModel{entered: make(chan struct{}), release: make(chan struct{})}
	service := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Authorize: freeAuthorization, Store: store, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: model}}
	p, err := service.ForActor(legacyHTTPActor).Create(ctx, "cancel-restart", "x.txt", []byte("祖父坐在工具间。"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		conn, e := pgx.Connect(context.Background(), url)
		if e == nil {
			defer conn.Close(context.Background())
			conn.Exec(context.Background(), "DELETE FROM drama_catalog_v1.projects WHERE id=$1", p.ID)
		}
	}()
	host, handler := httpapi.New(service, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	server := httptest.NewServer(handler)
	defer server.Close()
	defer host.Shutdown(ctx)
	post := func(path string, rev uint64) {
		input := map[string]any{"revision": rev}
		if path == "runs" {
			input["billingBasis"] = quoteProjectContent(t, server.URL, p.ID, app.QuoteInput{Revision: rev, Operation: "asset-preparation"})
		}
		body, _ := json.Marshal(input)
		res, e := http.Post(server.URL+"/api/projects/"+p.ID+"/"+path, "application/json", bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			raw, _ := io.ReadAll(res.Body)
			t.Fatalf("command %s: %d %s", path, res.StatusCode, raw)
		}
	}
	post("runs", p.Revision)
	select {
	case <-model.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	latest, err := store.Snapshot(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	post("cancel", latest.Revision)
	latest, err = store.Snapshot(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	bodyJSON, _ := json.Marshal(map[string]uint64{"revision": latest.Revision})
	res, e := http.Post(server.URL+"/api/projects/"+p.ID+"/runs", "application/json", bytes.NewReader(bodyJSON))
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatal("cancelled project reopened")
	}
	close(model.release)
	if err = host.WaitUntilIdle(ctx); err != nil {
		t.Fatal(err)
	}
	latest, err = store.Snapshot(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.LatestRun().Status != "cancelled" || len(latest.Assets) != 0 || len(latest.Runs) != 1 || latest.UsedMicroCNY() == 0 {
		t.Fatal("late response adopted or accounting lost")
	}

}

func TestRecoverPersistedResponseAcrossFreshStoreAndUnknownIsNotResent(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("real PG opt-in")
	}
	for _, status := range []string{"received", "pending"} {
		t.Run(status, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			first, err := pgfixture.Open(t, ctx, url)
			if err != nil {
				t.Fatal(err)
			}
			model := &protocolModel{}
			service := &app.Service{Credits: first, Prices: &frozenFixturePrices{version: 1}, Authorize: freeAuthorization, Store: first, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: model}}
			p, err := service.ForActor(legacyHTTPActor).Create(ctx, "recovery-contract", "x.txt", []byte("祖父坐在工具间。"), 0)
			if err != nil {
				t.Fatal(err)
			}
			p, err = quotedLegacyService(t, service, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "asset-preparation"}).Start(ctx, p.ID, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			p.LatestRun().End = len(p.ScriptSource.Spans)
			p.LatestRun().Calls = []d.Call{{ID: identity.New("call"), Stage: "extract", Start: 0, End: len(p.ScriptSource.Spans), Status: d.CallStatus(status), Actions: []d.Action{{ID: "save", Name: "save_catalog", Arguments: `{"assets":[{"kind":"character","name":"祖父","sourceIds":["` + p.ScriptSource.Spans[0].ID + `"]}]}`, Status: "pending"}, {ID: "finish", Name: "finish_batch", Arguments: `{}`, Status: "pending"}}, FinishReason: "tool_calls", ReservedMicroCNY: 999, Usage: billing.Usage{Known: status == "received", Input: 50, Output: 20}, CostMicroCNY: 260, Before: []d.Asset{}}}
			rev := p.Revision
			p.Revision++
			if err = first.Commit(ctx, p, rev); err != nil {
				t.Fatal(err)
			}
			first.Close()
			// A fresh adapter and application instance reconstruct entirely from PG.
			second, err := pgfixture.Open(t, ctx, url)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			defer func() {
				conn, e := pgx.Connect(context.Background(), url)
				if e == nil {
					defer conn.Close(context.Background())
					conn.Exec(context.Background(), "DELETE FROM drama_catalog_v1.projects WHERE id=$1", p.ID)
				}
			}()
			restarted := &app.Service{Credits: second, Prices: &frozenFixturePrices{version: 1}, Authorize: freeAuthorization, Store: second, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: model}}
			host, _ := httpapi.New(restarted, nil, "../../../demo", 0)
			defer host.Shutdown(ctx)
			if err = host.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			if err = host.WaitUntilIdle(ctx); err != nil {
				t.Fatal(err)
			}
			restored, err := second.Snapshot(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if status == "received" {
				if restored.LatestRun().Status != "completed" || model.calls.Load() != 1 || len(restored.Assets) != 1 || restored.UsedMicroCNY() != 520 {
					t.Fatal("known receipt resent or not adopted")
				}
			} else if restored.LatestRun().ErrorCode != "MODEL_RESULT_UNKNOWN" || model.calls.Load() != 0 || restored.UsedMicroCNY() != 999 {
				t.Fatal("unknown request resent or reservation released")
			}
		})
	}
}

func freeAuthorization(context.Context, d.Project, d.Run) (int64, error) {
	return 100000000, nil
}
