package project

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
)

func prepareVideoBreakdown(t *testing.T) (*Service, *fakeModel, d.Project) {
	t.Helper()
	s, m, p := p5Prepared(t)
	p.ProjectSettings = &d.ProjectSettings{VideoModel: "fixture-video"}
	var e error
	p, e = s.commit(context.Background(), p, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	s.Models.(*modelregistry.Registry).OptionsValue.Video = []ports.ModelOption{{ID: "fixture-video", Name: "免费测试视频", MediaProfile: &md.MediaProfile{Durations: []float64{8, 12, 4}, DefaultDuration: 4}}}
	return s, m, p
}

func TestBreakdownModesActualEinoRepairsDurationAndKeepsIndependentReview(t *testing.T) {
	for _, mode := range []string{"general", "story", "production"} {
		t.Run(mode, func(t *testing.T) {
			s, m, p := prepareVideoBreakdown(t)
			ctx := context.Background()
			p, e := s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown", BreakdownMode: mode})
			if e != nil {
				t.Fatal(e)
			}
			// 模拟重启后读取持久化 JSON，且后台能力已更改。任务仍须使用原模式和 12 秒。
			var restored d.Project
			if e = json.Unmarshal(mustJSON(p), &restored); e != nil {
				t.Fatal(e)
			}
			if restored.LatestRun().StoryboardScope.BreakdownMode != mode || restored.LatestRun().StoryboardScope.VideoConstraint.MaxDurationSeconds != 12 {
				t.Fatal("scope lost on persistence")
			}
			s.Models.(*modelregistry.Registry).OptionsValue.Video[0].MediaProfile.Durations = []float64{4}
			restarted := *s
			s = &restarted
			calls, reviewCalls := 0, 0
			var sceneID string
			var correct d.ShotValue
			m.generate = func(req ports.Request) (ports.Response, error) {
				calls++
				if calls > 10 {
					return ports.Response{}, errors.New("free fixture exhausted")
				}
				current, _ := s.Project(ctx, p.ID)
				r := current.LatestRun()
				instruction, _ := breakdownInstruction(mode)
				if !strings.Contains(req.System, instruction) || !strings.Contains(req.System, "最长 12 秒") || !strings.Contains(req.Input, `"breakdownMode":"`+mode+`"`) || !strings.Contains(req.Input, "complete_episode") || !strings.Contains(req.Input, "unused") {
					t.Fatal("selected mode, frozen cap or full material absent from actual request")
				}
				for _, phase := range []string{"P3", "P4", "P5", "独立审计产品", "人工审批"} {
					if strings.Contains(req.System+string(mustJSON(req.Tools)), phase) {
						t.Fatal("internal planning vocabulary leaked into actual model instructions", phase)
					}
				}
				if !strings.Contains(req.System, "逐镜从原文与当前实际动作、完整对白、停顿及摄影重新估算") || !strings.Contains(req.System, "不能把已填秒数、工具保存成功或生成者的估计当作证明") {
					t.Fatal("content-based timing instructions absent from actual generation/review request")
				}
				for _, tool := range req.Tools {
					if tool.Name == "save_storyboard" && !strings.Contains(string(tool.Parameters), `"maximum":12`) {
						t.Fatal("actual model schema lacks frozen maximum")
					}
				}
				refs := []d.VisualTarget{{AssetID: "c"}, {AssetID: "c2"}, {AssetID: "loc"}, {AssetID: "prop"}}
				if r.Stage == "storyboardReview" {
					reviewCalls++
					if reviewCalls == 1 {
						if len(req.Dialogue) != 0 || strings.Contains(req.Input, "GENERATOR_SELF_ASSESSMENT") || !strings.Contains(req.System, "独立复核上下文") {
							t.Fatal("generation history leaked into review")
						}
						return p5Response("read_assets", map[string]any{"targets": refs}), nil
					}
					if reviewCalls == 2 {
						return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "updateShot", "shotId": current.Storyboard.Shots[0].ID, "patch": map[string]any{"videoPrompt": map[string]any{"parts": p5Parts(false)}}}}}), nil
					}
					return p5Response("finish_storyboard", map[string]any{}), nil
				}
				switch calls {
				case 1:
					return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*current.Storyboard.Episode("e1"))}}}), nil
				case 2:
					sceneID = current.Storyboard.Scenes[0].ID
					return p5Response("read_assets", map[string]any{"targets": refs}), nil
				case 3:
					shot := p5Shot(*current.Storyboard.Episode("e1"))
					shot["durationSeconds"] = 12.01
					return p5Response("save_storyboard", map[string]any{"operations": []any{
						map[string]any{"op": "updateScene", "sceneId": sceneID, "patch": map[string]any{"summary": "must roll back"}},
						map[string]any{"op": "createShot", "sceneId": sceneID, "value": shot},
					}}), nil
				case 4:
					if len(current.Storyboard.Shots) != 0 || current.Storyboard.Scenes[0].Summary == "must roll back" || !strings.Contains(string(mustJSON(req.Dialogue)), "SHOT_DURATION_EXCEEDED") {
						t.Fatal("rejection was not atomic or not delivered to model")
					}
					shot := p5Shot(*current.Storyboard.Episode("e1"))
					shot["durationSeconds"] = 11.25
					shot["videoPrompt"] = map[string]any{"parts": p5Parts(true)}
					return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "value": shot}}}), nil
				case 5:
					correct = current.Storyboard.Shots[0].ShotValue
					out := p5Response("finish_storyboard", map[string]any{})
					out.Text = "GENERATOR_SELF_ASSESSMENT"
					return out, nil
				}
				return ports.Response{}, errors.New("unexpected fixture call")
			}
			if e = s.Execute(ctx, p.ID); e != nil {
				t.Fatal(e)
			}
			got, _ := s.Project(ctx, p.ID)
			if got.LatestRun().Status != d.RunCompleted || reviewCalls != 3 || calls != 8 {
				t.Fatalf("not completed after repair: %s %s", got.LatestRun().Status, got.LatestRun().Error)
			}
			sh := got.Storyboard.Shots[0]
			if sh.DurationSeconds == nil || *sh.DurationSeconds != 12 || !strings.Contains(got.LatestRun().Calls[3].Actions[0].Arguments, "11.25") || !strings.Contains(got.LatestRun().Calls[3].Actions[0].Result, `"durationSeconds":12`) {
				t.Fatal("fractional model duration was not rounded with original arguments retained")
			}
			if !reflect.DeepEqual(sh.ShotValue, correct) || !strings.Contains(string(mustJSON(sh.VideoPrompt)), "你自己决定") || len(got.Storyboard.Episode("e2").SceneIDs) != 0 {
				t.Fatal("review repair damaged correct content or another episode")
			}
			p5Record(t, got)
		})
	}
}

