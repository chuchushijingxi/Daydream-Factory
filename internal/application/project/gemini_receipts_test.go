package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

// 通过真实 Eino/Gemini SDK 与业务结束门；只向本机脚本服务发送虚构材料。
func TestGeminiNativeBusinessReceiptsFinishContentAndStoryboard(t *testing.T) {
	for _, nativeIDs := range []bool{true, false} {
		for _, business := range []string{"outline", "visual", "episode-index", "storyboard"} {
			t.Run(fmt.Sprintf("%s/nativeIDs=%t", business, nativeIDs), func(t *testing.T) {
				s, _, p := p5Prepared(t)
				ctx := context.Background()
				calls := 0
				var sceneID string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if calls > 12 {
						w.WriteHeader(500)
						return
					}
					current, err := s.Project(ctx, p.ID)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					run := current.LatestRun()
					name := ""
					args := any(map[string]any{})
					switch business {
					case "outline":
						name = "finish_outline"
						if calls == 1 {
							name = "save_outline"
							args = map[string]any{"outline": d.OutlineInput{Protagonist: "林晚", Summary: "她放下钥匙，由对方决定。"}}
						}
					case "visual":
						name = "finish_visual_prompts"
						if calls == 1 {
							name = "save_visual_prompts"
							args = map[string]any{"prompts": []VisualPromptInput{{Target: VisualTargetInput{AssetID: "prop"}, Text: "完整铜钥匙外观", DesignNotes: "免费原生回执验证"}}}
						}
					case "episode-index":
						name = "finish_episode_index"
						if calls == 1 {
							name = "save_episode_ranges"
							args = map[string]any{"operations": []any{map[string]any{"op": "setSegmentation", "segmentation": "whole"}, map[string]any{"op": "createEpisode", "value": d.EpisodeValue{Title: "整篇", SourceRanges: []d.SourceRange{{Start: d.SourcePoint{SpanID: "p1"}, End: d.SourcePoint{SpanID: "p2", Offset: len([]rune(current.ScriptSource.Spans[1].Text))}}}}}}}
						}
					case "storyboard":
						if strings.HasSuffix(run.Stage, "Review") {
							name = "finish_storyboard"
							if len(run.Calls) == 0 {
								t.Error("missing review call")
							}
							if len(run.Calls) == 1 || run.Calls[len(run.Calls)-2].Stage != run.Stage {
								name = "read_assets"
								args = map[string]any{"targets": []d.VisualTarget{{AssetID: "c"}, {AssetID: "c2"}, {AssetID: "loc"}, {AssetID: "prop"}}}
							}
						} else {
							switch calls {
							case 1:
								name = "read_assets"
								args = map[string]any{"targets": []d.VisualTarget{{AssetID: "c"}, {AssetID: "c2"}, {AssetID: "loc"}, {AssetID: "prop"}}}
							case 2:
								name = "save_storyboard"
								value := p5Scene(*current.Storyboard.Episode("e1"))
								value["sourceRanges"] = []d.SourceRange{p5Range("p1", current.ScriptSource.Spans[0].Text)}
								args = map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": "e1", "value": value}}}
							case 3:
								name = "save_storyboard"
								sceneID = current.Storyboard.Scenes[0].ID
								value := p5Shot(*current.Storyboard.Episode("e1"))
								value["sourceRanges"] = []d.SourceRange{p5Range("p1", current.ScriptSource.Spans[0].Text)}
								args = map[string]any{"operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "value": value}}}
							default:
								name = "finish_storyboard"
							}
						}
					}
					w.Header().Set("Content-Type", "application/json")
					id := ""
					if nativeIDs {
						id = fmt.Sprintf("native-%d", calls)
					}
					json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"functionCall": map[string]any{"id": id, "name": name, "args": args}, "thoughtSignature": "c2ln"}}}, "finishReason": "STOP"}}, "usageMetadata": map[string]int{"promptTokenCount": 20, "candidatesTokenCount": 10, "totalTokenCount": 30}})
				}))
				defer server.Close()
				preset, _ := md.OfficialPreset("google")
				var profile md.ConfiguredModel
				for _, v := range preset.Models {
					if v.ModelID == md.GeminiFlashModelID {
						profile = v
					}
				}
				model, err := modeladapter.NewGemini(ctx, "fictional-local-only", server.URL+"/v1beta", profile)
				if err != nil {
					t.Fatal(err)
				}
				reg := registry(s)
				reg.DefaultModel = profile.ID
				reg.Models = map[string]ports.Model{profile.ID: model}
				reg.ModelPrices = map[string]billing.TokenPricing{profile.ID: {}}
				before := d.Clone(p)
				switch business {
				case "outline":
					p, err = s.StartOutline(ctx, p.ID, p.Revision)
				case "visual":
					p, err = s.StartSingleVisual(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "prop"})
				case "episode-index":
					p.Storyboard = d.NewStoryboard(p.ScriptSource.Version)
					p.Runs = nil
					s.initialEpisodeIndex(ctx, &p)
					p, err = s.commit(ctx, p, p.Revision)
				case "storyboard":
					p, err = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Execute(ctx, p.ID); err != nil {
					t.Fatal(err)
				}
				got, err := s.Project(ctx, p.ID)
				if err != nil || got.LatestRun().Status != d.RunCompleted {
					t.Fatal(got.LatestRun(), err)
				}
				if !reflect.DeepEqual(got.ScriptSource, before.ScriptSource) || !reflect.DeepEqual(got.Assets, before.Assets) {
					t.Fatal("content task changed source or asset facts")
				}
				if business == "storyboard" && (len(got.Storyboard.Shots) != 1 || !got.ValidPrompt(got.Prompt(d.VisualTarget{AssetID: "prop"}), "")) {
					t.Fatal("storyboard lost content")
				}
				for _, call := range got.LatestRun().Calls {
					if len(call.Request) == 0 || call.Response == "" || call.ProviderState == "" || !call.Usage.Known {
						t.Fatal("native request/response/usage evidence missing")
					}
					for _, a := range call.Actions {
						if a.Status != "applied" || a.Result == "" {
							t.Fatal("business receipt was rejected", a)
						}
					}
				}
				t.Logf("business=%s calls=%d completed with native wire and saved receipts", business, calls)
			})
		}
	}
}
