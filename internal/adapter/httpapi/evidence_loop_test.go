package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
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
	"drama-agent/internal/adapter/localembedding"
	"drama-agent/internal/adapter/milvusevidence"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

// Real Eino + provider HTTP adapter + PG + learned local BGE + Milvus. Only
// the text provider's responses are scripted; no paid provider is contacted.
func TestActualEvidenceToolsAgentLoopAndRecovery(t *testing.T) {
	endpoint := os.Getenv("DRAMA_TEST_MILVUS_URL")
	if endpoint == "" || os.Getenv("DRAMA_TEST_LOCAL_EMBEDDING") != "1" {
		t.Skip("isolated PG/vector/local model opt-in")
	}
	store := openProjectContentPG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	embedder, err := localembedding.New("../../../.data/evidence-embedding/runtime/bin/python", "../../../.data/evidence-embedding/model")
	if err != nil {
		t.Fatal(err)
	}
	collection := identity.New("drama_evidence_loop_test")
	index, err := milvusevidence.New(endpoint, collection, embedder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b, _ := json.Marshal(map[string]string{"collectionName": collection})
		res, e := http.Post(endpoint+"/v2/vectordb/collections/drop", "application/json", strings.NewReader(string(b)))
		if e != nil {
			t.Error(e)
			return
		}
		defer res.Body.Close()
		var wire struct{ Code int }
		if json.NewDecoder(res.Body).Decode(&wire) != nil || wire.Code != 0 {
			t.Error("test collection cleanup failed")
		}
	})
	for _, recovered := range []bool{false, true} {
		t.Run(map[bool]string{false: "natural-loop", true: "saved-observation-recovery"}[recovered], func(t *testing.T) {
			var count atomic.Int32
			var initializations atomic.Int32
			factory := func() (ports.EvidenceIndex, error) {
				initializations.Add(1)
				return index, nil
			}
			var p d.Project
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := int(count.Add(1))
				if recovered {
					n++
				}
				var wire struct {
					Model    string
					Messages []struct {
						Role, Content string
						ID            string `json:"tool_call_id"`
					}
					Tools []struct {
						Function struct {
							Name, Description string
							Parameters        any
						}
					}
				}
				if json.NewDecoder(r.Body).Decode(&wire) != nil {
					t.Error("invalid request")
					http.Error(w, "bad", 400)
					return
				}
				if wire.Model != "deepseek-flash" || len(wire.Messages) < 2 || len(wire.Tools) != 4 {
					t.Error("wrong chosen model/tools")
				}
				for i, def := range app.TaskTools(d.VisualContract) {
					if i >= len(wire.Tools) {
						break
					}
					var expected any
					json.Unmarshal(def.Parameters, &expected)
					got := wire.Tools[i].Function
					if got.Name != def.Name || got.Description != def.Description || !reflect.DeepEqual(got.Parameters, expected) {
						t.Error("schema/wire mismatch", def.Name)
					}
				}
				if strings.Contains(wire.Messages[1].Content, "未关联原文") || strings.Contains(wire.Messages[1].Content, "无关庭院") || strings.Contains(wire.Messages[1].Content, "无关道具") {
					t.Error("single initial material leaked full script or unrelated asset")
				}
				if !strings.Contains(wire.Messages[1].Content, "linked_evidence_only_not_complete") {
					t.Error("material coverage missing")
				}
				last := wire.Messages[len(wire.Messages)-1]
				switch n {
				case 1:
					emitProjectContent(w, []d.Action{contentAction("search_script_evidence", app.SearchScriptEvidence{Query: "人物左眉旧伤和外貌", Limit: 1})})
				case 2:
					searchRef := "p3"
					if recovered { // 已保存旧 Call 的回执沿用原表示。
						searchRef = "v1-p000003"
					}
					if last.Role != "tool" || !strings.Contains(last.Content, `"matches"`) || !strings.Contains(last.Content, `"spanId":"`+searchRef+`"`) {
						t.Error("real vector search receipt missing", last.Content)
					}
					emitProjectContent(w, []d.Action{contentAction("read_script_evidence", app.ReadScriptEvidence{SpanIDs: []string{"p3"}, Before: 1})})
				case 3:
					if last.Role != "tool" || !strings.Contains(last.Content, "左眉尾浅疤") || !strings.Contains(last.Content, "未关联原文") {
						t.Error("exact original observation missing", last.Content)
					}
					emitProjectContent(w, []d.Action{contentAction("save_visual_prompts", app.SaveVisualPrompts{Prompts: []app.VisualPromptInput{{Target: app.VisualTargetInput{AssetID: "c"}, Text: "32岁，右眉浅疤，黑发低马尾，深蓝工装", DesignNotes: "深蓝工装为设计"}}}), contentAction("finish_visual_prompts", struct{}{})})
				case 4:
					if !strings.Contains(last.Content, `"ok":false`) {
						t.Error("same-response finish bypassed receipt check")
					}
					emitProjectContent(w, []d.Action{contentAction("save_visual_prompts", app.SaveVisualPrompts{Prompts: []app.VisualPromptInput{{Target: app.VisualTargetInput{AssetID: "c"}, Text: "32岁，左眉浅疤，黑发低马尾，深蓝工装", DesignNotes: "深蓝工装为设计"}}})})
				case 5:
					if !strings.Contains(last.Content, "左眉浅疤") || !strings.Contains(last.Content, `"ok":true`) {
						t.Error("correction actual save receipt missing")
					}
					emitProjectContent(w, []d.Action{contentAction("finish_visual_prompts", struct{}{})})
				default:
					t.Error("unexpected extra provider request")
					http.Error(w, "extra", 500)
				}
			}))
			defer provider.Close()
			model, err := modeladapter.New(ctx, "fictional-loopback-key", provider.URL, md.DeepSeekModelID)
			if err != nil {
				t.Fatal(err)
			}
			s := contentService(t, store, model)
			s.Evidence = milvusevidence.NewLazy(factory)
			p, err = s.Create(ctx, "虚构检索循环", "fictional.txt", []byte("林岚是一名32岁的修理师，黑发低马尾。\n她摘下帽子。\n未关联原文：左眉尾浅疤清晰可见。\n无关庭院里，园丁在浇花。"), 0)
			if err != nil {
				t.Fatal(err)
			}
			cleanProjectContentRow(t, p)
			p.Assets = []d.Asset{{ID: "c", Kind: "character", Name: "林岚", Description: "32岁修理师，黑发低马尾", SourceIDs: []string{"v1-p000001"}}, {ID: "other", Kind: "prop", Name: "无关道具"}}
			p.Runs = []d.Run{{ID: "completed-p3", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "completed"}}
			p.Revision++
			if err = store.Commit(ctx, p, p.Revision-1); err != nil {
				t.Fatal(err)
			}
			before := d.Clone(p)
			if initializations.Load() != 0 {
				t.Fatal("upload initialized visual evidence resources")
			}
			if recovered {
				p, err = quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "single-visual", AssetID: "c"}).StartSingleVisual(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "c"})
				if err != nil {
					t.Fatal(err)
				}
				if err = index.Ensure(ctx, ports.EvidenceDocument{ProjectID: p.ID, Source: p.ScriptSource}); err != nil {
					t.Fatal(err)
				}
				hits, more, err := index.Search(ctx, ports.EvidenceDocument{ProjectID: p.ID, Source: p.ScriptSource}, "人物左眉旧伤和外貌", 1, 0)
				if err != nil {
					t.Fatal(err)
				}
				result, _ := json.Marshal(map[string]any{"ok": true, "matches": hits, "hasMore": more, "coverage": "ranked_candidates_not_complete", "sourceVersion": 1})
				a := contentAction("search_script_evidence", app.SearchScriptEvidence{Query: "人物左眉旧伤和外貌", Limit: 1})
				a.Status = "applied"
				a.Result = string(result)
				run := p.LatestRun()
				run.End = len(p.ScriptSource.Spans)
				run.Calls = []d.Call{{ID: identity.New("saved_read"), Stage: run.Stage, End: run.End, Status: "applied", FinishReason: "tool_calls", Actions: []d.Action{a}}}
				p.Revision++
				if err = store.Commit(ctx, p, p.Revision-1); err != nil {
					t.Fatal(err)
				}
				// Recreate the application service as on process restart. The
				// committed search result resumes; no old model call is resent.
				s = contentService(t, store, model)
				s.Evidence = milvusevidence.NewLazy(factory)
				if err = s.Execute(ctx, p.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				host, handler := httpapi.New(configuredFixture(s), nil, "../../../demo", 0)
				server := httptest.NewServer(handler)
				code, body := postProjectContent(t, server.URL, "/api/projects/"+p.ID+"/assets/c/visual-prompt-runs", map[string]any{"revision": p.Revision})
				if code != 202 {
					t.Fatalf("%d %s", code, body)
				}
				if err = host.WaitUntilIdle(ctx); err != nil {
					t.Fatal(err)
				}
				host.Shutdown(ctx)
				server.Close()
			}
			p, err = store.Snapshot(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			prompt := p.Prompt(d.VisualTarget{AssetID: "c"})
			if p.LatestRun().Status != "completed" || prompt == nil || prompt.Text != "32岁，左眉浅疤，黑发低马尾，深蓝工装" || !reflect.DeepEqual(before.Assets, p.Assets) || !reflect.DeepEqual(before.ScriptSource, p.ScriptSource) {
				t.Fatal("loop did not correct/preserve", p.LatestRun(), prompt)
			}
			want := int32(5)
			if recovered {
				want = 4
			}
			if count.Load() != want {
				t.Fatal("wrong actual HTTP call count", count.Load())
			}
			run := p.LatestRun()
			for i, c := range run.Calls {
				for _, a := range c.Actions {
					a = expectedEvidenceReceipt(t, p, c, a)
					if a.Result != "" && a.Name != "finish_visual_prompts" && i+1 < len(run.Calls) && !model.ContainsToolReceipt(run.Calls[i+1].Request, a, run.Calls[i]) {
						t.Error("persisted actual receipt absent from next HTTP request", a.Name)
					}
				}
			}
			if initializations.Load() != 1 {
				t.Fatal("visual loop did not reuse the initialized index", initializations.Load())
			}
			if folder := os.Getenv("DRAMA_TEST_EVIDENCE_DIR"); folder != "" {
				if err := os.MkdirAll(folder, 0755); err != nil {
					t.Fatal(err)
				}
				name := "scripted-loop.json"
				if recovered {
					name = "scripted-recovery.json"
				}
				raw, err := json.MarshalIndent(map[string]any{"textProvider": "scripted loopback fixture, not an actual LLM", "embedding": "local learned BGE", "project": p}, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(folder, name), raw, 0644); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("recovered=%v; real Eino/HTTP/PG/local-BGE/Milvus; calls=%d; source and assets unchanged", recovered, count.Load())
		})
	}
}

