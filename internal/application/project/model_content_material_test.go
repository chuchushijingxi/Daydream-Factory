package project

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	modeladapter "drama-agent/internal/adapter/model"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

func materialObject(t *testing.T, value any) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(mustJSON(value), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func rejectMaterialFields(t *testing.T, value any, fields ...string) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			for _, forbidden := range fields {
				if key == forbidden {
					t.Errorf("model material contains %s", key)
				}
			}
			rejectMaterialFields(t, child, fields...)
		}
	case []any:
		for _, child := range v {
			rejectMaterialFields(t, child, fields...)
		}
	}
}

func compactStoryboardFixture(t *testing.T) (*Service, d.Project) {
	t.Helper()
	s, _, p := p5Prepared(t)
	p, _ = p5Change(t, s, p,
		map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))},
		map[string]any{"op": "createScene", "episodeId": "e2", "value": p5Scene(*p.Storyboard.Episode("e2"))})
	for _, episode := range p.Storyboard.Index.Episodes {
		p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": episode.SceneIDs[0], "value": p5Shot(episode)})
	}
	p.Assets[0].ImageFileID = "uploaded-image-sentinel"
	p.Assets[0].Looks[0].ImageFileID = "look-image-sentinel"
	var err error
	p, err = s.commit(context.Background(), p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestModelStoryboardMaterialKeepsBusinessContentAndPageView(t *testing.T) {
	s, p := compactStoryboardFixture(t)
	ctx := context.Background()
	shotID := p.Storyboard.Shots[0].ID
	p, err := s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "regeneratePrompts", ShotIDs: []string{shotID}})
	if err != nil {
		t.Fatal(err)
	}
	before := d.Clone(p)
	public, err := s.StoryboardEpisode(p, "e1")
	if err != nil || public.PlainText == "" || public.Shots[0].VideoPrompt.PlainText == "" || public.Shots[0].SelectedAssets[0].ThumbnailURL == "" || public.ActiveRun == nil {
		t.Fatal("fixture must include real page-only copies and image decoration", err)
	}
	material, err := s.storyboardMaterial(p)
	if err != nil {
		t.Fatal(err)
	}
	got := materialObject(t, material["currentStoryboard"])
	rejectMaterialFields(t, got, "plainText", "activeRun", "latestRun", "access", "thumbnailUrl", "videoTask", "previewDurationSeconds", "shotNumber", "sceneNumber", "projectId", "revision", "assetsLocked")
	page := materialObject(t, public)
	want := map[string]any{}
	for _, key := range []string{"sourceVersion", "episode", "scenes", "shots", "index"} {
		want[key] = page[key]
	}
	if !reflect.DeepEqual(public.Index.SupportRanges, append([]d.SourceRange{}, p.Storyboard.Index.SupportRanges...)) {
		t.Fatal("page source cache identity differs from authoritative ranges")
	}
	if _, exists := got["index"].(map[string]any)["supportRanges"]; exists {
		t.Fatal("page cache identity leaked into model material")
	}
	delete(want["index"].(map[string]any), "supportRanges")
	for _, raw := range want["scenes"].([]any) {
		delete(raw.(map[string]any), "sceneNumber")
	}
	for _, raw := range want["shots"].([]any) {
		shot := raw.(map[string]any)
		delete(shot, "shotNumber")
		for _, raw := range shot["selectedAssets"].([]any) {
			delete(raw.(map[string]any), "thumbnailUrl")
		}
		body := shot["videoPrompt"].(map[string]any)
		delete(body, "plainText")
		for _, raw := range body["parts"].([]any) {
			delete(raw.(map[string]any), "thumbnailUrl")
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("projection changed scene/shot content, identity, ordering, basis or diagnostics", string(mustJSON(got)), string(mustJSON(want)))
	}
	request, _, err := s.storyboardRequest(ctx, p)
	if err != nil || !strings.Contains(request.Input, p.ScriptSource.Spans[0].Text) || strings.Contains(request.Input, p.ScriptSource.Spans[1].Text) {
		t.Fatal("target source lost or foreign episode supplied", err)
	}
	if !strings.Contains(request.Input, `"spanId":"p1"`) || !strings.Contains(request.CurrentState, `"assetId":"c"`) {
		t.Fatal("source alias or real asset reference missing")
	}
	afterPublic, _ := s.StoryboardEpisode(p, "e1")
	if !reflect.DeepEqual(before, p) || !reflect.DeepEqual(public, afterPublic) {
		t.Fatal("model input mutated project or public API view")
	}
	writeContentMaterialEvidence(t, "storyboard-projection.json", map[string]any{"publicView": public, "actualRequest": request, "compactBoardBytes": len(mustJSON(got)), "pageBoardBytes": len(mustJSON(public))})
}

