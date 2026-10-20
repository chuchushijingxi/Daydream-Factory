package httpapi_test

import (
	"bytes"
	"context"
	"drama-agent/internal/application/ports"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/adapter/stylecatalog"
	app "drama-agent/internal/application/project"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
)

// The real PG command/read ports run; only the execution lock is unavailable.
// This allows HTTP 202/readback/cancel checks without executing even a fake model.
type interfaceNoExecutionStore struct{ *postgres.Store }

func (interfaceNoExecutionStore) Acquire(context.Context, string) (ports.Lease, bool, error) {
	return ports.Lease{}, false, nil
}

func TestSingleVisualPostgresInterfacesWithoutModelExecution(t *testing.T) {
	store := openProjectContentPG(t)
	s := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Store: interfaceNoExecutionStore{store}, Importer: scriptimport.Importer{}, Agent: interfaceOnlyAgent{}, Models: &modelregistry.Registry{Model: interfaceOnlyModel{}}}
	p := createProjectContent(t, s, pgAssets())
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(context.Background())
	request := func(suffix string, body any, want int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/api/projects/"+p.ID+suffix, bytes.NewReader(raw)))
		if w.Code != want {
			t.Fatalf("%s %d %s", suffix, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	before := d.Clone(p)
	for _, suffix := range []string{"/assets/c/visual-prompt-runs", "/assets/c/looks/l/visual-prompt-runs", "/visual-prompt-runs"} {
		request(suffix, map[string]any{"revision": p.Revision}, 403)
	}
	request("/assets/c/visual-prompt-runs", map[string]any{"revision": p.Revision, "targets": []any{}}, 400)
	request("/assets/c/looks/not-my-look/visual-prompt-runs", map[string]any{"revision": p.Revision}, 404)
	request("/assets/loc/looks/l/visual-prompt-runs", map[string]any{"revision": p.Revision}, 400)
	request("/assets/not-my-asset/visual-prompt-runs", map[string]any{"revision": p.Revision}, 404)
	request("/assets/c/visual-prompt-runs", map[string]any{"revision": p.Revision - 1}, 409)
	read, _ := store.Snapshot(context.Background(), p.ID)
	if !reflect.DeepEqual(read, before) {
		t.Fatal("denied commands changed PG/history")
	}
	// P3 completion permits ordinary saves without a visual task or model execution.
	request("/assets/c/looks/l", map[string]any{"revision": p.Revision, "look": d.LookInput{Name: "Changed look", Description: "Complete user description"}}, 200)
	p, _ = store.Snapshot(context.Background(), p.ID)
	if !p.CatalogEditedByUser || len(p.Runs) != 1 || p.UsedMicroCNY() != 789 {
		t.Fatal("P3 edit gate or historical reserve wrong")
	}
	s.Authorize = freeAuthorization
	for _, target := range []d.VisualTarget{{AssetID: "c"}, {AssetID: "c", LookID: "l"}, {AssetID: "loc"}, {AssetID: "prop"}} {
		suffix := "/assets/" + target.AssetID
		if target.LookID != "" {
			suffix += "/looks/" + target.LookID
		}
		suffix += "/visual-prompt-runs"
		raw := request(suffix, map[string]any{"revision": p.Revision, "billingBasis": legacyQuote(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "single-visual", AssetID: target.AssetID, LookID: target.LookID})}, 202)
		p, _ = store.Snapshot(context.Background(), p.ID)
		assertProjectContentView(t, raw, p)
		var response struct{ Run httpapi.RunView }
		if err := json.Unmarshal(raw, &response); err != nil || !reflect.DeepEqual(response.Run.Target, p.LatestRun().Target) || response.Run.Targets == nil || !reflect.DeepEqual(*response.Run.Targets, p.LatestRun().Targets) || response.Run.InputRevision != p.LatestRun().InputRevision || response.Run.Model != p.LatestRun().Model || response.Run.BudgetMicroCNY == nil || *response.Run.BudgetMicroCNY != p.LatestRun().BudgetMicroCNY {
			t.Fatal("API frozen target/model/budget projection differs from PG", err)
		}
		if *p.LatestRun().Target != target || p.LatestRun().Model != "deepseek-flash" || len(p.LatestRun().Calls) != 0 || p.UsedMicroCNY() != 789 {
			t.Fatal("start did not freeze correct single target/model or added costs")
		}
		if target.LookID != "" && !reflect.DeepEqual(p.LatestRun().Targets, []d.VisualTarget{{AssetID: "c"}, target}) {
			t.Fatal("required missing base not frozen")
		}
		request("/assets/c/delete", map[string]any{"revision": p.Revision}, 409)
		request(suffix, map[string]any{"revision": p.Revision}, 409)
		request("/cancel", map[string]any{"revision": p.Revision}, 200)
		p, _ = store.Snapshot(context.Background(), p.ID)
		if p.LatestRun().Status != "cancelled" || len(p.LatestRun().Calls) != 0 || p.FailedTask() != nil || p.HasCancelledTask() {
			t.Fatal("visual cancellation blocked project or generated calls")
		}
	}
	reopened := openProjectContentPG(t)
	read, err := reopened.Snapshot(context.Background(), p.ID)
	if err != nil || !reflect.DeepEqual(read, p) {
		t.Fatal("reopened PG view differs", err)
	}
	styles, err := stylecatalog.LoadStyles("../../../demo")
	if err != nil {
		t.Fatal(err)
	}
	s.Styles = styles
	_, err = s.SaveStyle(context.Background(), p.ID, p.Revision, styles[0].ID, styles[0].PromptVersion)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("real PG commands and HTTP routes, strict DTO, P3 edit gate, single scope, cancel and reopened readback passed; no Model/Agent/Runner execution")
}

func TestSingleVisualBrowserInterfacesWithoutModelExecution(t *testing.T) {
	addr := os.Getenv("DRAMA_SINGLE_API_UI_ADDR")
	if addr == "" {
		t.Skip("explicit isolated browser fixture opt-in")
	}
	if addr != "127.0.0.1:8792" {
		t.Fatal("isolated fixed loopback address required")
	}
	dir := os.Getenv("DRAMA_SINGLE_API_UI_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("explicit evidence directory required")
	}
	store := openProjectContentPG(t)
	s := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Store: interfaceNoExecutionStore{store}, Importer: scriptimport.Importer{}, Agent: interfaceOnlyAgent{}, Models: &modelregistry.Registry{Model: interfaceOnlyModel{}}}
	styles, loadErr := stylecatalog.LoadStyles("../../../demo")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	s.Styles = styles
	p := createProjectContent(t, s, pgAssets())
	p.Title = "单项接口 · 虚构免费验证"
	p.Revision++
	if err := store.Commit(context.Background(), p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(context.Background())
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if e := server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			t.Error(e)
		}
	}()
	defer server.Close()
	raw, _ := json.Marshal(map[string]string{"url": "http://" + addr + "/demo/app/#/playlet/" + p.ID + "/characters", "projectId": p.ID})
	if err := os.WriteFile(filepath.Join(dir, "browser-fixture.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("isolated P3-completed view ready; sentinel model/Agent cannot execute, no permit")
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "browser-stop")); err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	read, err := store.Snapshot(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Runs) != 1 || len(read.Runs[0].Calls) != 1 || read.UsedMicroCNY() != 789 {
		t.Fatal("browser created model execution or changed historic reserve")
	}
	raw, _ = json.MarshalIndent(read, "", "  ")
	if err = os.WriteFile(filepath.Join(dir, "browser-pg-final.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestManualVisualPromptPostgresAPIRevisionIsolationAndReadOnly(t *testing.T) {
	ctx := context.Background()
	store := openProjectContentPG(t)
	s := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Store: interfaceNoExecutionStore{store}, Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: interfaceOnlyModel{}}}
	p := createProjectContent(t, s, pgAssets())
	before := d.Clone(p)
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(ctx)
	post := func(suffix string, body any, want int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/api/projects/"+p.ID+suffix, bytes.NewReader(raw)))
		if w.Code != want {
			t.Fatalf("manual %s: %d %s", suffix, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	for _, bad := range []struct {
		path   string
		body   any
		status int
	}{
		{"/assets/c/visual-prompt", map[string]any{"revision": p.Revision - 1, "text": "x"}, 409},
		{"/assets/c/visual-prompt", map[string]any{"revision": p.Revision, "text": " \n"}, 400},
		{"/assets/foreign/visual-prompt", map[string]any{"revision": p.Revision, "text": "x"}, 404},
		{"/assets/c/looks/foreign/visual-prompt", map[string]any{"revision": p.Revision, "text": "x"}, 404},
		{"/assets/c/looks/l/visual-prompt", map[string]any{"revision": p.Revision, "text": "x"}, 409},
		{"/assets/c/visual-prompt", map[string]any{"revision": p.Revision, "text": "x", "origin": "model"}, 400},
	} {
		post(bad.path, bad.body, bad.status)
	}
	unchanged, _ := store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(unchanged, before) {
		t.Fatal("denied prompt edit changed PG")
	}
	for _, target := range []d.VisualTarget{{AssetID: "c"}, {AssetID: "c", LookID: "l"}, {AssetID: "loc"}, {AssetID: "prop"}} {
		path := "/assets/" + target.AssetID
		if target.LookID != "" {
			path += "/looks/" + target.LookID
		}
		path += "/visual-prompt"
		text := "用户完整正文\n保留全部文字 " + target.AssetID + target.LookID
		raw := post(path, map[string]any{"revision": p.Revision, "text": text}, 200)
		p, _ = store.Snapshot(ctx, p.ID)
		assertProjectContentView(t, raw, p)
		if !p.ValidPrompt(p.Prompt(target), "") || p.Prompt(target).Origin != "user" || p.Prompt(target).Text != text || !reflect.DeepEqual(before.Runs, p.Runs) || p.UsedMicroCNY() != before.UsedMicroCNY() {
			t.Fatal("manual save did not preserve target/fees/runs")
		}
	}
	host.ReadOnly = true
	before = d.Clone(p)
	post("/assets/c/visual-prompt", map[string]any{"revision": p.Revision, "text": "must not write"}, 403)
	p, _ = store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(p, before) {
		t.Fatal("readonly prompt write changed PG")
	}
}

func TestMergedPreparationVisualSourceLockPostgresAPI(t *testing.T) {
	store := openProjectContentPG(t)
	ctx := context.Background()
	s := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Store: interfaceNoExecutionStore{store}, Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: interfaceOnlyModel{}}}
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(ctx)
	for _, status := range []d.RunStatus{d.RunCompleted, d.RunFailed, d.RunCancelled} {
		t.Run(string(status), func(t *testing.T) {
			p := createProjectContent(t, s, pgAssets())
			r := p.LatestRun()
			r.Stage = "visualPrompt"
			r.Status = status
			r.Targets = p.PendingVisualTargets()
			p.Revision++
			if err := store.Commit(ctx, p, p.Revision-1); err != nil {
				t.Fatal(err)
			}
			before := d.Clone(p)
			raw, _ := json.Marshal(map[string]any{"revision": p.Revision, "fileId": "file_ffffffffffffffffffffffffffffffff"})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/api/projects/"+p.ID+"/source", bytes.NewReader(raw)))
			if w.Code != 409 || !bytes.Contains(w.Body.Bytes(), []byte(`"SOURCE_LOCKED"`)) {
				t.Fatalf("merged source guard %d %s", w.Code, w.Body.String())
			}
			p, err := store.Snapshot(ctx, p.ID)
			if err != nil || !reflect.DeepEqual(p, before) {
				t.Fatal("source denial changed PG source/catalog/history/fees", err)
			}
		})
	}
}

func TestHistoricalExtractionSourceReplacementDoesNotReuseOldCompletion(t *testing.T) {
	ctx := context.Background()
	store := openProjectContentPG(t)
	s := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Store: interfaceNoExecutionStore{store}, Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: interfaceOnlyModel{}}}
	p := createProjectContent(t, s, pgAssets())
	p.LatestRun().Contract = d.HistoricalCatalogContract
	p.LatestRun().Stage = "correct"
	p.Revision++
	if err := store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	before := d.Clone(p)
	changed, err := s.Upload(ctx, p.ID, p.Revision, "replacement.txt", []byte("另一份完整的虚构剧本。"))
	if err != nil {
		t.Fatal(err)
	}
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/projects/"+p.ID, nil))
	var v struct {
		Run    json.RawMessage
		Assets []d.Asset
		Source struct{ Version uint64 }
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || string(v.Run) != "null" || len(v.Assets) != 0 || v.Source.Version != 2 {
		t.Fatal("historical completion leaked to replacement source", w.Body.String())
	}
	if !reflect.DeepEqual(changed.Runs, before.Runs) || changed.UsedMicroCNY() != before.UsedMicroCNY() {
		t.Fatal("historical evidence/fees changed")
	}
}