// 数值关卡不能裁决表演时长。夹具故意提交不合理的 12 秒，再脚本化复核修订，
// 验证隔离材料和真实保存链；这不是对真实 LLM 能否识别时间问题的质量测评。
func TestBreakdownNumericResubmissionAndIndependentReviewSplit(t *testing.T) {
	s, m, p := prepareVideoBreakdown(t)
	ctx := context.Background()
	line := "钥匙在这里，是否回来由你自己决定，我不会替你做这个决定。"
	p.ScriptSource.Spans[0].Text = "上集😀。林晚把铜钥匙放到桌上，与陈默沉默十秒，再对他说：" + line
	p.Storyboard.Episode("e1").SourceRanges = []d.SourceRange{p5Range("s1", p.ScriptSource.Spans[0].Text)}
	var err error
	p, err = s.commit(ctx, p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e2", "value": p5Scene(*p.Storyboard.Episode("e2"))})
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Scenes[0].ID, "value": p5Shot(*p.Storyboard.Episode("e2"))})
	otherShot := p.Storyboard.Shots[0]
	originalSource := p.ScriptSource
	p, err = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
	if err != nil {
		t.Fatal(err)
	}
	parts := func(ending string) []d.PromptPart {
		return []d.PromptPart{{Type: "asset", AssetID: "loc"}, {Type: "text", Text: "内，中景平视。"}, {Type: "asset", AssetID: "c"}, {Type: "text", Text: "站在桌边，"}, {Type: "asset", AssetID: "c2"}, {Type: "text", Text: "坐在桌子另一侧。"}, {Type: "asset", AssetID: "prop"}, {Type: "text", Text: ending}}
	}
	fullAction := "林晚放下铜钥匙，两人沉默十秒，然后她向陈默说出完整对白"
	speaker := "c"
	fullParts := parts("由林晚放到桌上，松手后两人沉默十秒，然后林晚说：" + line + "结束停在钥匙与陈默未伸出的手之间。")
	var sceneID, firstID string
	calls, reviews := 0, 0
	m.generate = func(req ports.Request) (ports.Response, error) {
		calls++
		if calls > 8 {
			return ports.Response{}, errors.New("free timing fixture exhausted")
		}
		current, _ := s.Project(ctx, p.ID)
		refs := []d.VisualTarget{{AssetID: "c"}, {AssetID: "c2"}, {AssetID: "loc"}, {AssetID: "prop"}}
		if current.LatestRun().Stage == "storyboardReview" {
			reviews++
			switch reviews {
			case 1:
				sh := current.Storyboard.Shot(firstID)
				// 保存时仅补只读显示名；比较模型可写的正文，不能把派生标签当成内容变化。
				savedParts := append([]d.PromptPart{}, sh.VideoPrompt.Parts...)
				for i := range savedParts {
					savedParts[i].LabelAtSave = ""
				}
				if sh.DurationSeconds == nil || *sh.DurationSeconds != 12 || sh.Action != fullAction || len(sh.Dialogues) != 1 || sh.Dialogues[0].Text != line || !reflect.DeepEqual(savedParts, fullParts) {
					t.Fatal("numeric-only resubmission was not actually saved unchanged")
				}
				if len(req.Dialogue) != 0 || strings.Contains(req.Input, "GENERATOR_SELF_ASSESSMENT") || strings.Contains(string(mustJSON(req.Dialogue)), "SHOT_DURATION_EXCEEDED") || !strings.Contains(req.Input, "沉默十秒") || !strings.Contains(req.Input, line) {
					t.Fatal("review did not receive clean original/current content")
				}
				return p5Response("read_assets", map[string]any{"targets": refs}), nil
			case 2:
				second := p5Shot(*current.Storyboard.Episode("e1"))
				second["action"] = "承接桌上钥匙与人物位置，林晚向陈默说出完整对白"
				second["dialogues"] = []d.Dialogue{{SpeakerAssetID: &speaker, Text: line}}
				second["durationSeconds"] = 12
				second["videoPrompt"] = map[string]any{"parts": parts("留在桌上，人物位置不变。林晚说：" + line + "结束停在钥匙与陈默未伸出的手之间。")}
				out := p5Response("save_storyboard", map[string]any{"operations": []any{
					map[string]any{"op": "updateShot", "shotId": firstID, "patch": map[string]any{"action": "林晚放下铜钥匙，松手后两人沉默十秒", "dialogues": []d.Dialogue{}, "durationSeconds": 11, "designNotes": "结束停在桌上钥匙，两人位置不变，承接后续对白", "videoPrompt": map[string]any{"parts": parts("由林晚放到桌上，松手后两人沉默十秒；结束停在钥匙上，两人位置不变。")}}},
					map[string]any{"op": "createShot", "sceneId": sceneID, "afterShotId": firstID, "value": second},
				}})
				out.Text = "该镜包含原文明确的十秒沉默及完整对白，12 秒无法自然完成；保留沉默，将后续完整对白放在相邻连续镜头。"
				return out, nil
			case 3:
				if len(current.Storyboard.Scene(sceneID).ShotIDs) != 2 || !strings.Contains(string(mustJSON(req.Dialogue)), "承接桌上钥匙与人物位置") {
					t.Fatal("review finished without receiving actual split save receipt")
				}
				return p5Response("finish_storyboard", map[string]any{}), nil
			}
		}
		switch calls {
		case 1:
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*current.Storyboard.Episode("e1"))}}}), nil
		case 2:
			sceneID = current.Storyboard.Episode("e1").SceneIDs[0]
			return p5Response("read_assets", map[string]any{"targets": refs}), nil
		case 3, 4:
			if calls == 4 && (len(current.Storyboard.Scene(sceneID).ShotIDs) != 0 || !strings.Contains(string(mustJSON(req.Dialogue)), "SHOT_DURATION_EXCEEDED")) {
				t.Fatal("over-limit response did not reject and deliver error")
			}
			shot := p5Shot(*current.Storyboard.Episode("e1"))
			shot["action"], shot["dialogues"] = fullAction, []d.Dialogue{{SpeakerAssetID: &speaker, Text: line}}
			shot["videoPrompt"] = map[string]any{"parts": fullParts}
			shot["durationSeconds"] = 13
			if calls == 4 {
				shot["durationSeconds"] = 12
			}
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "value": shot}}}), nil
		case 5:
			firstID = current.Storyboard.Scene(sceneID).ShotIDs[0]
			out := p5Response("finish_storyboard", map[string]any{})
			out.Text = "GENERATOR_SELF_ASSESSMENT"
			return out, nil
		}
		return ports.Response{}, errors.New("unexpected timing fixture call")
	}
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Project(ctx, p.ID)
	if got.LatestRun().Status != d.RunCompleted || calls != 8 || reviews != 3 {
		t.Fatalf("split review did not complete: status=%s error=%s calls=%d reviews=%d", got.LatestRun().Status, got.LatestRun().Error, calls, reviews)
	}
	ids := got.Storyboard.Scene(sceneID).ShotIDs
	first, second := got.Storyboard.Shot(ids[0]), got.Storyboard.Shot(ids[1])
	if ids[0] != firstID || len(first.Dialogues) != 0 || *first.DurationSeconds != 11 || len(second.Dialogues) != 1 || second.Dialogues[0].Text != line || *second.DurationSeconds != 12 || !strings.Contains(string(mustJSON(first.VideoPrompt.Parts)), "十秒") || !strings.Contains(string(mustJSON(second.VideoPrompt.Parts)), line) {
		t.Fatal("actual content split or complete dialogue was lost")
	}
	if !reflect.DeepEqual(*got.Storyboard.Shot(otherShot.ID), otherShot) || !reflect.DeepEqual(got.ScriptSource, originalSource) || first.Camera != otherShot.Camera || len(got.VideoPromptIssues(first, "")) != 0 || len(got.VideoPromptIssues(second, "")) != 0 {
		t.Fatal("review damaged preserved content, original source, camera or prompt validity")
	}
	p5Record(t, got)
}

