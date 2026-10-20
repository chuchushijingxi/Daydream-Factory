package project

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func TestAssetPreparationOneRunCorrectsFactsThenDesignAndPreservesGoodContent(t *testing.T) {
	ctx := context.Background()
	m := &fakeModel{}
	s, p := fixture(t, m)
	m.catalogVisual = false
	p.ScriptSource.Spans = []d.SourceSpan{{ID: "s1", Text: "林岚左眉有疤，穿灰色外套。修理间东侧有木门，门边挂着有缺口的铜钥匙。"}}
	p, err := s.commit(ctx, p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	initialID := p.LatestRun().ID
	var stableAssets []d.Asset
	goodText := "左眉细疤的成年女子，灰色外套，短发，直立平静。"
	locationText := "无人修理间，近处旧工作台，中段空地，远处东侧木门。"
	m.generate = func(req ports.Request) (ports.Response, error) {
		current, _ := s.Project(ctx, p.ID)
		if current.LatestRun().ID != initialID || current.LatestRun().Status != "running" {
			t.Fatal("phase completed or replaced run before design review")
		}
		switch m.count {
		case 1:
			if req.System != ExtractPrompt+modelSourceInstruction {
				t.Fatal("extraction prompt changed")
			}
			return response(`{"assets":[{"kind":"character","name":"林岚","description":"灰衣女子，右眉有疤","sourceIds":["s1"]},{"kind":"location","name":"修理间","description":"东侧木门","sourceIds":["s1"]},{"kind":"prop","name":"铜钥匙","description":"缺口铜钥匙","sourceIds":["s1"]}]}`), nil
		case 2:
			if req.System != CorrectPrompt+modelSourceInstruction || current.LatestRun().Stage != "correct" {
				t.Fatal("missing correction phase")
			}
			a := current.Assets[0]
			a.Description = "灰衣女子，左眉有疤"
			raw, _ := json.Marshal(CatalogChangeSet{Assets: catalogInputs([]d.Asset{a})})
			return response(string(raw)), nil
		case 3, 4, 5:
			if req.System != visualPrompt+modelSourceInstruction || current.LatestRun().Stage != "visualPrompt" || len(current.LatestRun().Targets) != 3 || current.LatestRun().Cursor != 0 {
				t.Fatal("design phase not frozen within original run")
			}
			if m.count == 3 {
				stableAssets = current.Assets
			}
			if !reflect.DeepEqual(stableAssets, current.Assets) {
				t.Fatal("visual model changed factual catalog")
			}
			if m.count == 5 {
				if len(req.Dialogue) != 4 || current.Prompt(d.VisualTarget{AssetID: current.Assets[2].ID}).Text != "铜质钥匙本体，齿端有缺口，简单衬底，完整可见。" {
					t.Fatal("review did not see real saved correction")
				}
				out := response("")
				out.Actions = []d.Action{action("finish_visual_prompts", struct{}{})}
				return out, nil
			}
			prompts := []d.VisualPromptInput{{Target: d.VisualTarget{AssetID: current.Assets[0].ID}, Text: goodText, DesignNotes: "短发为补充设计"}, {Target: d.VisualTarget{AssetID: current.Assets[1].ID}, Text: locationText, DesignNotes: "工作台为补充设计"}, {Target: d.VisualTarget{AssetID: current.Assets[2].ID}, Text: "银色完整钥匙，无缺口。", DesignNotes: "待核对"}}
			if m.count == 4 {
				if len(req.Dialogue) != 2 || !strings.Contains(req.Input, "银色完整钥匙") {
					t.Fatal("model lacked actual saved draft/receipt")
				}
				prompts = prompts[2:]
				prompts[0].Text = "铜质钥匙本体，齿端有缺口，简单衬底，完整可见。"
				prompts[0].DesignNotes = "按原文明示材质、缺口修正；衬底为设计"
			}
			raw, _ := json.Marshal(map[string]any{"prompts": prompts})
			out := response("")
			out.Actions = []d.Action{{ID: "visual-save-" + string(rune('0'+m.count)), Name: "save_visual_prompts", Arguments: string(raw), Status: "pending"}}
			return out, nil
		}
		t.Fatal("unexpected extra call")
		return ports.Response{}, errors.New("extra")
	}
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	result, _ := s.Project(ctx, p.ID)
	if len(result.Runs) != 1 || result.LatestRun().ID != initialID || result.LatestRun().Status != "completed" || len(result.LatestRun().Calls) != 5 || len(result.PendingVisualTargets()) != 0 || result.UsedMicroCNY() != 1800 {
		t.Fatal("initial delivery not complete/accounted", result)
	}
	if result.Assets[0].Description != "灰衣女子，左眉有疤" || result.Assets[1].Description != "东侧木门" || result.Prompt(d.VisualTarget{AssetID: result.Assets[0].ID}).Text != goodText || result.Prompt(d.VisualTarget{AssetID: result.Assets[1].ID}).Text != locationText {
		t.Fatal("fact correction or correct design was lost")
	}
	for _, c := range result.LatestRun().Calls {
		if len(c.Request) == 0 || c.Response == "" || !c.Usage.Known {
			t.Fatal("missing call/fee evidence")
		}
	}
}

func TestUserVisualEditsAreOrdinaryAtomicCommandsAndInvalidateAttachedLooks(t *testing.T) {
	ctx := context.Background()
	s, p := contentFixture(t, contentAssets())
	p = succeedVisual(t, s, p)
	before := d.Clone(p)
	base := d.VisualTarget{AssetID: "c"}
	look := d.VisualTarget{AssetID: "c", LookID: "l"}
	if _, err := s.SaveVisualPrompt(ctx, p.ID, p.Revision-1, base, "changed"); err == nil {
		t.Fatal("stale revision accepted")
	}
	if _, err := s.SaveVisualPrompt(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "foreign"}, "changed"); err == nil {
		t.Fatal("foreign target accepted")
	}
	if _, err := s.SaveVisualPrompt(ctx, p.ID, p.Revision, base, " \n"); err == nil {
		t.Fatal("empty prompt accepted")
	}
	after, _ := s.Project(ctx, p.ID)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("rejected edit changed project")
	}
	p, err := s.SaveVisualPrompt(ctx, p.ID, p.Revision, base, "用户完整正文\n保留换行")
	if err != nil {
		t.Fatal(err)
	}
	if !p.ValidPrompt(p.Prompt(base), "") || p.Prompt(base).Origin != "user" || !p.Prompt(look).Stale || p.Prompt(d.VisualTarget{AssetID: "loc"}).Stale {
		t.Fatal("manual base/attached invalidation incorrect")
	}
	p, err = s.SaveVisualPrompt(ctx, p.ID, p.Revision, look, "用户晚宴装完整正文")
	if err != nil || !p.ValidPrompt(p.Prompt(look), "") {
		t.Fatal("manual attached base reference incorrect", err)
	}
	p, err = s.SaveVisualPrompt(ctx, p.ID, p.Revision, base, "第二次基础修改")
	if err != nil || p.ValidPrompt(p.Prompt(look), "") {
		t.Fatal("manual base revision did not invalidate attached", err)
	}
	if !reflect.DeepEqual(p.Runs, before.Runs) || p.UsedMicroCNY() != before.UsedMicroCNY() || registry(s).Model.(*fakeModel).count != 0 {
		t.Fatal("ordinary edits executed model or changed fees")
	}
}

