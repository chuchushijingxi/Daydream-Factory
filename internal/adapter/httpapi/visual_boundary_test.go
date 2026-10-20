package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/localembedding"
	"drama-agent/internal/adapter/milvusevidence"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

type visualSmallWindow struct{ ports.Model }

// 夹具窗口须容纳当前完整指令、补读和小范围修复；大正文的完整回执仍须超窗。
// 这是脚本模型的测试容量，不调整生产模型配置或删减材料。
func (visualSmallWindow) Capacity() (int, int) { return 27000, 12000 }

func TestVisualMixedReadSavePostgresEinoRejectsThenRepairsThroughActualReceipt(t *testing.T) {
	store := openProjectContentPG(t)
	ctx := context.Background()
	var calls atomic.Int32
	var p d.Project
	var model ports.Model
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var wire struct {
			Messages []struct{ Role, Content string }
		}
		if json.Unmarshal(raw, &wire) != nil {
			t.Error("invalid actual model request")
		}
		switch calls.Add(1) {
		case 1:
			emitProjectContent(w, []d.Action{contentAction("read_script_evidence", app.ReadScriptEvidence{SpanIDs: []string{"p2"}}), contentAction("save_visual_prompts", app.SaveVisualPrompts{Prompts: []app.VisualPromptInput{{Target: app.VisualTargetInput{AssetID: "c"}, Text: strings.Repeat("visual ", 800), DesignNotes: "fictional design"}}})})
		case 2:
			latest, err := store.Snapshot(ctx, p.ID)
			if err != nil {
				t.Error(err)
			}
			actions := latest.LatestRun().Calls[0].Actions
			if len(actions) != 2 || actions[0].Status != "applied" || actions[1].Status != "rejected" || !strings.Contains(actions[1].Result, "CAPACITY_EXCEEDED") || len(latest.VisualPrompts) != 0 {
				t.Error("oversized save not atomically rejected")
			}
			for _, action := range actions {
				action = expectedEvidenceReceipt(t, latest, latest.LatestRun().Calls[0], action)
				if !model.ContainsToolReceipt(raw, action, d.Call{}) {
					t.Error("actual success/rejection receipt absent from next HTTP input")
				}
			}
			emitProjectContent(w, []d.Action{contentAction("save_visual_prompts", app.SaveVisualPrompts{Prompts: []app.VisualPromptInput{{Target: app.VisualTargetInput{AssetID: "c"}, Text: "修正后的完整小范围正文", DesignNotes: "保留已有事实"}}})})
		case 3:
			latest, _ := store.Snapshot(ctx, p.ID)
			for _, action := range latest.LatestRun().Calls[1].Actions {
				if !model.ContainsToolReceipt(raw, action, d.Call{}) {
					t.Error("corrected save receipt absent from finish input")
				}
			}
			emitProjectContent(w, []d.Action{contentAction("finish_visual_prompts", struct{}{})})
		default:
			t.Error("unexpected extra model request")
			http.Error(w, "extra", 500)
		}
	}))
	defer provider.Close()
	baseModel, err := modeladapter.New(ctx, "fictional-loopback-key", provider.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	model = visualSmallWindow{baseModel}
	s := contentService(t, store, model)
	p = createProjectContent(t, s, pgAssets())
	p.Assets[0].SourceIDs = []string{"v1-p000001"}
	p.ScriptSource.Spans[1].Text = strings.Repeat("fictional evidence ", 400)
	p.Revision++
	if err = store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	before := d.Clone(p)
	p, err = quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "single-visual", AssetID: "c"}).StartSingleVisual(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Execute(ctx, p.ID); err != nil {
		if failed, readErr := store.Snapshot(ctx, p.ID); readErr == nil {
			writeVisualBoundaryEvidence(t, "mixed-read-save-failed.json", failed)
		}
		t.Fatal(err)
	}
	after, _ := store.Snapshot(ctx, p.ID)
	if calls.Load() != 3 || after.LatestRun().Status != "completed" || after.Prompt(d.VisualTarget{AssetID: "c"}).Text != "修正后的完整小范围正文" || !reflect.DeepEqual(before.Assets, after.Assets) || !reflect.DeepEqual(before.ScriptSource, after.ScriptSource) {
		t.Fatal("real fixture loop did not repair/preserve", calls.Load(), after.LatestRun())
	}
	fixtureHost, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, fixtureHost, handler, "workspace")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "http://127.0.0.1/api/projects/"+p.ID, nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "修正后的完整小范围正文") {
		t.Fatal("API disagrees with committed result", recorder.Code)
	}
	writeVisualBoundaryEvidence(t, "mixed-read-save.json", after)
}