// 独立构造这两个只读工具的预期视图；完整回执的其他字段仍逐字参与核对。
func expectedEvidenceReceipt(t *testing.T, p d.Project, c d.Call, a d.Action) d.Action {
	t.Helper()
	if c.SourceReferenceFormat == "" || a.Name != "search_script_evidence" && a.Name != "read_script_evidence" || a.Result == "" {
		return a
	}
	if c.SourceReferenceFormat != "paragraph-ordinal-v1" {
		t.Fatal("unexpected source reference format", c.SourceReferenceFormat)
	}
	var receipt map[string]json.RawMessage
	if err := json.Unmarshal([]byte(a.Result), &receipt); err != nil {
		t.Fatal(err)
	}
	aliases := map[string]string{}
	for i, span := range p.ScriptSource.Spans {
		aliases[span.ID] = fmt.Sprintf("p%d", i+1)
	}
	for field, idKey := range map[string]string{"matches": "spanId", "source": "id"} {
		if raw, ok := receipt[field]; ok {
			var items []map[string]json.RawMessage
			if err := json.Unmarshal(raw, &items); err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				var id string
				if err := json.Unmarshal(item[idKey], &id); err != nil || aliases[id] == "" {
					t.Fatal("receipt source missing from original source", id, err)
				}
				item[idKey], _ = json.Marshal(aliases[id])
			}
			receipt[field], _ = json.Marshal(items)
		}
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	a.Result = string(raw)
	return a
}