func TestPreparationKnownVisualFailureRetriesDesignOnlyUnknownNeverResends(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "known", true: "unknown"}[unknown], func(t *testing.T) {
			ctx := context.Background()
			s, p := contentFixture(t, contentAssets())
			r := p.LatestRun()
			r.Contract = d.CatalogExtractionContract
			r.Stage = "visualPrompt"
			r.Status = "failed"
			r.Targets = p.PendingVisualTargets()
			r.ErrorCode = "MODEL_PROVIDER_FAILED"
			r.Calls = []d.Call{{ID: "old-evidence", Stage: "visualPrompt", Status: "rejected", CostMicroCNY: 42}}
			if unknown {
				r.ErrorCode = "MODEL_RESULT_UNKNOWN"
				r.Calls[0].Status = "unknown"
				r.Calls[0].ReservedMicroCNY = 876
			}
			p, err := s.commit(ctx, p, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			before := d.Clone(p)
			next, err := s.Start(ctx, p.ID, p.Revision)
			if unknown {
				expectCode(t, err, "MODEL_RESULT_UNKNOWN")
				saved, _ := s.Project(ctx, p.ID)
				if !reflect.DeepEqual(saved, before) {
					t.Fatal("unknown retry mutated evidence")
				}
				return
			}
			if err != nil || next.LatestRun().Stage != "visualPrompt" || len(next.Runs) != 2 || !reflect.DeepEqual(next.Assets, before.Assets) || !reflect.DeepEqual(next.Runs[0], before.Runs[0]) {
				t.Fatal("retry restarted extraction or lost evidence", err)
			}
		})
	}
}