func TestModelStoryboardMaterialRetainsMissingReferencesAndRunValidity(t *testing.T) {
	s, p := compactStoryboardFixture(t)
	ep := *p.Storyboard.Episode("e1")
	shot := p.Storyboard.Shot(p.Storyboard.Scene(ep.SceneIDs[0]).ShotIDs[0])
	p.Runs = append(p.Runs, d.Run{ID: "in-progress", Contract: d.StoryboardContract, Status: d.RunRunning})
	shot.VideoPrompt.Origin = "model"
	shot.VideoPrompt.ActionRef = &d.ActionRef{RunID: "in-progress", CallID: "retained-call", ActionID: "retained-action"}
	public, _ := s.StoryboardEpisode(p, ep.ID)
	got := storyboardEpisodeModelMaterial(p, ep, "in-progress")
	if public.Shots[0].VideoPrompt.Valid || !got.Shots[0].VideoPrompt.Valid {
		t.Fatal("public completion and current-task validity were conflated")
	}
	shot.VideoPrompt.Basis.ShotInputDigest = "changed-input"
	got = storyboardEpisodeModelMaterial(p, ep, "in-progress")
	if got.Shots[0].VideoPrompt.Valid || !strings.Contains(string(mustJSON(got)), "SHOT_INPUT_CHANGED") {
		t.Fatal("real input invalidity hidden by current-run exception")
	}
	shot.SelectedAssets = append(shot.SelectedAssets, d.VisualTarget{AssetID: "deleted-identity"})
	shot.VideoPrompt.Parts = append(shot.VideoPrompt.Parts, d.PromptPart{Type: "asset", AssetID: "deleted-identity", LabelAtSave: "原称谓"})
	got = storyboardEpisodeModelMaterial(p, ep, "in-progress")
	last := got.Shots[0].VideoPrompt.Parts[len(got.Shots[0].VideoPrompt.Parts)-1]
	if last.AssetID != "deleted-identity" || last.Available == nil || *last.Available || last.Label != "原称谓（已删除）" || !strings.Contains(string(mustJSON(got)), "ASSET_MISSING") {
		t.Fatal("missing identity or its diagnostic was silently dropped/rebound")
	}
}

func TestModelVisualMaterialPreservesDependenciesAndActualReceipts(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	base, look := d.VisualTarget{AssetID: "c"}, d.VisualTarget{AssetID: "c", LookID: "l"}
	p = singleStart(t, s, p, base)
	p = singleSave(t, s, p, base, "完整基础正文😀，p1 是内容")
	p = singleFinish(t, s, p)
	baseRef := p.Prompt(base).ActionRef
	p = singleStart(t, s, p, look)
	request, _, err := s.contentRequest(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var input struct{ VisualPrompts []visualPromptMaterial }
	if json.Unmarshal([]byte(request.Input), &input) != nil || len(input.VisualPrompts) != 1 || !input.VisualPrompts[0].Valid || input.VisualPrompts[0].Text != "完整基础正文😀，p1 是内容" || input.VisualPrompts[0].DesignNotes == "" {
		t.Fatal("valid cross-run base or full design notes missing")
	}
	rejectMaterialFields(t, materialObject(t, input.VisualPrompts[0]), "runId", "callId", "actionId", "basePrompt")
	p = singleSave(t, s, p, look, "附加完整正文，沿用准确基础")
	before := d.Clone(p)
	request, _, err = s.contentRequest(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(request.Input), &input)
	if len(input.VisualPrompts) != 2 || !input.VisualPrompts[1].Valid || *p.Prompt(look).BasePrompt != baseRef {
		t.Fatal("current-run look validity or stored base reference lost")
	}
	if !strings.Contains(request.Dialogue[len(request.Dialogue)-1].Content, `"basePrompt":`) || !strings.Contains(request.Dialogue[len(request.Dialogue)-1].Content, baseRef.CallID) || !reflect.DeepEqual(before, p) {
		t.Fatal("actual receipt was compacted or stored provenance changed")
	}
	// 引用不匹配即使 stale 尚未置位仍须无效，不能只投影 !stale。
	broken := d.Clone(p)
	broken.Prompt(look).BasePrompt = &d.ActionRef{RunID: "different-base"}
	brokenRequest, _, err := s.contentRequest(context.Background(), broken)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(brokenRequest.Input), &input)
	if input.VisualPrompts[1].Valid || input.VisualPrompts[1].Stale {
		t.Fatal("hidden dependency mismatch treated as valid or repaired by projection")
	}
	p = singleFinish(t, s, p)
	p = singleStart(t, s, p, base)
	p = singleSave(t, s, p, base, "修订后的基础正文")
	if !p.Prompt(look).Stale || !strings.Contains(p.LatestRun().Calls[0].Actions[0].Result, `"invalidatedTargets":`) {
		t.Fatal("base change no longer invalidates look in real receipt")
	}
	writeContentMaterialEvidence(t, "visual-projection.json", map[string]any{"actualRequest": request, "storedProject": before, "brokenDependencyRequest": brokenRequest})
}

