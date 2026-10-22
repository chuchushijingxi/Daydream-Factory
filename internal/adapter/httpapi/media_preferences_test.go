package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	creditsdomain "drama-agent/internal/domain/credits"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

func TestMediaPreferenceHTTPAndPGReadbackWithoutModelExecution(t *testing.T) {
	ctx := context.Background()
	store := openProjectContentPG(t)
	cipher, err := postgres.OpenModelCipher(filepath.Join(t.TempDir(), "fictional-model.key"), true)
	if err != nil {
		t.Fatal(err)
	}
	factoryCalls := 0
	c := &modelconfig.Service{Store: store, Projects: store, Cipher: cipher, Factory: func(_ context.Context, _, _, _ string, m md.ConfiguredModel) (ports.Model, error) {
		factoryCalls++
		if m.Category != "analysis" {
			t.Fatal("media preference constructed an execution adapter")
		}
		return interfaceOnlyModel{}, nil
	}}
	s := &app.Service{Store: store, Importer: scriptimport.Importer{}, Models: c}
	cfg, err := store.ModelConfigSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original := cfg
	t.Cleanup(func() {
		current, err := store.ModelConfigSnapshot(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		original.Revision = current.Revision + 1
		if err = store.ModelConfigCommit(context.Background(), original, current.Revision); err != nil {
			t.Error(err)
		}
	})
	conn, ok := md.OfficialPreset("volcengine")
	if !ok {
		t.Fatal("official fixture missing")
	}
	on, off, key := true, false, "fictional-local-only-media-key"
	input := modelconfig.ModelConfigInput{Revision: cfg.Revision, APIKey: &key, Connection: modelconfig.ConnectionInput{ID: conn.ID, Name: conn.Name, Kind: conn.Kind, Protocol: conn.Protocol, BaseURL: conn.BaseURL}}
	var imageID, videoID string
	for _, m := range conn.Models {
		enabled := &off
		if m.Category == "image" && imageID == "" {
			enabled, imageID = &on, m.ID
		}
		if m.Category == "video" && videoID == "" {
			enabled, videoID = &on, m.ID
		}
		input.Connection.Models = append(input.Connection.Models, modelconfig.ModelEnableInput{ID: m.ID, ModelID: m.ModelID, Category: m.Category, Enabled: enabled, CreditPrice: &creditsdomain.PriceRule{UnitPrice: 0}, ContextWindow: m.ContextWindow, MaxOutputTokens: m.MaxOutputTokens, Pricing: m.Pricing})
	}
	view, err := c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range view.Connections {
		for _, m := range connection.Models {
			if m.Category != "analysis" && m.Available {
				t.Fatal("media execution advertised")
			}
		}
	}
	configBefore, _ := store.ModelConfigSnapshot(ctx)
	p, err := fixtureProject(t, s, legacyHTTPActor).Create(ctx, "Fictional media preference", "fictional.txt", []byte("Full fictional script; no generation."), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	host, handler := httpapi.New(configuredFixture(s), c, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(ctx)
	request := func(method, path string, body any, want int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	var options ports.ModelOptions
	if err = json.Unmarshal(request("GET", "/api/models", nil, 200), &options); err != nil || len(options.Image) != 1 || len(options.Video) != 1 || options.Image[0].ID != imageID || options.Video[0].ID != videoID {
		t.Fatal("media candidate DTO missing", options, err)
	}
	settings := d.ProjectSettings{AspectRatio: "9:16", ImageModel: imageID, VideoModel: videoID}
	path := "/api/projects/" + p.ID + "/settings"
	body := map[string]any{"revision": 1, "settings": settings}
	request("POST", path, body, 200)
	saved, err := store.Snapshot(ctx, p.ID)
	if err != nil || saved.ProjectSettings == nil || *saved.ProjectSettings != settings || saved.Revision != 2 || len(saved.Runs) != 0 || saved.UsedMicroCNY() != 0 || factoryCalls != 0 {
		t.Fatal("preference save changed execution state", saved, err)
	}
	request("POST", path, body, 409)
	changed := settings
	changed.VideoModel = ""
	request("POST", path, map[string]any{"revision": 2, "settings": changed}, 409)
	request("GET", "/api/projects/"+p.ID, nil, 200)
	reopened := openProjectContentPG(t)
	readback, err := reopened.Snapshot(ctx, p.ID)
	if err != nil || !reflect.DeepEqual(readback, saved) {
		t.Fatal("reopened PG changed preference", err)
	}
	configAfter, _ := store.ModelConfigSnapshot(ctx)
	if !reflect.DeepEqual(configBefore, configAfter) {
		t.Fatal("project preference mutated model configuration")
	}
	t.Log("registered media IDs selected and fixed in PG/API; no Model.Generate, Runner, Call, reservation or fee")
}