func TestPreparationVisualCancellationPreservesFactsAndAccountsLateResponse(t *testing.T) {
	ctx := context.Background()
	m := &fakeModel{}
	s, p := fixture(t, m)
	m.catalogVisual = false
	m.generate = func(req ports.Request) (ports.Response, error) {
		current, _ := s.Project(ctx, p.ID)
		if current.LatestRun().Stage == "correct" {
			return response(`{"assets":[]}`), nil
		}
		if current.LatestRun().Stage != "visualPrompt" {
			return response(`{"assets":[{"kind":"character","name":"祖父","description":"坐在工具间，姓名未知","sourceIds":["s1"]}]}`), nil
		}
		if _, err := s.Cancel(ctx, p.ID, current.Revision); err != nil {
			t.Fatal(err)
		}
		out := response("")
		out.Actions = []d.Action{action("save_visual_prompts", map[string]any{"prompts": []d.VisualPromptInput{{Target: d.VisualTarget{AssetID: current.Assets[0].ID}, Text: "迟到正文"}}})}
		return out, nil
	}
	if err := s.Execute(ctx, p.ID); err != nil && !errors.Is(err, fault.ErrCancelled) {
		t.Fatal(err)
	}
	got, _ := s.Project(ctx, p.ID)
	if got.LatestRun().Status != "cancelled" || got.LatestRun().Stage != "visualPrompt" || len(got.Assets) != 1 || len(got.VisualPrompts) != 0 || got.UsedMicroCNY() != 1080 || len(got.LatestRun().Calls) != 3 {
		t.Fatalf("late design boundary: status=%s stage=%s assets=%d prompts=%d fees=%d calls=%d", got.LatestRun().Status, got.LatestRun().Stage, len(got.Assets), len(got.VisualPrompts), got.UsedMicroCNY(), len(got.LatestRun().Calls))
	}
}

func TestMergedPreparationVisualStageLocksSourceWithoutSeparateVisualRun(t *testing.T) {
	for _, status := range []d.RunStatus{d.RunCompleted, d.RunFailed, d.RunCancelled} {
		for _, fromFile := range []bool{false, true} {
			t.Run(string(status)+map[bool]string{false: "/bytes", true: "/file"}[fromFile], func(t *testing.T) {
				ctx := context.Background()
				s, p := contentFixture(t, contentAssets())
				s.Importer = scriptimport.Importer{}
				r := p.LatestRun()
				r.Stage = "visualPrompt"
				r.Status = status
				r.Targets = p.PendingVisualTargets()
				p, err := s.commit(ctx, p, p.Revision)
				if err != nil {
					t.Fatal(err)
				}
				before := d.Clone(p)
				if fromFile {
					_, err = s.UploadFromFile(ctx, p.ID, p.Revision, "file_not_read_before_lock")
				} else {
					_, err = s.Upload(ctx, p.ID, p.Revision, "replacement.txt", []byte("另一份完整剧本。"))
				}
				expectCode(t, err, "SOURCE_LOCKED")
				saved, _ := s.Project(ctx, p.ID)
				if !reflect.DeepEqual(saved, before) || registry(s).Model.(*fakeModel).count != 0 {
					t.Fatal("source lock changed source/assets/history/fees or called a model")
				}
			})
		}
	}
}
