package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/imagethumbnail"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	testconfig "drama-agent/internal/testsupport/config"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/uploadfiles"
)

// Complete provider wire, real Eino, HTTP and reopened PG. This frozen fictional provider proves transport/adoption, not autonomous semantic quality.
func TestP5HTTPPostgresActualEinoWireFullFlowAndEditableAssets(t *testing.T) {
	store := openProjectContentPG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var calls, indexCalls, storyCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var wire struct {
			Messages []struct {
				Role, Content string
				ToolID        string `json:"tool_call_id"`
			}
			Tools []struct {
				Function struct {
					Name       string
					Parameters json.RawMessage
				}
			}
		}
		if e := json.NewDecoder(r.Body).Decode(&wire); e != nil {
			t.Error(e)
			http.Error(w, "bad", 400)
			return
		}
		if calls.Load() > 25 {
			t.Error("free fixture exceeded expected calls")
			http.Error(w, "fixture exhausted", 400)
			return
		}
		tools := map[string]bool{}
		for _, tool := range wire.Tools {
			tools[tool.Function.Name] = true
		}
		if tools["save_storyboard"] || tools["save_episode_ranges"] {
			contract := d.StoryboardContract
			if tools["save_episode_ranges"] {
				contract = d.EpisodeIndexContract
			}
			expected := app.TaskTools(contract)
			if len(expected) != len(wire.Tools) {
				t.Error("P5 registered/wire tool count differs")
			}
			for i, definition := range expected {
				if i >= len(wire.Tools) {
					break
				}
				var want, got any
				json.Unmarshal(definition.Parameters, &want)
				json.Unmarshal(wire.Tools[i].Function.Parameters, &got)
				if definition.Name == "save_storyboard" {
					if strings.Count(string(wire.Tools[i].Function.Parameters), `"maximum":12`) != 2 {
						t.Error("createShot/updateShot wire schemas lack the actual model maximum")
					}
					continue
				}
				if definition.Name != wire.Tools[i].Function.Name || !reflect.DeepEqual(want, got) {
					t.Errorf("P5 actual provider schema differs: %s", definition.Name)
				}
			}
		}
		if modelregistry.FreeVisualWireReply(w, wire) {
			return
		}
		if len(wire.Messages) < 2 {
			t.Error("missing actual input")
			http.Error(w, "bad", 400)
			return
		}
		// 生产请求将稳定材料和对话末尾当前状态分成两条 user 消息；夹具消费两者，
		// 不能只读稳定前缀而反复尝试用空 episode/旧场景创建内容。
		material := map[string]json.RawMessage{}
		for _, message := range wire.Messages[1:] {
			if message.Role != "user" {
				continue
			}
			var part map[string]json.RawMessage
			if err := json.Unmarshal([]byte(message.Content), &part); err != nil {
				t.Error("bad user material", err)
				http.Error(w, "invalid fixture input", 400)
				return
			}
			for key, value := range part {
				material[key] = value
			}
		}
		merged, _ := json.Marshal(material)
		raw := string(merged)

		if tools["save_episode_ranges"] {
			if strings.Contains(wire.Messages[0].Content, "designNotes、已填时长和正文都是待核对结果") || strings.Contains(wire.Messages[0].Content, "区分人物实际位置、画面左右") {
				t.Error("storyboard instructions leaked into episode index")
			}
			n := indexCalls.Add(1)
			if n == 1 {
				var input struct{ Source []d.SourceSpan }
				if json.Unmarshal([]byte(raw), &input) != nil || len(input.Source) != 4 {
					t.Error("full source missing")
				}
				sp := input.Source
				end := sp[len(sp)-1]
				full := []d.SourceRange{{Start: d.SourcePoint{SpanID: sp[0].ID}, End: d.SourcePoint{SpanID: end.ID, Offset: len([]rune(end.Text))}}}
				emitProjectContent(w, []d.Action{contentAction("save_episode_ranges", map[string]any{"operations": []any{map[string]any{"op": "setSegmentation", "segmentation": "whole"}, map[string]any{"op": "setSupportRanges", "ranges": []d.SourceRange{}}, map[string]any{"op": "createEpisode", "value": d.EpisodeValue{Title: "整篇", SourceRanges: full}}}})})
				return
			}
			if n == 3 && (len(wire.Messages) != 3 || !strings.Contains(wire.Messages[0].Content, "独立复核")) {
				t.Error("index review inherited generation dialogue")
			}
			emitProjectContent(w, []d.Action{contentAction("finish_episode_index", map[string]any{})})
			return
		}
		if tools["save_catalog"] {
			var input struct {
				Source []d.SourceSpan          `json:"source"`
				Assets []app.CatalogAssetInput `json:"assets"`
			}
			if json.Unmarshal([]byte(raw), &input) != nil {
				t.Error("bad catalog material")
			}
			if len(input.Assets) == 0 {
				ids := []string{}
				for _, s := range input.Source {
					ids = append(ids, s.ID)
				}
				input.Assets = catalogInputs([]d.Asset{{Kind: "character", Name: "林晚", Description: "灰外套", SourceIDs: ids}, {Kind: "character", Name: "陈默", Description: "另一人", SourceIDs: ids}, {Kind: "location", Name: "客厅", Description: "桌边", SourceIDs: ids}, {Kind: "prop", Name: "铜钥匙", Description: "铜质钥匙", SourceIDs: ids}})
			}
			emitProjectContent(w, []d.Action{contentAction("save_catalog", app.CatalogChangeSet{Assets: input.Assets}), contentAction("finish_batch", map[string]any{})})
			return
		}
		if !tools["save_storyboard"] {
			t.Error("unexpected tools")
			http.Error(w, "bad", 400)
			return
		}
		storyCalls.Add(1)
		for _, rule := range []string{"区分人物实际位置、画面左右", "下一镜从已形成的结果继续", "每镜正文能独立理解"} {
			if !strings.Contains(wire.Messages[0].Content, rule) {
				t.Error("approved storyboard rule missing from actual provider wire", rule)
			}
		}
		if strings.Contains(wire.Messages[0].Content, "独立复核上下文") && !strings.Contains(wire.Messages[0].Content, "designNotes、已填时长和正文都是待核对结果") {
			t.Error("actual storyboard review lacks content-specific checks")
		}
		var input struct {
			TaskScope  d.StoryboardScope `json:"taskScope"`
			AssetIndex []d.Asset         `json:"assetIndex"`
			Current    app.EpisodeView   `json:"currentStoryboard"`
		}
		if json.Unmarshal([]byte(raw), &input) != nil {
			t.Error("bad storyboard material")
		}
		if input.TaskScope.BreakdownMode != "story" || input.TaskScope.VideoConstraint == nil || input.TaskScope.VideoConstraint.MaxDurationSeconds != 12 || !strings.Contains(wire.Messages[0].Content, "剧情友好") || !strings.Contains(wire.Messages[0].Content, "最长 12 秒") {
			t.Error("HTTP mode or frozen video maximum missing from actual provider request/review")
		}
		if len(input.AssetIndex) != 4 || !strings.Contains(raw, "complete_work") || !strings.Contains(raw, "你自己决定") {
			t.Error("target or candidate catalog incomplete")
		}
		read := false
		for _, m := range wire.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, `"assets":[`) && strings.Contains(m.Content, `"inputDigest"`) {
				read = true
			}
		}
		refs := []d.VisualTarget{}
		ids := map[string]string{}
		for _, a := range input.AssetIndex {
			refs = append(refs, d.VisualTarget{AssetID: a.ID})
			ids[a.Name] = a.ID
		}
		if len(input.Current.Scenes) == 0 {
			emitProjectContent(w, []d.Action{contentAction("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": input.Current.Episode.ID, "value": d.SceneValue{Title: "客厅交谈", SourceRanges: input.Current.Episode.SourceRanges, LocationAssetID: strptr(ids["客厅"]), Time: "日", Summary: "放钥匙并交谈"}}}})})
			return
		}
		if !read {
			emitProjectContent(w, []d.Action{contentAction("read_assets", map[string]any{"targets": refs})})
			return
		}
		if len(input.Current.Shots) == 0 {
			parts := []d.PromptPart{{Type: "asset", AssetID: ids["客厅"]}, {Type: "text", Text: "内，中景平视。"}, {Type: "asset", AssetID: ids["林晚"]}, {Type: "text", Text: "站在桌边，对"}, {Type: "asset", AssetID: ids["陈默"]}, {Type: "text", Text: "说：你自己决定。手随镜头下移，将"}, {Type: "asset", AssetID: ids["铜钥匙"]}, {Type: "text", Text: "放在桌面，松手停顿。结束画面停在钥匙与尚未伸出的手之间。"}}
			value := map[string]any{"sourceRanges": input.Current.Episode.SourceRanges, "action": "林晚放下铜钥匙并松手停顿", "dialogues": []d.Dialogue{{SpeakerAssetID: strptr(ids["林晚"]), Text: "你自己决定。"}}, "camera": d.Camera{Size: "中景", Angle: "平视", Movement: "随手下移"}, "durationSeconds": 8.5, "designNotes": "机位是补充拍摄设计，结束停在钥匙与手之间", "selectedAssets": refs, "videoPrompt": map[string]any{"parts": parts}}
			emitProjectContent(w, []d.Action{contentAction("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createShot", "sceneId": input.Current.Scenes[0].ID, "value": value}}})})
			return
		}
		emitProjectContent(w, []d.Action{contentAction("finish_storyboard", map[string]any{})})
	}))
	defer provider.Close()
	model, e := modeladapter.New(ctx, "fictional-loopback-key", provider.URL, md.DeepSeekModelID)
	if e != nil {
		t.Fatal(e)
	}
	s := contentService(t, store, model)
	s.Models.(*modelregistry.Registry).OptionsValue.Video = []ports.ModelOption{{ID: "free-video", Name: "免费视频能力夹具", MediaProfile: &md.MediaProfile{Durations: []float64{4, 12, 8}, DefaultDuration: 4}}}
	files := uploadfiles.New()
	s.Files = files
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	host.Files = files
	defer host.Shutdown(ctx)
	request := func(method, path string, v any, want int) []byte {
		t.Helper()
		var body []byte
		if v != nil {
			body, _ = json.Marshal(v)
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	file, e := files.ForActor(legacyHTTPActor).Upload(ctx, uploadapp.Input{Filename: "fictional-p5.txt", Data: []byte("林晚穿灰外套。\n客厅里有桌子，陈默坐在桌边。\n林晚把铜钥匙放到桌上，对陈默说：你自己决定。\n她松手停顿。")})
	if e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 0 {
		t.Fatal("file upload called model")
	}
	settings := d.ProjectSettings{LLMModel: md.DeepSeekModelID, AspectRatio: "16:9", VideoModel: "free-video"}
	raw := request("POST", "/api/projects", map[string]any{"fileId": file.ID, "title": "P5免费全链", "settings": settings, "billingBasis": legacyFileQuote(t, s, file.ID, settings)}, 201)
	var created struct{ ID string }
	if json.Unmarshal(raw, &created) != nil {
		t.Fatal("bad creation")
	}
	p, e := store.Snapshot(ctx, created.ID)
	if e != nil {
		t.Fatal(e)
	}
	cleanProjectContentRow(t, p)
	path := "/api/projects/" + p.ID
	if e = host.WaitUntilIdle(ctx); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Project(ctx, p.ID)
	index := s.StoryboardIndex(p)
	if p.LatestRun().Status != d.RunCompleted || !p.IndexReady() || index.EpisodeCount != nil {
		t.Fatalf("index failed: %s %s", p.LatestRun().Status, p.LatestRun().Error)
	}
	beforeCalls := calls.Load()
	request("GET", path+"/storyboard", nil, 200)
	if calls.Load() != beforeCalls {
		t.Fatal("GET called model")
	}
	if len(p.Runs) != 2 || !p.Runs[0].ContinueAssetPreparation || !p.CatalogReady() || len(p.PendingVisualTargets()) != 0 {
		t.Fatalf("P3/P4 changed: %s %s", p.LatestRun().Status, p.LatestRun().Error)
	}
	var summaries []struct {
		ID        string
		LatestRun *struct{ ID, Contract, Status, Stage string }
	}
	if json.Unmarshal(request("GET", "/api/projects", nil, 200), &summaries) != nil {
		t.Fatal("bad project summaries")
	}
	found := false
	for _, row := range summaries {
		if row.ID == p.ID {
			found = row.LatestRun != nil && row.LatestRun.ID == p.LatestRun().ID && row.LatestRun.Status == "completed" && row.LatestRun.Stage == "visualPrompt"
		}
	}
	if !found {
		t.Fatal("list did not expose real task state")
	}
	catalogRun := p.LatestCatalogRun().ID
	epID := p.Storyboard.Index.Episodes[0].ID
	endpoint := path + "/storyboard/episodes/" + epID + "/runs"
	beforeStart := d.Clone(p)
	for _, value := range []any{"bad", "", nil} {
		request("POST", endpoint, map[string]any{"revision": p.Revision, "operation": "breakdown", "breakdownMode": value}, 400)
		request("POST", path+"/quote", map[string]any{"revision": p.Revision, "operation": "breakdown", "episodeId": epID, "breakdownMode": value}, 400)
	}
	request("POST", endpoint, map[string]any{"revision": p.Revision, "operation": "fillPrompts", "breakdownMode": "story"}, 400)
	request("POST", endpoint, map[string]any{"revision": p.Revision, "operation": "breakdown", "maxDurationSeconds": 100}, 400)
	var quote app.ChargeQuote
	json.Unmarshal(request("POST", path+"/quote", app.QuoteInput{Revision: p.Revision, Operation: "breakdown", EpisodeID: epID}, 200), &quote)
	request("POST", endpoint, map[string]any{"revision": p.Revision, "operation": "breakdown", "breakdownMode": "story", "billingBasis": quote.Basis}, 409)
	unchanged, _ := s.Project(ctx, p.ID)
	if !reflect.DeepEqual(beforeStart, d.Clone(unchanged)) || calls.Load() != beforeCalls {
		t.Fatal("rejected mode or stale quote changed state or called supplier")
	}
	json.Unmarshal(request("POST", path+"/quote", app.QuoteInput{Revision: p.Revision, Operation: "breakdown", EpisodeID: epID, BreakdownMode: "story"}, 200), &quote)
	request("POST", endpoint, map[string]any{"revision": p.Revision, "operation": "breakdown", "breakdownMode": "story", "billingBasis": quote.Basis}, 202)
	if e = host.WaitUntilIdle(ctx); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Project(ctx, p.ID)
	if p.LatestRun().Status != d.RunCompleted || len(p.Storyboard.Shots) != 1 {
		t.Fatalf("storyboard not complete: %s %s", p.LatestRun().Status, p.LatestRun().Error)
	}
	if p.LatestRun().StoryboardScope.BreakdownMode != "story" || p.LatestRun().StoryboardScope.VideoConstraint.MaxDurationSeconds != 12 || *p.Storyboard.Shots[0].DurationSeconds != 9 {
		t.Fatal("real persistence lost mode, frozen maximum, or required integer rounding")
	}
	originalEstimate := false
	for _, call := range p.LatestRun().Calls {
		for _, action := range call.Actions {
			originalEstimate = originalEstimate || strings.Contains(action.Arguments, `"durationSeconds":8.5`)
		}
	}
	if !originalEstimate {
		t.Fatal("integer adoption rewrote original model arguments")
	}
	if storyCalls.Load() != 6 || indexCalls.Load() != 3 {
		t.Fatal("unexpected scripted loop", storyCalls.Load(), indexCalls.Load())
	}
	v, e := s.StoryboardEpisode(p, epID)
	if e != nil || !v.Shots[0].VideoPrompt.Valid || !strings.Contains(v.Shots[0].VideoPrompt.PlainText, "@林晚") {
		t.Fatal("PG result missing content", e)
	}
	apiRaw := request("GET", path+"/storyboard/episodes/"+epID, nil, 200)
	var apiView app.EpisodeView
	var expectedJSON, actualJSON any
	json.Unmarshal(mustP5JSON(v), &expectedJSON)
	json.Unmarshal(apiRaw, &actualJSON)
	if json.Unmarshal(apiRaw, &apiView) != nil || !reflect.DeepEqual(expectedJSON, actualJSON) {
		t.Fatal("PG/API episode mismatch")
	}
	var pv struct {
		Run *httpapi.RunView
	}
	json.Unmarshal(request("GET", path, nil, 200), &pv)
	if pv.Run == nil || pv.Run.ID != catalogRun {
		t.Fatal("P5 run masked P3/P4 status")
	}
	frozen := d.Clone(p)
	count := calls.Load()
	asset := p.Assets[0]
	request("POST", path+"/assets/"+asset.ID+"/image", map[string]any{"revision": p.Revision, "fileId": "nonexistent"}, 404)
	p, _ = s.Project(ctx, p.ID)
	if !reflect.DeepEqual(frozen, d.Clone(p)) || calls.Load() != count {
		t.Fatal("locked writes altered graph/run/call/fees")
	}
	// Image adoption remains independent of editable asset text.
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	files.Thumbnails = imagethumbnail.FFmpeg{Settings: testconfig.Example().Media}
	imageFile, err := files.ForActor(legacyHTTPActor).Upload(ctx, uploadapp.Input{Filename: "asset.png", ContentType: "image/png", Data: imageBytes.Bytes(), Thumbnail: &ports.ThumbnailOptions{MaxWidth: 8, MaxHeight: 8}})
	if err != nil {
		t.Fatal(err)
	}
	request("POST", path+"/assets/"+asset.ID+"/image", map[string]any{"revision": p.Revision, "fileId": imageFile.ID}, 200)
	p, _ = s.Project(ctx, p.ID)
	if p.Asset(asset.ID).ImageFileID != imageFile.ID || calls.Load() != count {
		t.Fatal("image adoption failed or invoked a model")
	}
	request("POST", path+"/storyboard/changes", map[string]any{"revision": p.Revision - 1, "operations": []any{map[string]any{"op": "deleteShot", "shotId": p.Storyboard.Shots[0].ID}}}, 409)
	request("POST", path+"/storyboard/changes", map[string]any{"revision": p.Revision, "assetsLocked": false, "operations": []any{}}, 400)
	request("POST", path+"/storyboard/episodes/foreign/runs", map[string]any{"revision": p.Revision, "operation": "breakdown"}, 404)
	// A gap insertion is one CAS, rather than an append followed by a second
	// reorder request that could fail after creating the shot in the wrong place.
	originalShot := p.Storyboard.Shots[0]
	var shotValue map[string]any
	json.Unmarshal(mustP5JSON(originalShot.ShotValue), &shotValue)
	// Saved display labels are read-only metadata, not fields in the write DTO.
	parts := make([]d.PromptPart, len(originalShot.VideoPrompt.Parts))
	for i, part := range originalShot.VideoPrompt.Parts {
		parts[i] = d.PromptPart{Type: part.Type, Text: part.Text, AssetID: part.AssetID, LookID: part.LookID}
	}
	shotValue["videoPrompt"] = map[string]any{"parts": parts}
	sceneID, initialRevision := originalShot.SceneID, p.Revision
	request("POST", path+"/storyboard/changes", map[string]any{"revision": p.Revision, "operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "value": shotValue}}}, 200)
	p, _ = s.Project(ctx, p.ID)
	lastID := p.Storyboard.Scene(sceneID).ShotIDs[1]
	request("POST", path+"/storyboard/changes", map[string]any{"revision": p.Revision, "operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "afterShotId": originalShot.ID, "value": shotValue}}}, 200)
	p, _ = s.Project(ctx, p.ID)
	order := p.Storyboard.Scene(sceneID).ShotIDs
	if p.Revision != initialRevision+2 || len(order) != 3 || order[0] != originalShot.ID || order[2] != lastID || !reflect.DeepEqual(*p.Storyboard.Shot(originalShot.ID), originalShot) || calls.Load() != count {
		t.Fatal("HTTP insertion changed content, task calls or order", order)
	}
	request("POST", path+"/storyboard/changes", map[string]any{"revision": p.Revision, "operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "beforeShotId": originalShot.ID, "value": shotValue}}}, 200)
	p, _ = s.Project(ctx, p.ID)
	order = p.Storyboard.Scene(sceneID).ShotIDs
	if p.Revision != initialRevision+3 || len(order) != 4 || order[1] != originalShot.ID || order[3] != lastID || !reflect.DeepEqual(*p.Storyboard.Shot(originalShot.ID), originalShot) || calls.Load() != count {
		t.Fatal("HTTP insertion before first shot changed content, calls or order", order)
	}
	json.Unmarshal(request("GET", path+"/storyboard/episodes/"+epID, nil, 200), &apiView)
	if !reflect.DeepEqual(apiView.Scenes[0].ShotIDs, order) {
		t.Fatal("HTTP insertion order differs from PG")
	}
	// Compare independently read PG snapshots without re-encoding RawMessage fields.
	frozen = p
	request("POST", path+"/storyboard/changes", map[string]any{"revision": p.Revision, "operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "afterShotId": "foreign", "value": shotValue}}}, 400)
	request("POST", path+"/storyboard/changes", map[string]any{"revision": p.Revision, "operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "beforeShotId": "foreign", "value": shotValue}}}, 400)
	request("POST", path+"/storyboard/changes", map[string]any{"revision": p.Revision, "operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "beforeShotId": originalShot.ID, "afterShotId": originalShot.ID, "value": shotValue}}}, 400)
	request("POST", path+"/storyboard/changes", map[string]any{"revision": initialRevision, "operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "afterShotId": originalShot.ID, "value": shotValue}}}, 409)
	p, _ = s.Project(ctx, p.ID)
	if !reflect.DeepEqual(p, frozen) || calls.Load() != count {
		t.Fatalf("rejected insertion mutated PG or called a model: same JSON=%t, calls before=%d after=%d", bytes.Equal(mustP5JSON(p), mustP5JSON(frozen)), count, calls.Load())
	}
	assetInput := d.AssetInput{Kind: asset.Kind, Name: asset.Name, Aliases: append([]string{}, asset.Aliases...), Description: asset.Description, Character: asset.Character}
	assetInput.Description += "用户在拆解后修订资料"
	if assetInput.Kind == "character" && assetInput.Character == nil {
		assetInput.Character = &d.Character{}
	}
	request("POST", path+"/assets/"+asset.ID, map[string]any{"revision": p.Revision, "asset": assetInput}, 200)
	request("POST", path+"/assets/"+asset.ID, map[string]any{"revision": p.Revision, "asset": assetInput}, 409)
	p, _ = s.Project(ctx, p.ID)
	if p.Asset(asset.ID).Description != assetInput.Description || !reflect.DeepEqual(d.Clone(p).Storyboard, d.Clone(frozen).Storyboard) || !reflect.DeepEqual(d.Clone(p).Runs, d.Clone(frozen).Runs) || calls.Load() != count {
		t.Fatal("post-breakdown edit lost content, accepted stale revision, or invoked a model")
	}
	frozen = d.Clone(p)

	v, _ = s.StoryboardEpisode(p, epID)
	freshStore := openProjectContentPG(t)
	fresh := contentService(t, freshStore, model)
	again, e := fresh.Project(ctx, p.ID)
	if e != nil || !reflect.DeepEqual(p, again) {
		t.Fatal("PG reopen changed IDs, asset edits, parts or ledger", e)
	}
	if e = fresh.Execute(ctx, p.ID); e != nil || calls.Load() != count {
		t.Fatal("restart resent completed task", e)
	}
	host.ReadOnly = true
	var ro app.EpisodeView
	json.Unmarshal(request("GET", path+"/storyboard/episodes/"+epID, nil, 200), &ro)
	if ro.Access.Editable || ro.Access.CanStart || ro.Access.Reason != "SERVICE_READ_ONLY" {
		t.Fatal("read-only access projection wrong")
	}
	request("POST", path+"/storyboard/changes", map[string]any{"revision": p.Revision, "operations": []any{}}, 403)
	if dir := os.Getenv("DRAMA_TEST_P5_EVIDENCE_DIR"); dir != "" {
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		artifact := map[string]any{"purpose": "free scripted provider protocol evidence, not real-model quality", "originalText": "林晚穿灰外套。\n客厅里有桌子，陈默坐在桌边。\n林晚把铜钥匙放到桌上，对陈默说：你自己决定。\n她松手停顿。", "project": p, "episodeView": v, "providerCalls": calls.Load(), "indexCalls": indexCalls.Load(), "storyboardCalls": storyCalls.Load()}
		raw, e := json.MarshalIndent(artifact, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, "full-flow-"+p.LatestRun().ID+".json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	t.Logf("full flow: %d persisted provider calls, index=%d storyboard=%d; source/IDs/parts/asset edits/ledger match reopened PG", calls.Load(), indexCalls.Load(), storyCalls.Load())
}
func strptr(s string) *string { return &s }

func mustP5JSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
