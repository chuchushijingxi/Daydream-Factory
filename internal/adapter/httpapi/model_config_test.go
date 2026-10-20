package httpapi_test

import (
	"bytes"
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/adapter/httpapi"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"

	"github.com/jackc/pgx/v5"
)

func TestModelConfigurationHTTPPGAndActualDeepSeekTransport(t *testing.T) {
	for _, modelID := range []string{md.DeepSeekModelID, md.DeepSeekProModelID} {
		t.Run(modelID, func(t *testing.T) { testModelConfigurationHTTPPG(t, modelID) })
	}
}

func testModelConfigurationHTTPPG(t *testing.T, modelID string) {
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("isolated PG opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, e := pgfixture.Open(t, ctx, database)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	original, e := store.ModelConfigSnapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		current, e := store.ModelConfigSnapshot(context.Background())
		if e == nil {
			original.Revision = current.Revision + 1
			if e = store.ModelConfigCommit(context.Background(), original, current.Revision); e != nil {
				t.Error(e)
			}
		}
	}()
	// 测试独立建立固定配置，再恢复原快照；不依赖上轮 PG 媒体模型开关。
	fixture := md.DefaultModelConfig()
	fixture.Revision = original.Revision + 1
	if e = store.ModelConfigCommit(ctx, fixture, original.Revision); e != nil {
		t.Fatal(e)
	}
	cipher, e := postgres.OpenModelCipher(filepath.Join(t.TempDir(), "key"), true)
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	var metadata atomic.Int32
	var auth atomic.Bool
	var selected atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/models" {
			metadata.Add(1)
			if r.Header.Get("Authorization") != "Bearer dummy-contract-key" {
				t.Error("directory did not use shared key")
			}
			io.WriteString(w, `{"data":[{"id":"fixture-text"},{"id":"fixture-image"}]}`)
			return
		}
		calls.Add(1)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		selected.Store(body.Model == modelID)
		auth.Store(r.Header.Get("Authorization") == "Bearer dummy-contract-key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"message":"local fixture rejects request","type":"authentication_error","code":"invalid_api_key"}}`)
	}))
	defer upstream.Close()
	config := &modelconfig.Service{Store: store, Projects: store, Cipher: cipher, Logos: store, Directory: modeladapter.FetchModelDirectory, Factory: func(ctx context.Context, key, url, protocol string, profile md.ConfiguredModel) (ports.Model, error) {
		return modeladapter.NewConfigured(ctx, key, url, protocol, profile)
	}}
	service := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Store: store, Agent: einoagent.New(), Models: config}
	host, handler := httpapi.New(service, config, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "operations")
	server := httptest.NewServer(handler)
	defer server.Close()
	defer host.Shutdown(ctx)
	request := func(method, path string, payload any) (int, []byte) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			raw, _ := json.Marshal(payload)
			body = bytes.NewReader(raw)
		}
		r, _ := http.NewRequest(method, server.URL+path, body)
		r.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "dummy-contract-key") {
			t.Fatal("credential leaked in response")
		}
		return res.StatusCode, raw
	}
	status, raw := request("GET", "/api/model-config", nil)
	var view modelconfig.ModelConfigView
	if status != 200 || json.Unmarshal(raw, &view) != nil {
		t.Fatal(status, string(raw))
	}
	connection := md.DefaultModelConfig().Connections[0]
	modelInputs := []map[string]any{}
	for _, m := range connection.Models {
		modelInputs = append(modelInputs, map[string]any{"id": "", "modelId": m.ModelID, "alias": "", "description": "", "resolutionEnabled": nil, "creditPrice": map[string]any{"unitPrice": 0, "freeEnabled": false, "freeStart": nil, "freeEnd": nil}, "category": m.Category, "enabled": true, "contextWindow": m.ContextWindow, "maxOutputTokens": m.MaxOutputTokens, "pricing": m.Pricing})
	}
	// Upload is local; the registry is unchanged until explicit save.
	img := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	img.SetNRGBA(2, 2, color.NRGBA{10, 180, 220, 255})
	var pngBody bytes.Buffer
	if e = png.Encode(&pngBody, img); e != nil {
		t.Fatal(e)
	}
	res, e := http.Post(server.URL+"/api/model-logos", "image/png", &pngBody)
	if e != nil {
		t.Fatal(e)
	}
	var uploaded modelconfig.LogoView
	if res.StatusCode != 201 || json.NewDecoder(res.Body).Decode(&uploaded) != nil {
		t.Fatal("logo upload failed", res.StatusCode)
	}
	res.Body.Close()
	defer func() {
		conn, e := pgx.Connect(context.Background(), database)
		if e == nil {
			defer conn.Close(context.Background())
			_, e = conn.Exec(context.Background(), "DELETE FROM drama_catalog_v1.model_logos WHERE id=$1", uploaded.LogoID)
			if e != nil {
				t.Error(e)
			}
		}
	}()
	status, pngBytes := request("GET", uploaded.LogoURL, nil)
	decoded, e := png.Decode(bytes.NewReader(pngBytes))
	if status != 200 || e != nil || decoded.Bounds().Dx() != 96 {
		t.Fatal("logo readback failed", status, e)
	}
	modelInputs[0]["logoId"] = uploaded.LogoID
	connPayload := map[string]any{"id": "", "name": "回环中转站", "kind": "gateway", "protocol": "deepseek", "baseUrl": upstream.URL, "models": modelInputs}
	payload := map[string]any{"revision": view.Revision, "connection": connPayload, "apiKey": "dummy-contract-key"}

	status, raw = request("POST", "/api/model-config", payload)
	if status != 200 || json.Unmarshal(raw, &view) != nil || !view.Connections[len(view.Connections)-1].Models[0].Available || !view.Connections[len(view.Connections)-1].CredentialConfigured {
		t.Fatal(status, string(raw))
	}
	gateway := view.Connections[len(view.Connections)-1]
	connPayload["id"] = gateway.ID
	selectedID := ""
	for i, m := range gateway.Models {
		modelInputs[i]["id"] = m.ID
		if m.ModelID == modelID {
			selectedID = m.ID
		}
	}
	status, raw = request("POST", "/api/model-config/models/discover", map[string]any{"revision": view.Revision, "connectionId": gateway.ID, "baseUrl": upstream.URL, "protocol": "deepseek"})
	var directory modelconfig.DiscoverModelsView
	if status != 200 || json.Unmarshal(raw, &directory) != nil || len(directory.Models) != 2 || metadata.Load() != 1 || calls.Load() != 0 {
		t.Fatal("explicit directory request failed", status, string(raw))
	}
	reloaded, e := store.ModelConfigSnapshot(ctx)
	if e != nil || reloaded.Revision != view.Revision || reloaded.Connections[len(reloaded.Connections)-1].Models[0].LogoID != uploaded.LogoID {
		t.Fatal("logo config not persistent or discovery mutated registry", e)
	}
	status, raw = request("GET", "/api/models", nil)
	var options ports.ModelOptions
	if status != 200 || json.Unmarshal(raw, &options) != nil || len(options.Analysis) != 2 || len(options.Image) != 0 || len(options.Video) != 0 {
		t.Fatal(status, string(raw))
	}
	if calls.Load() != 0 {
		t.Fatal("configuration save made supplier call")
	}
	status, _ = request("POST", "/api/model-config", payload)
	if status != 409 {
		t.Fatal("stale revision accepted", status)
	}
	bad := map[string]any{"revision": view.Revision, "connection": connPayload, "apiKey": "dummy-contract-key", "extra": true}
	status, _ = request("POST", "/api/model-config", bad)
	if status != 400 {
		t.Fatal("unknown field accepted")
	}
	p := d.Project{OwnerID: legacyHTTPActor.UserID, ID: identity.New("config_http_test"), Revision: 1, ScriptSource: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "fiction", Text: "虚构模型配置运输测试文本。"}}}, Assets: []d.Asset{}, Runs: []d.Run{}, ProjectSettings: &d.ProjectSettings{LLMModel: selectedID}}
	if e = store.Create(ctx, p); e != nil {
		t.Fatal(e)
	}
	defer func() {
		conn, e := pgx.Connect(context.Background(), database)
		if e == nil {
			defer conn.Close(context.Background())
			if _, e = conn.Exec(context.Background(), "DELETE FROM drama_catalog_v1.projects WHERE id=$1", p.ID); e != nil {
				t.Error(e)
			}
		}
	}()
	status, _ = request("POST", "/api/projects/"+p.ID+"/outline-runs", map[string]any{"revision": 1})
	if status != 403 || calls.Load() != 0 {
		t.Fatal("key configuration authorized execution", status)
	}
	service.Authorize = freeAuthorization // Only the loopback protocol fixture is authorized.
	status, _ = request("POST", "/api/projects/"+p.ID+"/outline-runs", map[string]any{"revision": 1, "billingBasis": legacyQuote(t, service, p.ID, app.QuoteInput{Revision: 1, Operation: "outline"})})
	if status != 202 {
		t.Fatal("task not accepted", status)
	}
	var result d.Project
	for i := 0; i < 200; i++ {
		result, e = store.Snapshot(ctx, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		if result.LatestRun().Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if result.LatestRun().Status != "failed" || calls.Load() != 1 || !auth.Load() || !selected.Load() {
		t.Fatal("configured URL/key were not used by real adapter", calls.Load(), result.LatestRun().Status)
	}
	if result.ProjectSettings.LLMModel != selectedID {
		t.Fatal("fixed project choice changed")
	}
	for _, m := range modelInputs {
		m["enabled"] = false
	}
	status, raw = request("POST", "/api/model-config", map[string]any{"revision": view.Revision, "connection": connPayload})
	if status != 200 || json.Unmarshal(raw, &view) != nil || view.Connections[len(view.Connections)-1].Models[0].Available || !view.Connections[len(view.Connections)-1].CredentialConfigured {
		t.Fatal(status, string(raw))
	}
	status, raw = request("GET", "/api/models", nil)
	if status != 200 || json.Unmarshal(raw, &options) != nil || len(options.Analysis) != 0 {
		t.Fatal(status, string(raw))
	}
	config.ReadOnly = true
	host.ReadOnly = true
	status, _ = request("POST", "/api/model-config", map[string]any{"revision": view.Revision, "connection": connPayload})
	if status != 403 {
		t.Fatal("readonly config write admitted")
	}
	if calls.Load() != 1 {
		t.Fatal("switch called supplier")
	}
}
