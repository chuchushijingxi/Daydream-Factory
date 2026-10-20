package project

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func TestStoryboardInstructionsMatchApprovedFixtures(t *testing.T) {
	for fixture, actual := range map[string]string{"storyboard-main": storyboardPrompt, "storyboard-review": storyboardReviewPrompt} {
		raw, err := os.ReadFile("testdata/contracts/" + fixture + ".txt")
		if err != nil || string(raw) != actual {
			t.Fatal("approved instruction differs from production", fixture, err)
		}
	}
	for _, op := range []string{"breakdown", "fillPrompts", "regeneratePrompts"} {
		raw, err := os.ReadFile("testdata/contracts/" + op + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		actual, err := storyboardTaskInstruction(&d.StoryboardScope{Operation: op, BreakdownMode: "general"})
		if err != nil || string(raw) != strings.TrimPrefix(actual, "\n") {
			t.Fatal("approved operation differs from production", op, err)
		}
	}
}

// 真实 Eino 请求与保存链；错误及修复由免费脚本给出，不证明模型自主识错。
func TestStoryboardContinuityReviewRepairsHandAndRepeatedAction(t *testing.T) {
	s, m, p := p5Prepared(t)
	ctx := context.Background()
	p.ScriptSource.Spans[0].Text = "林晚站在桌南，右手将铜钥匙交给桌北陈默的左手，松手。陈默握住钥匙说：我会回来。两人沉默五秒。"
	p.Storyboard.Episode("e1").SourceRanges = []d.SourceRange{p5Range("s1", p.ScriptSource.Spans[0].Text)}
	var err error
	p, err = s.commit(ctx, p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e2", "value": p5Scene(*p.Storyboard.Episode("e2"))})
	otherScene := p.Storyboard.Episode("e2").SceneIDs[0]
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": otherScene, "value": p5Shot(*p.Storyboard.Episode("e2"))})
	other := *p.Storyboard.Shot(p.Storyboard.Scene(otherScene).ShotIDs[0])
	originalSource, originalAssets := p.ScriptSource, p.Assets
	p, err = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
	if err != nil {
		t.Fatal(err)
	}
	refs := []d.VisualTarget{{AssetID: "c"}, {AssetID: "c2"}, {AssetID: "loc"}, {AssetID: "prop"}}
	parts := func(end string) []d.PromptPart {
		return []d.PromptPart{{Type: "asset", AssetID: "loc"}, {Type: "text", Text: "内，中景平视。"}, {Type: "asset", AssetID: "c"}, {Type: "text", Text: "在桌南，"}, {Type: "asset", AssetID: "c2"}, {Type: "text", Text: "在桌北，"}, {Type: "asset", AssetID: "prop"}, {Type: "text", Text: end}}
	}
	goodAction := "林晚已松手，陈默左手握住铜钥匙，说：我会回来。两人保持五秒沉默。"
	goodParts := parts("已在陈默左手中，林晚右手已松开。镜头观察陈默，他说：我会回来。两人保持五秒沉默，钥匙仍由陈默左手握住。")
	var first, beforeRepair d.Shot
	var targetID string
	calls, reviews := 0, 0
	m.generate = func(req ports.Request) (ports.Response, error) {
		calls++
		current, _ := s.Project(ctx, p.ID)
		if !strings.HasSuffix(req.System, storyboardPrompt+mustStoryboardInstruction(t, current.LatestRun().StoryboardScope)+modelSourceInstruction) || !strings.Contains(req.Input, "complete_episode") || !strings.Contains(req.Input, "unused") || !strings.Contains(req.Input, "沉默五秒") {
			t.Fatal("actual request lacks complete source, catalog or approved instructions")
		}
		if current.LatestRun().Stage == "storyboardReview" {
			reviews++
			switch reviews {
			case 1:
				if len(req.Dialogue) != 0 || !strings.HasPrefix(req.System, storyboardReviewPrompt+"\n") || strings.Contains(req.Input, "GENERATOR_HISTORY_SENTINEL") || !strings.Contains(req.CurrentState, "GENERATOR_DESIGN_ASSERTION") {
					t.Fatal("review must isolate dialogue while retaining designNotes to inspect")
				}
				return p5Response("read_assets", map[string]any{"targets": refs}), nil
			case 2:
				return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "updateShot", "shotId": targetID, "patch": map[string]any{"action": goodAction, "videoPrompt": map[string]any{"parts": goodParts}}}}}), nil
			case 3:
				if !strings.Contains(string(mustJSON(req.Dialogue)), goodAction) || current.Storyboard.Shot(targetID).Action != goodAction {
					t.Fatal("review did not receive the actual repair receipt")
				}
				return p5Response("finish_storyboard", map[string]any{}), nil
			}
		}
		switch calls {
		case 1:
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*current.Storyboard.Episode("e1"))}}}), nil
		case 2:
			return p5Response("read_assets", map[string]any{"targets": refs}), nil
		case 3:
			ep := *current.Storyboard.Episode("e1")
			firstValue, secondValue := p5Shot(ep), p5Shot(ep)
			firstValue["action"] = "林晚右手递出铜钥匙，陈默左手接稳，林晚松手。"
			firstValue["dialogues"] = []d.Dialogue{}
			firstValue["videoPrompt"] = map[string]any{"parts": parts("由林晚右手递给陈默左手；陈默接稳后林晚松手，钥匙留在陈默左手。")}
			secondValue["action"] = "林晚重新递钥匙，陈默再次伸右手接过，说：我会回来。两人沉默五秒。"
			speaker := "c2"
			secondValue["dialogues"] = []d.Dialogue{{SpeakerAssetID: &speaker, Text: "我会回来。"}}
			secondValue["designNotes"] = "GENERATOR_DESIGN_ASSERTION：已核对动作与持物正确。"
			secondValue["videoPrompt"] = map[string]any{"parts": parts("又由林晚递出，陈默重新伸右手接过，说：我会回来。两人沉默五秒。")}
			sceneID := ep.SceneIDs[0]
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "value": firstValue}, map[string]any{"op": "createShot", "sceneId": sceneID, "value": secondValue}}}), nil
		case 4:
			ids := current.Storyboard.Scene(current.Storyboard.Episode("e1").SceneIDs[0]).ShotIDs
			first, beforeRepair = *current.Storyboard.Shot(ids[0]), *current.Storyboard.Shot(ids[1])
			targetID = ids[1]
			out := p5Response("finish_storyboard", map[string]any{})
			out.Text = "GENERATOR_HISTORY_SENTINEL：不需要修改"
			return out, nil
		}
		return ports.Response{}, errors.New("free continuity fixture exhausted")
	}
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Project(ctx, p.ID)
	if got.LatestRun().Status != d.RunCompleted || calls != 7 || reviews != 3 {
		t.Fatalf("continuity repair did not finish: %s %s", got.LatestRun().Status, got.LatestRun().Error)
	}
	repaired := got.Storyboard.Shot(targetID)
	wantValue := beforeRepair.ShotValue
	wantValue.Action = goodAction
	if !reflect.DeepEqual(repaired.ShotValue, wantValue) || !reflect.DeepEqual(first, *got.Storyboard.Shot(first.ID)) || !reflect.DeepEqual(other, *got.Storyboard.Shot(other.ID)) || !reflect.DeepEqual(originalSource, got.ScriptSource) || !reflect.DeepEqual(originalAssets, got.Assets) {
		t.Fatal("repair changed correct inputs, adjacent shot, another episode or upstream material")
	}
	view, err := s.StoryboardEpisode(got, "e1")
	if err != nil || len(view.Shots) != 2 || !view.Shots[1].VideoPrompt.Valid || !strings.Contains(view.Shots[1].VideoPrompt.PlainText, "陈默左手") || !strings.Contains(view.Shots[1].VideoPrompt.PlainText, "五秒沉默") {
		t.Fatal("readback lost hand continuity, dialogue or pause", err)
	}
	p5Record(t, got)
}

