package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/adapter/stylecatalog"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/uploadfiles"
)

// A selection can be saved, but any attempt to execute a model fails this test.
// There is no Eino Runner, provider, credential, permit or generated fixture output.
type interfaceOnlyModel struct{}

func (interfaceOnlyModel) Capacity() (int, int) { return 1000000, 393216 }

func (interfaceOnlyModel) Generate(context.Context, ports.Request, func(json.RawMessage) error) (ports.Response, error) {
	panic("model execution forbidden in API-only checks")
}

type interfaceOnlyAgent struct{}

func (interfaceOnlyAgent) Run(context.Context, ports.AgentSession) error {
	panic("agent execution forbidden in API-only checks")
}

func TestProjectContentInterfacesWithoutModelExecution(t *testing.T) {
	store := openProjectContentPG(t)
	styles, err := stylecatalog.LoadStyles("../../../demo")
	if err != nil {
		t.Fatal(err)
	}
	s := &app.Service{Store: store, Importer: scriptimport.Importer{}, Styles: styles, Agent: interfaceOnlyAgent{}, Models: &modelregistry.Registry{Models: map[string]ports.Model{"interface-analysis": interfaceOnlyModel{}}, OptionsValue: ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "interface-analysis", Name: "Fictional analysis"}}, Image: []ports.ModelOption{{ID: "interface-image", Name: "Fictional image"}}, Video: []ports.ModelOption{{ID: "interface-video", Name: "Fictional video"}}}}}
	files := uploadfiles.New()
	s.Files = files
	host, handler := httpapi.New(configuredFixture(s), nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	host.Files = files
	defer host.Shutdown(context.Background())
	request := func(method, path string, body []byte, contentType string, want int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, bytes.NewReader(body))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	original := []byte("Complete fictional interface script.\nA character enters the station carrying a watch.\n")
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)

	file, _ := form.CreateFormFile("file", "interface.txt")
	_, _ = file.Write(original)
	_ = form.Close()
	fileJSON := request("POST", "/api/upload", upload.Bytes(), form.FormDataContentType(), 201)
	var receipt httpapi.FileView
	if err = json.Unmarshal(fileJSON, &receipt); err != nil {
		t.Fatal(err)
	}
	createJSON, _ := json.Marshal(map[string]any{"fileId": receipt.FileID, "title": "P4 API-only fictional project"})
	raw := request("POST", "/api/projects", createJSON, "application/json", 201)
	var created struct{ ID string }
	if err = json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	p, err := store.Snapshot(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	path := "/api/projects/" + p.ID
	command := func(suffix string, input any, want int) {
		t.Helper()
		body, _ := json.Marshal(input)
		raw = request("POST", path+suffix, body, "application/json", want)
		p, err = store.Snapshot(context.Background(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	settings := d.ProjectSettings{AspectRatio: "16:9", LLMModel: "interface-analysis", ImageModel: "interface-image", VideoModel: "interface-video"}
	command("/settings", map[string]any{"revision": p.Revision, "settings": settings}, 200)
	command("/style", map[string]any{"revision": p.Revision, "styleId": styles[0].ID, "promptVersion": styles[0].PromptVersion}, 200)
	if len(p.Original) != 0 || p.SourceFileID != receipt.FileID || !reflect.DeepEqual(p.ProjectSettings, &settings) || !reflect.DeepEqual(p.Style, &styles[0].ProjectStyle) || len(p.Runs) != 1 || p.Runs[0].Contract != d.EpisodeIndexContract || p.Runs[0].Status != d.RunFailed || len(p.Runs[0].Calls) != 0 || p.UsedMicroCNY() != 0 {
		t.Fatal("ordinary upload/preferences changed source or task/fees")
	}
	// Before visual success, ordinary writes remain closed and task start has no permit.
	command("/assets", map[string]any{"revision": p.Revision, "asset": d.AssetInput{Kind: "prop", Name: "Watch", Aliases: []string{}}}, 409)
	command("/runs", map[string]any{"revision": p.Revision}, 409)
	if len(p.Runs) != 1 {
		t.Fatal("unauthorized start created a run")
	}
	// Seed a saved completed ProjectView for CRUD consumption only. This is not a generation test.
	p.Assets = pgAssets()
	targets := []d.VisualTarget{}
	artifacts := []d.VisualPrompt{}
	for _, a := range p.Assets {
		targets = append(targets, d.VisualTarget{AssetID: a.ID})
		for _, l := range a.Looks {
			targets = append(targets, d.VisualTarget{AssetID: a.ID, LookID: l.ID})
		}
	}
	for _, target := range targets {
		artifacts = append(artifacts, d.VisualPrompt{VisualPromptInput: d.VisualPromptInput{Target: target, Text: "Persisted fictional API prompt", DesignNotes: "Private fixture note"}, SourceVersion: p.ScriptSource.Version, ActionRef: d.ActionRef{RunID: "interface-visual", CallID: "fixture", ActionID: "fixture"}})
	}
	p.Runs = []d.Run{{ID: "interface-p3", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "completed"}, {ID: "interface-visual", Contract: d.VisualContract, SourceVersion: 1, Status: "completed", Stage: "visualPrompt", Targets: targets}}
	p.VisualPrompts = artifacts
	for i := range p.VisualPrompts {
		if p.VisualPrompts[i].Target.LookID != "" {
			base := p.Prompt(d.VisualTarget{AssetID: p.VisualPrompts[i].Target.AssetID}).ActionRef
			p.VisualPrompts[i].BasePrompt = &base
		}
	}
	if !p.VisualComplete(p.LatestRun()) {
		t.Fatal("invalid saved-view fixture")
	}
	p.Revision++
	if err = store.Commit(context.Background(), p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	savedSettings, savedStyle := *p.ProjectSettings, *p.Style
	input := d.AssetInput{Kind: "character", Name: "Interface character", Description: "Full description\nSecond line", Aliases: []string{"Alias"}, Character: &d.Character{RoleType: "support", Age: "32", Gender: "female", Biography: "Full biography", Appearance: "Short hair", Personality: "Calm"}}
	command("/assets/c", map[string]any{"revision": p.Revision, "asset": input}, 200)
	if !reflect.DeepEqual(p.Assets[0].Character, input.Character) || p.Assets[0].Description != input.Description || !p.CatalogEditedByUser {
		t.Fatal("character input lost")
	}
	for _, v := range p.VisualPrompts {
		if v.Target.AssetID == "c" && !v.Stale {
			t.Fatal("related character prompt not invalidated")
		}
		if v.Target.AssetID != "c" && v.Stale {
			t.Fatal("unrelated prompt invalidated")
		}
	}
	command("/assets/c/looks", map[string]any{"revision": p.Revision, "look": d.LookInput{Name: "New look", Description: ""}}, 201)
	look := p.Assets[0].Looks[len(p.Assets[0].Looks)-1]
	for _, v := range p.VisualPrompts {
		if v.Target.LookID == look.ID {
			t.Fatal("new look received generated text")
		}
	}
	command("/assets/c/looks/"+look.ID, map[string]any{"revision": p.Revision, "look": d.LookInput{Name: "Renamed look", Description: "Full additional description"}}, 200)
	command("/assets/loc/looks/"+look.ID, map[string]any{"revision": p.Revision, "look": d.LookInput{Name: "Invalid parent", Description: ""}}, 400)
	command("/assets/c/looks/"+look.ID+"/delete", map[string]any{"revision": p.Revision}, 200)
	for _, kind := range []string{"location", "prop"} {
		command("/assets", map[string]any{"revision": p.Revision, "asset": d.AssetInput{Kind: kind, Name: "New " + kind, Description: "Full description", Aliases: []string{}}}, 201)
		a := p.Assets[len(p.Assets)-1]
		if len(a.Looks) != 0 || a.Character != nil {
			t.Fatal("non-character received looks/character fields")
		}
		command("/assets/"+a.ID, map[string]any{"revision": p.Revision, "asset": d.AssetInput{Kind: kind, Name: "Edited " + kind, Description: "Changed\nFull description", Aliases: []string{"Alias"}}}, 200)
		command("/assets/"+a.ID+"/delete", map[string]any{"revision": p.Revision}, 200)
	}
	stale := d.Clone(p)
	command("/assets/c/delete", map[string]any{"revision": p.Revision - 1}, 409)
	if !reflect.DeepEqual(p, stale) {
		t.Fatal("conflict partially changed data")
	}
	command("/assets/c/delete", map[string]any{"revision": p.Revision}, 200)
	for _, v := range p.VisualPrompts {
		if v.Target.AssetID == "c" {
			t.Fatal("cascade retained removed character prompt")
		}
	}
	if *p.ProjectSettings != savedSettings || !reflect.DeepEqual(*p.Style, savedStyle) || len(p.Original) != 0 || p.SourceFileID != receipt.FileID || p.UsedMicroCNY() != 0 {
		t.Fatal("CRUD changed fixed choices, original or fees")
	}
	assertProjectContentView(t, request("GET", path, nil, "", 200), p)
	reopened := openProjectContentPG(t)
	read, err := reopened.Snapshot(context.Background(), p.ID)
	if err != nil || !reflect.DeepEqual(read, p) {
		t.Fatal("PG reopen differs from API state", err)
	}
	t.Log("upload, fixed settings/style, all ordinary CRUD, invalidation, conflict, cascade and PG/API readback passed; model execution impossible")
}

// Optional temporary browser fixture has ordinary API ports only; no model/Agent/permit.
func TestProjectContentInterfaceBrowserWithoutModels(t *testing.T) {
	addr := os.Getenv("DRAMA_P4_API_UI_ADDR")
	if addr == "" {
		t.Skip("API-only browser fixture opt-in")
	}
	if addr != "127.0.0.1:8791" {
		t.Fatal("fixed isolated loopback address required")
	}
	dir := os.Getenv("DRAMA_P4_API_UI_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("explicit evidence directory required")
	}
	store := openProjectContentPG(t)
	styles, err := stylecatalog.LoadStyles("../../../demo")
	if err != nil {
		t.Fatal(err)
	}
	s := &app.Service{Store: store, Importer: scriptimport.Importer{}, Styles: styles}
	p, err := fixtureProject(t, s, legacyHTTPActor).Create(context.Background(), "API-only fictional saved project", "interface.txt", []byte("A fictional person carries a watch into the station.\n"), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	p.Assets = pgAssets()
	targets := []d.VisualTarget{}
	for _, a := range p.Assets {
		targets = append(targets, d.VisualTarget{AssetID: a.ID})
		for _, l := range a.Looks {
			targets = append(targets, d.VisualTarget{AssetID: a.ID, LookID: l.ID})
		}
	}
	p.Runs = []d.Run{{ID: "p3-fixture", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "completed"}, {ID: "visual-fixture", Contract: d.VisualContract, SourceVersion: 1, Status: "completed", Stage: "visualPrompt", Targets: targets}}
	for _, target := range targets {
		p.VisualPrompts = append(p.VisualPrompts, d.VisualPrompt{VisualPromptInput: d.VisualPromptInput{Target: target, Text: "Saved fictional interface body"}, SourceVersion: 1, ActionRef: d.ActionRef{RunID: "visual-fixture"}})
	}
	for i := range p.VisualPrompts {
		if p.VisualPrompts[i].Target.LookID != "" {
			base := p.Prompt(d.VisualTarget{AssetID: p.VisualPrompts[i].Target.AssetID}).ActionRef
			p.VisualPrompts[i].BasePrompt = &base
		}
	}
	if !p.VisualComplete(p.LatestRun()) {
		t.Fatal("invalid saved-view browser fixture")
	}
	p.Revision++
	if err = store.Commit(context.Background(), p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	host, handler := httpapi.New(configuredFixture(s), nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(context.Background())
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if e := server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			t.Error(e)
		}
	}()
	defer server.Close()
	raw, _ := json.Marshal(map[string]string{"url": "http://" + addr, "projectId": p.ID})
	if err = os.WriteFile(filepath.Join(dir, "browser-fixture.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("API-only browser fixture ready; no model ports")
	// Stop cleanly before go test's default timeout, so test rows are removed.
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err = os.Stat(filepath.Join(dir, "browser-stop")); err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	rows, err := store.ListSummaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == p.ID {
			continue
		}
		p, readErr := store.Snapshot(context.Background(), row.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		cleanProjectContentRow(t, p)
		if len(p.Runs) != 1 || p.Runs[0].Contract != d.EpisodeIndexContract || p.Runs[0].Status != d.RunFailed || len(p.Runs[0].Calls) != 0 || p.UsedMicroCNY() != 0 {
			t.Fatal("upload unexpectedly created task or fees")
		}
	}
}