func TestVisualPostgresBrokenEncoderWithCompleteIndexNeverCallsTextModel(t *testing.T) {
	store := openProjectContentPG(t)
	ctx := context.Background()
	var textCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		textCalls.Add(1)
		http.Error(w, "text request must not happen", 500)
	}))
	defer provider.Close()
	model, err := modeladapter.New(ctx, "fictional-loopback-key", provider.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	s := contentService(t, store, model)
	p := createProjectContent(t, s, pgAssets())
	before := d.Clone(p)
	folder := t.TempDir()
	python := filepath.Join(folder, "python")
	if err = os.WriteFile(python, []byte("fictional non-executable worker"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"model.safetensors", "config.json", "tokenizer.json", "tokenizer_config.json", "special_tokens_map.json", "vocab.txt"} {
		if err = os.WriteFile(filepath.Join(folder, name), []byte("fictional weights"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	encoder, err := localembedding.New(python, folder)
	if err != nil {
		t.Fatal(err)
	}
	vectorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/has"):
			data = map[string]any{"has": true}
		case strings.HasSuffix(r.URL.Path, "/describe"):
			data = map[string]any{"description": "drama-agent visual evidence v1: derived paragraph index", "fields": []any{map[string]any{"name": "vector", "type": "FloatVector", "params": []any{map[string]string{"key": "dim", "value": "512"}}}}}
		case strings.HasSuffix(r.URL.Path, "/query"):
			data = []any{map[string]any{"count(*)": len(p.ScriptSource.Spans)}}
		default:
			t.Error("complete fixture index must not be written", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer vectorServer.Close()
	s.Evidence = milvusevidence.NewLazy(func() (ports.EvidenceIndex, error) {
		return milvusevidence.New(vectorServer.URL, "fictional_cached_index", encoder)
	})
	p, err = quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "single-visual", AssetID: "c"}).StartSingleVisual(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := store.Snapshot(ctx, p.ID)
	if textCalls.Load() != 0 || after.LatestRun().Status != "failed" || after.LatestRun().ErrorCode != "EVIDENCE_UNAVAILABLE" || len(after.LatestRun().Calls) != 0 || after.UsedMicroCNY() != before.UsedMicroCNY() || !reflect.DeepEqual(before.Assets, after.Assets) || !reflect.DeepEqual(before.ScriptSource, after.ScriptSource) {
		t.Fatal("unusable runtime reached text provider or changed project", textCalls.Load())
	}
	fixtureHost, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, fixtureHost, handler, "workspace")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "http://127.0.0.1/api/projects/"+p.ID, nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "EVIDENCE_UNAVAILABLE") {
		t.Fatal("API lost persisted failure", recorder.Code)
	}
	writeVisualBoundaryEvidence(t, "encoder-unavailable.json", after)
}

func writeVisualBoundaryEvidence(t *testing.T, name string, p d.Project) {
	t.Helper()
	if folder := os.Getenv("DRAMA_TEST_EVIDENCE_DIR"); folder != "" {
		if err := os.MkdirAll(folder, 0755); err != nil {
			t.Fatal(err)
		}
		raw, err := json.MarshalIndent(map[string]any{"textProvider": "fictional loopback fixture; no paid model", "project": p}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(folder, name), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