func mustStoryboardInstruction(t *testing.T, scope *d.StoryboardScope) string {
	t.Helper()
	text, err := storyboardTaskInstruction(scope)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestStoryboardPromptTasksEinoKeepFrozenInputsAndRepairOnlyTargets(t *testing.T) {
	for _, op := range []string{"fillPrompts", "regeneratePrompts"} {
		t.Run(op, func(t *testing.T) {
			s, m, p := p5Prepared(t)
			ctx := context.Background()
			p.ProjectSettings = &d.ProjectSettings{VideoModel: "unavailable-video-must-not-be-resolved"}
			var err error
			p, err = s.commit(ctx, p, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))})
			sceneID := p.Storyboard.Episode("e1").SceneIDs[0]
			p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": sceneID, "value": p5Shot(*p.Storyboard.Episode("e1"))}, map[string]any{"op": "createShot", "sceneId": sceneID, "value": p5Shot(*p.Storyboard.Episode("e1"))})
			ids := p.Storyboard.Scene(sceneID).ShotIDs
			if op == "fillPrompts" {
				p, _ = p5Change(t, s, p, map[string]any{"op": "updateShot", "shotId": ids[0], "patch": map[string]any{"videoPrompt": nil}})
			}
			target, neighbor := *p.Storyboard.Shot(ids[0]), *p.Storyboard.Shot(ids[1])
			assets, source := p.Assets, p.ScriptSource
			in := EpisodeRunInput{Revision: p.Revision, Operation: op}
			if op == "regeneratePrompts" {
				in.ShotIDs = []string{target.ID}
			}
			p, err = s.StartEpisode(ctx, p.ID, "e1", in)
			if err != nil {
				t.Fatal(err)
			}
			attempts := []map[string]any{{"action": "越权动作"}, {"dialogues": []d.Dialogue{}}, {"camera": d.Camera{Size: "特写"}}, {"designNotes": "越权设计"}, {"durationSeconds": 1}, {"selectedAssets": []d.VisualTarget{}}}
			parts := p5Parts(false)
			parts[len(parts)-1].Text += "陈默仍在桌子另一侧，钥匙保持桌面位置。"
			calls, reviews := 0, 0
			m.generate = func(req ports.Request) (ports.Response, error) {
				calls++
				current, _ := s.Project(ctx, p.ID)
				scope := current.LatestRun().StoryboardScope
				if scope.Operation != op || scope.VideoConstraint != nil || len(scope.ShotIDs) != 1 || scope.ShotIDs[0] != target.ID || !strings.HasSuffix(req.System, storyboardPrompt+mustStoryboardInstruction(t, scope)+modelSourceInstruction) || strings.Contains(req.System, "本次拆解模式") || strings.Contains(req.System, "本次生成当前集完整拆解") {
					t.Fatal("prompt-only request received breakdown instructions or a new video constraint")
				}
				if !reflect.DeepEqual(target.ShotValue, current.Storyboard.Shot(target.ID).ShotValue) || !reflect.DeepEqual(neighbor, *current.Storyboard.Shot(neighbor.ID)) {
					t.Fatal("a rejected write changed frozen inputs or neighbor")
				}
				if current.LatestRun().Stage == "storyboardReview" {
					reviews++
					if reviews == 1 {
						if len(req.Dialogue) != 0 || !strings.HasPrefix(req.System, storyboardReviewPrompt+"\n") {
							t.Fatal("prompt-only review inherited generation dialogue")
						}
						return p5Response("read_assets", map[string]any{"targets": target.SelectedAssets}), nil
					}
					return p5Response("finish_storyboard", map[string]any{}), nil
				}
				if calls == 1 {
					return p5Response("read_assets", map[string]any{"targets": target.SelectedAssets}), nil
				}
				if calls > 2 && calls <= 10 {
					if !strings.Contains(req.Dialogue[len(req.Dialogue)-1].Content, `"ok":false`) {
						t.Fatal("model did not receive actual rejection")
					}
				}
				patch := map[string]any{"videoPrompt": map[string]any{"parts": parts}}
				shotID := target.ID
				switch {
				case calls >= 2 && calls <= 7:
					for key, value := range attempts[calls-2] {
						patch[key] = value
					}
				case calls == 8:
					shotID = neighbor.ID
				case calls == 9:
					patch["videoPrompt"] = nil
				case calls == 10:
				case calls == 11:
					return p5Response("finish_storyboard", map[string]any{}), nil
				default:
					return ports.Response{}, errors.New("free prompt-only fixture exhausted")
				}
				return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "updateShot", "shotId": shotID, "patch": patch}}}), nil
			}
			if err = s.Execute(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Project(ctx, p.ID)
			if got.LatestRun().Status != d.RunCompleted || calls != 13 || reviews != 2 || !reflect.DeepEqual(assets, got.Assets) || !reflect.DeepEqual(source, got.ScriptSource) || !reflect.DeepEqual(target.ActionRef, got.Storyboard.Shot(target.ID).ActionRef) {
				t.Fatalf("prompt-only flow damaged material or failed: %s %s", got.LatestRun().Status, got.LatestRun().Error)
			}
			view, err := s.StoryboardEpisode(got, "e1")
			if err != nil || !view.Shots[0].VideoPrompt.Valid || !strings.Contains(view.Shots[0].VideoPrompt.PlainText, "钥匙保持桌面位置") {
				t.Fatal("valid target prompt did not survive readback", err)
			}
			p5Record(t, got)
		})
	}
}