func TestBreakdownModeDefaultsStrictInputsAndVideoAvailability(t *testing.T) {
	for _, raw := range []string{
		`{"revision":1,"operation":"breakdown","breakdownMode":"unknown"}`,
		`{"revision":1,"operation":"breakdown","breakdownMode":""}`,
		`{"revision":1,"operation":"breakdown","breakdownMode":null}`,
		`{"revision":1,"operation":"fillPrompts","breakdownMode":"story"}`,
		`{"revision":1,"operation":"regeneratePrompts","shotIds":["x"],"breakdownMode":"general"}`,
		`{"revision":1,"operation":"breakdown","maxDurationSeconds":100}`,
	} {
		var input EpisodeRunInput
		if DecodeStoryboard([]byte(raw), "EpisodeRunInput", &input) == nil {
			t.Fatal("accepted invalid mode or client cap", raw)
		}
	}
	s, _, p := p5Prepared(t)
	p, e := s.StartEpisode(context.Background(), p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
	if e != nil || p.LatestRun().StoryboardScope.BreakdownMode != "general" || p.LatestRun().StoryboardScope.VideoConstraint != nil {
		t.Fatal("omitted mode or optional video broken", e)
	}
	for _, state := range []string{"missing", "no-durations", "retired"} {
		t.Run(state, func(t *testing.T) {
			s, _, p := prepareVideoBreakdown(t)
			model := &s.Models.(*modelregistry.Registry).OptionsValue.Video[0]
			switch state {
			case "missing":
				model.ID = "other"
			case "no-durations":
				model.MediaProfile.Durations = nil
			case "retired":
				model.MediaProfile.Retired = true
			}
			_, e := s.StartEpisode(context.Background(), p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
			var problem *fault.Error
			if !errors.As(e, &problem) || problem.Code != "VIDEO_MODEL_UNAVAILABLE" {
				t.Fatal(e)
			}
			got, _ := s.Project(context.Background(), p.ID)
			if !reflect.DeepEqual(got, p) {
				t.Fatal("refused start mutated project")
			}
		})
	}
}

func TestBreakdownQuoteBindsModeAndFrozenVideoMaximum(t *testing.T) {
	s, _, p := prepareVideoBreakdown(t)
	s.Prices = scopeFixturePrices{}
	ctx := context.Background()
	input := QuoteInput{Revision: p.Revision, Operation: "breakdown", EpisodeID: "e1"}
	general, e := s.Quote(ctx, p.ID, input)
	if e != nil {
		t.Fatal(e)
	}
	input.BreakdownMode = "general"
	explicit, e := s.Quote(ctx, p.ID, input)
	if e != nil || explicit.Basis != general.Basis {
		t.Fatal("default quote differs", e)
	}
	input.BreakdownMode = "story"
	story, e := s.Quote(ctx, p.ID, input)
	if e != nil || story.Basis == general.Basis || story.Amount != general.Amount {
		t.Fatal("mode not bound or text quantity changed", e)
	}
	s.Models.(*modelregistry.Registry).OptionsValue.Video[0].MediaProfile.Durations = []float64{8}
	changed, e := s.Quote(ctx, p.ID, input)
	if e != nil || changed.Basis == story.Basis {
		t.Fatal("changed capability not bound", e)
	}
	got, _ := s.Project(ctx, p.ID)
	if !reflect.DeepEqual(got, p) {
		t.Fatal("quote created a run")
	}
}

func TestBreakdownFinishRequiresAnActualDurationForEveryRetainedShot(t *testing.T) {
	s, _, p := prepareVideoBreakdown(t)
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))})
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Scenes[0].ID, "value": p5Shot(*p.Storyboard.Episode("e1"))})
	p, e := s.StartEpisode(context.Background(), p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
	if e != nil {
		t.Fatal(e)
	}
	var problem *fault.Error
	if e = p.StoryboardComplete(p.LatestRun()); !errors.As(e, &problem) || problem.Code != "SHOT_DURATION_REQUIRED" {
		t.Fatal("null estimate passed finish", e)
	}
	if p.Storyboard.Shots[0].DurationSeconds != nil {
		t.Fatal("program fabricated a duration")
	}
}
