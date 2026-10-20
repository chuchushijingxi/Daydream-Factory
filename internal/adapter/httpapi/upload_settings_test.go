package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/uploadfiles"
)

func TestUploadAllPreferencesAndFailedAutomaticIndexPersistWithoutExternalCall(t *testing.T) {
	store := openProjectContentPG(t)
	model := &protocolModel{}
	service := contentService(t, store, model)
	service.Authorize = nil
	service.Models.(*modelregistry.Registry).DefaultModel = "fixture-analysis"
	service.Models.(*modelregistry.Registry).Models = map[string]ports.Model{"fixture-analysis": model}
	service.Models.(*modelregistry.Registry).OptionsValue = ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "fixture-analysis", Name: "Fictional analysis"}}, Image: []ports.ModelOption{{ID: "fixture-image", Name: "Fictional image"}}, Video: []ports.ModelOption{{ID: "fixture-video", Name: "Fictional video"}}}
	files := uploadfiles.New()
	service.Files = files
	host, handler := httpapi.New(service, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	host.Files = files
	server := httptest.NewServer(handler)
	defer server.Close()
	defer host.Shutdown(context.Background())
	raw := []byte("Complete fictional script.\nA person picks up a watch.\nEnd of script.\n")
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "fictional.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	uploaded, err := http.Post(server.URL+"/api/upload", form.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	var receipt httpapi.FileView
	if uploaded.StatusCode != 201 || json.NewDecoder(uploaded.Body).Decode(&receipt) != nil {
		t.Fatal("file upload failed")
	}
	uploaded.Body.Close()
	createJSON, _ := json.Marshal(map[string]any{"fileId": receipt.FileID, "title": "Fictional upload preferences", "billingBasis": quoteUploadedContent(t, server.URL, receipt.FileID, d.ProjectSettings{})})
	response, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(createJSON))
	if err != nil {
		t.Fatal(err)
	}
	result, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 201 {
		t.Fatalf("upload %d %s", response.StatusCode, result)
	}
	var projection struct {
		ID       string
		Revision uint64
	}
	if err = json.Unmarshal(result, &projection); err != nil {
		t.Fatal(err)
	}
	p, err := store.Snapshot(context.Background(), projection.ID)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	if len(p.Original) != 0 || p.SourceFileID != receipt.FileID || p.UploadedAt == nil {
		t.Fatal("original upload not persisted")
	}
	settings := d.ProjectSettings{AspectRatio: "16:9", LLMModel: "fixture-analysis", ImageModel: "fixture-image", VideoModel: "fixture-video"}
	command := func(path string, input any, want int) {
		t.Helper()
		code, result := postProjectContent(t, server.URL, "/api/projects/"+p.ID+path, input)
		if code != want {
			t.Fatalf("%s: %d %s", path, code, result)
		}
		p, err = store.Snapshot(context.Background(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	command("/settings", map[string]any{"revision": p.Revision, "settings": settings}, 200)
	style := service.Styles[0].ProjectStyle
	command("/style", map[string]any{"revision": p.Revision, "styleId": style.ID, "promptVersion": style.PromptVersion}, 200)
	saved := d.Clone(p)
	if !reflect.DeepEqual(p.ProjectSettings, &settings) || !reflect.DeepEqual(p.Style, &style) || len(p.Runs) != 1 || p.Runs[0].Contract != d.EpisodeIndexContract || p.Runs[0].Status != d.RunFailed || len(p.Runs[0].Calls) != 0 || p.UsedMicroCNY() != 0 || model.calls.Load() != 0 {
		t.Fatal("ordinary save lost values or invoked model")
	}
	// Reopen the real adapter to rule out process-local state.
	reopened := openProjectContentPG(t)
	read, err := reopened.Snapshot(context.Background(), p.ID)
	if err != nil || !reflect.DeepEqual(read, saved) {
		t.Fatal("reopened PG changed saved choices", err)
	}
	res, err := http.Get(server.URL + "/api/projects/" + p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var view struct {
		ProjectSettings *d.ProjectSettings `json:"settings"`
		Style           *d.ProjectStyle
		VisualPrompts   []any
		Runs            []any
	}
	if err = json.NewDecoder(res.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.ProjectSettings, &settings) || !reflect.DeepEqual(view.Style, &style) || len(view.Runs) != 1 || len(view.VisualPrompts) != 0 {
		t.Fatal("API readback inconsistent")
	}
	command("/settings", map[string]any{"revision": p.Revision, "settings": d.ProjectSettings{AspectRatio: "9:16"}}, 409)
	command("/style", map[string]any{"revision": p.Revision, "styleId": service.Styles[1].ID, "promptVersion": service.Styles[1].PromptVersion}, 409)
	command("/assets", map[string]any{"revision": p.Revision, "asset": d.AssetInput{Kind: "prop", Name: "Fictional watch", Aliases: []string{}}}, 409)
	command("/outline-runs", map[string]any{"revision": p.Revision}, 403)
	if !reflect.DeepEqual(p, saved) || model.calls.Load() != 0 {
		t.Fatal("rejection changed data, reserved budget or invoked model")
	}
}