func TestModelStoryboardMaterialKeepsOldReceiptsAndNativeHistory(t *testing.T) {
	for _, format := range []string{"", modelSourceReferenceFormat} {
		t.Run("format-"+format, func(t *testing.T) {
			s, p := compactStoryboardFixture(t)
			shotID := p.Storyboard.Shots[0].ID
			p, err := s.StartEpisode(context.Background(), p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "regeneratePrompts", ShotIDs: []string{shotID}})
			if err != nil {
				t.Fatal(err)
			}
			public, _ := s.StoryboardEpisode(p, "e1")
			oldReceipt := string(mustJSON(map[string]any{"ok": true, "currentStoryboard": public, "source": p.ScriptSource.Spans}))
			r := p.LatestRun()
			r.End = len(p.ScriptSource.Spans)
			r.Calls = []d.Call{{ID: "persisted-call", Stage: r.Stage, End: r.End, Status: "applied", SourceReferenceFormat: format, Request: json.RawMessage(`{"originalWire":true}`), Response: `{"originalResponse":true}`, Text: "原始说明", Reasoning: "原始思考", ProviderState: "signed opaque state", FinishReason: "tool_calls", Actions: []d.Action{{ID: "old-read", Name: "read_storyboard", Arguments: `{}`, Status: "applied", Result: oldReceipt}}}}
			before := d.Clone(p)
			req, _, err := s.storyboardRequest(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			wantReceipt, _ := modelSourceReceipt(p, r.Calls[0], oldReceipt)
			if len(req.Dialogue) != 2 || req.Dialogue[1].Content != wantReceipt || req.Dialogue[0].ProviderState != "signed opaque state" || req.Dialogue[0].Reasoning != "原始思考" || !strings.Contains(req.Dialogue[1].Content, `"plainText":`) || !strings.Contains(req.Dialogue[1].Content, "uploaded-image-sentinel") || !reflect.DeepEqual(before, p) {
				t.Fatal("new initial projection rewrote old receipts, native history or persisted evidence")
			}
			rejectMaterialFields(t, materialObject(t, json.RawMessage(req.CurrentState)), "plainText", "thumbnailUrl", "activeRun", "latestRun")
		})
	}
}