func TestStoryboardPromptCapacityCountsFullInstructionsWithoutDroppingMaterial(t *testing.T) {
	for _, op := range []string{"breakdown", "fillPrompts", "regeneratePrompts"} {
		for _, review := range []bool{false, true} {
			name := op
			if review {
				name += "Review"
			}
			t.Run(name, func(t *testing.T) {
				s, m, p := p5Prepared(t)
				ctx := context.Background()
				p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))})
				value := p5Shot(*p.Storyboard.Episode("e1"))
				value["videoPrompt"] = nil
				p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Episode("e1").SceneIDs[0], "value": value})
				input := EpisodeRunInput{Revision: p.Revision, Operation: op}
				if op == "regeneratePrompts" {
					input.ShotIDs = []string{p.Storyboard.Shots[0].ID}
				}
				var err error
				p, err = s.StartEpisode(ctx, p.ID, "e1", input)
				if err != nil {
					t.Fatal(err)
				}
				if review {
					p.LatestRun().Stage += "Review"
				}
				full, _, err := s.storyboardRequest(ctx, p)
				if err != nil {
					t.Fatal(err)
				}
				if review != strings.HasPrefix(full.System, storyboardReviewPrompt+"\n") {
					t.Fatal("review routing does not match stage")
				}
				needed := inputSize(full) + len(mustJSON(full.Tools))
				m.window, m.output = needed+1200, 2000
				bounded, _, err := s.storyboardRequest(ctx, p)
				if err != nil || bounded.MaxOutputTokens != 1200 || full.System != bounded.System || full.Input != bounded.Input || !reflect.DeepEqual(full.Tools, bounded.Tools) {
					t.Fatal("capacity ignored instructions or dropped material", err)
				}
				m.window = needed
				rejected, _, err := s.storyboardRequest(ctx, p)
				if !errors.Is(err, fault.ErrCapacity) || rejected.Input != full.Input || m.count != 0 {
					t.Fatal("capacity failure truncated source or invoked provider", err)
				}
				t.Logf("%s: system=%d bytes, full request+tools=%d conservative units, output headroom=1200; exact boundary rejected before provider", name, len(full.System), needed)
			})
		}
	}
}

