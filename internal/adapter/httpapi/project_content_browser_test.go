package httpapi_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	modeladapter "drama-agent/internal/adapter/model"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

// Explicitly opt-in browser fixture. It serves the production handler, store,
// Eino runner and model adapter; the provider is strictly local and fictional.
// Nothing from this test is assembled by cmd/server.
func TestProjectContentBrowserIntegrationFixture(t *testing.T) {
	addr := os.Getenv("DRAMA_P4_UI_TEST_ADDR")
	if addr == "" {
		t.Skip("browser fixture opt-in")
	}
	ip, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(ip) == nil || !net.ParseIP(ip).IsLoopback() {
		t.Fatal("loopback address required")
	}
	dir := os.Getenv("DRAMA_P4_UI_TEST_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("explicit evidence directory required")
	}
	store := openProjectContentPG(t)
	var providerCalls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		var wire struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", 400)
			return
		}
		var input struct {
			ScriptSource []d.SourceSpan `json:"source"`
			Assets       []d.Asset      `json:"assets"`
		}
		correct := false
		for _, m := range wire.Messages {
			if m.Role == "system" && m.Content == app.CorrectPrompt {
				correct = true
			}
			if m.Role == "user" {
				if err := json.Unmarshal([]byte(m.Content), &input); err != nil {
					t.Error(err)
				}
			}
		}
		saved := false
		for _, m := range wire.Messages {
			if m.Role == "tool" {
				var receipt struct {
					OK      bool              `json:"ok"`
					Changes []json.RawMessage `json:"changes"`
				}
				_ = json.Unmarshal([]byte(m.Content), &receipt)
				if receipt.OK && len(receipt.Changes) > 0 {
					saved = true
				}
			}
		}
		if len(wire.Tools) == 0 {
			http.Error(w, "no tools", 400)
			return
		}
		switch wire.Tools[0].Function.Name {
		case "save_outline":
			if saved {
				emitProjectContent(w, []d.Action{contentAction("finish_outline", struct{}{})})
				return
			}
			emitProjectContent(w, []d.Action{contentAction("save_outline", app.SaveOutline{Outline: d.OutlineInput{Protagonist: "林岚，修理师", Genre: "虚构测试故事", Summary: "林岚在修理间用铜钥匙开门。", Background: "修理间", Logline: "一把钥匙打开修理间的门。"}})})
		case "save_catalog":
			if saved || correct {
				emitProjectContent(w, []d.Action{contentAction("finish_batch", struct{}{})})
				return
			}
			assets := []d.Asset{{Kind: "character", Name: "林岚", Description: "32岁修理师，黑发低马尾，左眉尾浅疤", SourceIDs: []string{input.ScriptSource[0].ID}}, {Kind: "location", Name: "修理间", Description: "完整空间描述：工作台和北侧门", SourceIDs: []string{input.ScriptSource[0].ID}}, {Kind: "prop", Name: "铜钥匙", Description: "圆形匙头，齿部缺口", SourceIDs: []string{input.ScriptSource[0].ID}}}
			emitProjectContent(w, []d.Action{contentAction("save_catalog", map[string]any{"assets": assets})})
		case "save_visual_prompts":
			if saved {
				emitProjectContent(w, []d.Action{contentAction("finish_visual_prompts", struct{}{})})
				return
			}
			prompts := []app.VisualPromptInput{}
			for _, a := range input.Assets {
				text := map[string]string{"character": "林岚，32岁修理师，黑发低马尾，左眉尾浅疤，深蓝工装。单人形象，背景简洁。", "location": "修理间环境设定：木工作台，北侧门，完整空间布局。", "prop": "铜钥匙物件本体，圆形匙头，齿部缺口。"}[a.Kind]
				prompts = append(prompts, app.VisualPromptInput{Target: app.VisualTargetInput{AssetID: a.ID}, Text: text, DesignNotes: "免费夹具的虚构设计说明"})
				for _, l := range a.Looks {
					prompts = append(prompts, app.VisualPromptInput{Target: app.VisualTargetInput{AssetID: a.ID, LookID: l.ID}, Text: "林岚，32岁，黑发低马尾，左眉尾浅疤，黑礼服。", DesignNotes: "虚构附加造型"})
				}
			}
			emitProjectContent(w, []d.Action{contentAction("save_visual_prompts", app.SaveVisualPrompts{Prompts: prompts})})
		default:
			http.Error(w, "unexpected tools", 400)
		}
	}))
	defer provider.Close()
	model, err := modeladapter.New(context.Background(), "fictional-loopback-ui-key", provider.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	s := contentService(t, store, model)
	fixtures := map[string]string{}
	// A naturally executed local fixture provides real persisted P3/visual
	// call receipts for the project process page. No external provider is used.
	processProject, err := s.ForActor(legacyHTTPActor).Create(context.Background(), "免费联调 · 提取过程", "process.txt", []byte("林岚，32岁修理师，黑发低马尾，左眉尾浅疤。\n她在修理间用铜钥匙开门。\n晚宴上穿黑礼服。"), 0)
	if err == nil {
		processProject, err = quotedLegacyService(t, s, processProject.ID, app.QuoteInput{Revision: processProject.Revision, Operation: "asset-preparation"}).Start(context.Background(), processProject.ID, processProject.Revision)
	}
	if err == nil {
		err = s.Execute(context.Background(), processProject.ID)
	}
	if err == nil {
		processProject, err = store.Snapshot(context.Background(), processProject.ID)
	}
	if err == nil {
		processProject, err = quotedLegacyService(t, s, processProject.ID, app.QuoteInput{Revision: processProject.Revision, Operation: "visual"}).StartVisual(context.Background(), processProject.ID, processProject.Revision)
	}
	if err == nil {
		err = s.Execute(context.Background(), processProject.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	fixtures["process"] = processProject.ID
	for _, kind := range []string{"ready", "empty", "failed", "cancelled", "partial"} {
		assets := pgAssets()
		if kind == "empty" {
			assets = nil
		}
		p := createProjectContent(t, s, assets)
		p.Title = "免费联调 · " + kind
		if kind == "failed" || kind == "cancelled" || kind == "partial" {
			status := map[string]string{"failed": "failed", "cancelled": "cancelled", "partial": "running"}[kind]
			r := d.Run{ID: identity.New("visual"), Contract: d.VisualContract, SourceVersion: 1, Status: d.RunStatus(status), Stage: "visualPrompt", Targets: []d.VisualTarget{{AssetID: "c"}, {AssetID: "c", LookID: "l"}, {AssetID: "loc"}, {AssetID: "prop"}}}
			if kind == "failed" {
				r.ErrorCode = "PROTOCOL_REJECTED"
				r.Error = "虚构测试：视觉格式错误，任务终结。"
			}
			p.Runs = append(p.Runs, r)
			p.VisualPrompts = []d.VisualPrompt{{VisualPromptInput: d.VisualPromptInput{Target: d.VisualTarget{AssetID: "c"}, Text: "虚构部分正文，仅供查看"}, SourceVersion: 1, ActionRef: d.ActionRef{RunID: r.ID, CallID: "fixture", ActionID: "fixture"}}}
		}
		p.Revision++
		if err := store.Commit(context.Background(), p, p.Revision-1); err != nil {
			t.Fatal(err)
		}
		fixtures[kind] = p.ID
	}
	owner, handler := httpapi.New(s, nil, "../../../demo", 100000000)
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close(); _ = owner.Shutdown(context.Background()) }()
	raw, _ := json.MarshalIndent(map[string]any{"url": "http://" + addr, "projects": fixtures, "provider": "loopback fixture only"}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "ui-fixtures.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("browser fixture ready", addr)
	for {
		if _, err := os.Stat(filepath.Join(dir, "ui-stop")); err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	projects, err := store.ListSummaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshots := []d.Project{}
	for _, row := range projects {
		p, err := store.Snapshot(context.Background(), row.ID)
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, p)
		snapshot, readErr := store.Snapshot(context.Background(), p.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		cleanProjectContentRow(t, snapshot)
	}
	raw, _ = json.MarshalIndent(map[string]any{"providerCalls": providerCalls.Load(), "realSupplierCalls": 0, "projects": snapshots}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "pg-final-snapshots.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