func TestModelStoryboardMaterialActualHTTPAgentRepairsOnlyFrozenPrompt(t *testing.T) {
	ctx := context.Background()
	s, p := compactStoryboardFixture(t)
	shotID := p.Storyboard.Shots[0].ID
	otherShot := *p.Storyboard.Shot(p.Storyboard.Shots[1].ID)
	p, err := s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "regeneratePrompts", ShotIDs: []string{shotID}})
	if err != nil {
		t.Fatal(err)
	}
	beforeShot, source, assets := *p.Storyboard.Shot(shotID), p.ScriptSource, p.Assets
	parts := p5Parts(false)
	parts[len(parts)-1].Text += "完整停顿😀，原文字样 p1 不改写。"
	var mu sync.Mutex
	var wires []json.RawMessage
	stableInput := ""
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		wires = append(wires, append(json.RawMessage(nil), raw...))
		step++
		var wire struct {
			Messages []struct{ Role, Content string }
		}
		if json.Unmarshal(raw, &wire) != nil || len(wire.Messages) < 2 || wire.Messages[0].Role != "system" || wire.Messages[1].Role != "user" {
			t.Error("actual initial message layout changed")
			http.Error(w, "invalid actual wire", 500)
			return
		}
		var input map[string]any
		if json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil {
			t.Error("invalid model material")
			http.Error(w, "invalid material", 500)
			return
		}
		if step == 1 || step == 6 {
			stableInput = wire.Messages[1].Content
		} else if wire.Messages[1].Content != stableInput {
			t.Error("save or read changed the actual input before the tool history")
		}
		var state map[string]any
		if json.Unmarshal([]byte(wire.Messages[len(wire.Messages)-1].Content), &state) != nil || wire.Messages[len(wire.Messages)-1].Role != "user" {
			t.Error("current result must be a final user message")
		}
		rejectMaterialFields(t, state["currentStoryboard"], "plainText", "thumbnailUrl", "activeRun", "latestRun", "access", "shotNumber", "sceneNumber")
		if !strings.Contains(wire.Messages[1].Content, source.Spans[0].Text) || strings.Contains(wire.Messages[1].Content, source.Spans[1].Text) {
			t.Error("complete target source or episode isolation changed")
		}
		current, _ := s.Project(ctx, p.ID)
		name := ""
		var args any = map[string]any{}
		switch step {
		case 1:
			name = "read_storyboard"
		case 2, 6:
			if step == 6 && (current.LatestRun().Stage != "storyboardReview" || len(wire.Messages) != 3) {
				t.Error("independent review inherited generation dialogue")
			}
			name, args = "read_assets", map[string]any{"targets": beforeShot.SelectedAssets}
		case 3:
			name, args = "save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "updateShot", "shotId": otherShot.ID, "patch": map[string]any{"videoPrompt": map[string]any{"parts": parts}}}}}
		case 4:
			if !strings.Contains(wire.Messages[len(wire.Messages)-2].Content, "STORYBOARD_SCOPE_VIOLATION") || current.Storyboard.Shot(shotID).Action != beforeShot.Action {
				t.Error("out-of-scope write was not atomically refused with real feedback")
			}
			name, args = "save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "updateShot", "shotId": shotID, "patch": map[string]any{"videoPrompt": map[string]any{"parts": parts}}}}}
		case 5, 7:
			if !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, "完整停顿😀") {
				t.Error("saved full prompt missing from next actual request")
			}
			name = "finish_storyboard"
		default:
			t.Error("unexpected extra model request")
			http.Error(w, "fixture exhausted", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": fmt.Sprintf("free-compact-%d", step), "choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "reasoning_content": "完整原始思考 p1", "tool_calls": []any{map[string]any{"id": fmt.Sprintf("compact-tool-%d", step), "type": "function", "function": map[string]any{"name": name, "arguments": string(mustJSON(args))}}}}}}, "usage": map[string]int{"prompt_tokens": 0, "completion_tokens": 0}})
	}))
	defer server.Close()
	provider, err := modeladapter.New(ctx, "free-loopback-only", server.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	registry(s).Model = provider
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	got, _ := s.Project(ctx, p.ID)
	writeContentMaterialEvidence(t, "storyboard-http-agent.json", map[string]any{"actualWireRequests": wires, "project": got, "freeFixture": true})
	content := append([]d.PromptPart{}, got.Storyboard.Shot(shotID).VideoPrompt.Parts...)
	for i := range content {
		content[i].LabelAtSave = "" // 程序按准确 ID 冻结的标签不是模型填写正文。
	}
	if step != 7 || got.LatestRun().Status != d.RunCompleted || got.UsedMicroCNY() != 0 || !reflect.DeepEqual(source, got.ScriptSource) || !reflect.DeepEqual(assets, got.Assets) || !reflect.DeepEqual(otherShot, *got.Storyboard.Shot(otherShot.ID)) || !reflect.DeepEqual(beforeShot.ShotValue, got.Storyboard.Shot(shotID).ShotValue) || !reflect.DeepEqual(parts, content) {
		t.Fatal("compact model loop altered source, frozen fields, another episode or incurred cost", got.LatestRun().Error)
	}
	for _, call := range got.LatestRun().Calls {
		if call.Reasoning != "完整原始思考 p1" || call.SourceReferenceFormat != modelSourceReferenceFormat {
			t.Fatal("raw response or source transport metadata changed")
		}
	}
	if !strings.Contains(got.LatestRun().Calls[1].Actions[0].Result, `"basis":`) || !strings.Contains(got.LatestRun().Calls[3].Actions[0].Result, `"before":`) || !strings.Contains(got.LatestRun().Calls[3].Actions[0].Result, `"after":`) {
		t.Fatal("real read basis or complete save receipt was compacted")
	}
}

func writeContentMaterialEvidence(t *testing.T, name string, value any) {
	t.Helper()
	if root := os.Getenv("DRAMA_TEST_MODEL_CONTENT_EVIDENCE_DIR"); root != "" {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), mustJSON(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