func TestStoryboardPromptConflictDoesNotFinishOrRewriteFrozenInputs(t *testing.T) {
	s, m, p := p5Prepared(t)
	ctx := context.Background()
	p.ScriptSource.Spans[0].Text = "林晚右手将铜钥匙交给陈默左手，陈默说：我会回来。"
	p.Storyboard.Episode("e1").SourceRanges = []d.SourceRange{p5Range("s1", p.ScriptSource.Spans[0].Text)}
	var err error
	p, err = s.commit(ctx, p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))})
	value := p5Shot(*p.Storyboard.Episode("e1"))
	value["action"] = "陈默右手接过铜钥匙。"
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Episode("e1").SceneIDs[0], "value": value})
	before := p.Storyboard
	p, err = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "regeneratePrompts", ShotIDs: []string{p.Storyboard.Shots[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	// 夹具明确给出冲突说明；现有协议把无合法工具结束作为失败，不新增状态或工具。
	reason := "冻结镜头动作写陈默右手接钥匙，完整原文写陈默左手接钥匙；正文任务不能修改 action，无法提交一致的正文。"
	m.generate = func(req ports.Request) (ports.Response, error) {
		if !strings.Contains(req.Input, "陈默左手") || !strings.Contains(req.CurrentState, "陈默右手") || !strings.Contains(req.System, "指出具体阻断") {
			t.Fatal("conflict material or read-only instruction absent")
		}
		return ports.Response{Text: reason, Raw: mustJSON(map[string]any{"freeFixture": true, "reason": reason}), FinishReason: "stop", Usage: billing.Usage{Known: true}}, nil
	}
	_ = s.Execute(ctx, p.ID)
	got, _ := s.Project(ctx, p.ID)
	r := got.LatestRun()
	if r.Status != d.RunFailed || m.count != 1 || !reflect.DeepEqual(before, got.Storyboard) || len(r.Calls) != 1 || r.Calls[0].Text != reason || !strings.Contains(r.Calls[0].Response, reason) {
		t.Fatal("conflict lost its explanation, falsely completed or rewrote inputs")
	}
	p5Record(t, got)
}
