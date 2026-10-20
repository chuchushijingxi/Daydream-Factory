package project

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// These checks call the program's command/tool transaction ports directly.
// They never execute a model, ports.Agent or Runner; Generate would panic.
func singleFixture(t *testing.T) (*Service, d.Project) {
	s, p := contentFixture(t, contentAssets())
	registry(s).
		Model = preferenceOnlyModel{}
	return s, p
}

func singleStart(t *testing.T, s *Service, p d.Project, target d.VisualTarget) d.Project {
	t.Helper()
	p, err := s.StartSingleVisual(context.Background(), p.ID, p.Revision, target)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func singleSave(t *testing.T, s *Service, p d.Project, target d.VisualTarget, text string) d.Project {
	t.Helper()
	p, res := tool(t, s, p, action("save_visual_prompts", SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput(target), Text: text, DesignNotes: "program transaction fixture"}}}))
	if !strings.Contains(res.Text, `"ok":true`) {
		t.Fatal(res.Text)
	}
	return p
}

func singleFinish(t *testing.T, s *Service, p d.Project) d.Project {
	t.Helper()
	p, res := tool(t, s, p, action("finish_visual_prompts", struct{}{}))
	if !res.Finished {
		t.Fatal(res.Text)
	}
	return p
}

func TestSingleVisualScopeReceiptDependencyAndPendingBatchWithoutModels(t *testing.T) {
	s, p := singleFixture(t)
	base, look := d.VisualTarget{AssetID: "c"}, d.VisualTarget{AssetID: "c", LookID: "l"}
	p = singleStart(t, s, p, base)
	if !reflect.DeepEqual(p.LatestRun().Targets, []d.VisualTarget{base}) || *p.LatestRun().Target != base {
		t.Fatal("scope not frozen")
	}
	before := d.Clone(p)
	p, res := tool(t, s, p, action("save_visual_prompts", SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput(base), Text: "valid field"}, {Target: VisualTargetInput{AssetID: "prop"}, Text: "outside scope"}}}))
	if !strings.Contains(res.Text, `"ok":false`) || !reflect.DeepEqual(p.VisualPrompts, before.VisualPrompts) {
		t.Fatal("partial adoption of rejected scope")
	}
	p = singleSave(t, s, p, base, "完整角色基础")
	req, _, err := s.contentRequest(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Input, "完整角色基础") || !strings.Contains(req.Input, "完整 P3 说明") || strings.Contains(req.Input, `"id":"prop"`) || !strings.Contains(req.Input, p.ScriptSource.Spans[0].Text) {
		t.Fatal("necessary materials lost or foreign asset supplied", req.Input)
	}
	for _, a := range p.LatestRun().Calls[len(p.LatestRun().Calls)-1].Actions {
		raw, _ := json.Marshal(req)
		if !registry(s).Model.ContainsToolReceipt(raw, a, d.Call{}) {
			t.Fatal("real receipt missing from next input")
		}
	}
	p = singleFinish(t, s, p)
	if len(p.PendingVisualTargets()) != 3 || !p.ValidPrompt(p.Prompt(base), "") {
		t.Fatal("single success pretended full catalog completion")
	}
	baseRef := p.Prompt(base).ActionRef
	p = singleStart(t, s, p, look)
	if !reflect.DeepEqual(p.LatestRun().Targets, []d.VisualTarget{look}) {
		t.Fatal("effective parent regenerated unnecessarily")
	}
	req, _, err = s.contentRequest(context.Background(), p)
	if err != nil || !strings.Contains(req.Input, "完整角色基础") || !strings.Contains(req.Input, "program transaction fixture") {
		t.Fatal("cross-run parent and design notes missing", err)
	}
	p = singleSave(t, s, p, look, "同人物晚宴正文")
	if *p.Prompt(look).BasePrompt != baseRef {
		t.Fatal("wrong effective parent reference")
	}
	p = singleFinish(t, s, p)
	if !p.ValidPrompt(p.Prompt(look), "") {
		t.Fatal("cross-run parent treated invalid")
	}
	p = singleStart(t, s, p, base)
	p = singleSave(t, s, p, base, "修订基础")
	if !p.Prompt(look).Stale {
		t.Fatal("out-of-scope child not invalidated atomically")
	}
	last := p.LatestRun().Calls[len(p.LatestRun().Calls)-1].Actions[0]
	if !strings.Contains(last.Result, `"invalidatedTargets":[{"assetId":"c","lookId":"l"}]`) {
		t.Fatal("invalidation missing from actual receipt", last.Result)
	}
	p = singleFinish(t, s, p) // A base-only task does not require unauthorized child repair.
	p = startVisual(t, s, p)
	if p.LatestRun().Target != nil || !reflect.DeepEqual(p.LatestRun().Targets, []d.VisualTarget{look, {AssetID: "loc"}, {AssetID: "prop"}}) {
		t.Fatal("batch not limited to pending items", p.LatestRun().Targets)
	}
	for _, target := range p.LatestRun().Targets {
		p = singleSave(t, s, p, target, "完整正文 "+target.AssetID+target.LookID)
	}
	p = singleFinish(t, s, p)
	if len(p.PendingVisualTargets()) != 0 || p.Prompt(base).Text != "修订基础" {
		t.Fatal("batch overwrote correct base or missed pending range")
	}
	_, err = s.StartVisual(context.Background(), p.ID, p.Revision)
	expectCode(t, err, "VISUAL_PROMPTS_UP_TO_DATE")
	if p.UsedMicroCNY() != 999 {
		t.Fatal("command checks changed historical unknown reserve")
	}
}

func TestSingleVisualMissingBaseAndSameResponseFinishWithoutModels(t *testing.T) {
	s, p := singleFixture(t)
	look := d.VisualTarget{AssetID: "c", LookID: "l"}
	p = singleStart(t, s, p, look)
	if !reflect.DeepEqual(p.LatestRun().Targets, []d.VisualTarget{{AssetID: "c"}, look}) {
		t.Fatal("missing parent not included")
	}
	p, res := tool(t, s, p, action("save_visual_prompts", SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput(look), Text: "child first"}}}))
	if !strings.Contains(res.Text, `"ok":false`) || len(p.VisualPrompts) != 0 {
		t.Fatal("child adopted without base")
	}
	save := action("save_visual_prompts", SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput(look), Text: "child"}, {Target: VisualTargetInput{AssetID: "c"}, Text: "base"}}})
	finish := action("finish_visual_prompts", struct{}{})
	p = recordProjectContent(t, s, p, save, finish)
	if _, err := s.executeTool(context.Background(), p.ID, save.ID); err != nil {
		t.Fatal(err)
	}
	res, err := s.executeTool(context.Background(), p.ID, finish.ID)
	if err != nil || res.Finished || !strings.Contains(res.Text, "next model request") {
		t.Fatal("same response skipped real receipts", res, err)
	}
	p, _ = s.Store.Snapshot(context.Background(), p.ID)
	p = singleFinish(t, s, p)
	if !p.ValidPrompt(p.Prompt(look), "") || len(p.PendingVisualTargets()) != 2 {
		t.Fatal("parent-child completion wrong")
	}
}

func TestSingleVisualP3EditingFailureCancelAndScopedAuthorizationWithoutModels(t *testing.T) {
	ctx := context.Background()
	for _, status := range []string{"missing", "failed", "cancelled"} {
		s, p := singleFixture(t)
		if status != "missing" {
			p.Runs = append(p.Runs, d.Run{ID: "old", Contract: d.VisualContract, SourceVersion: 1, Status: d.RunStatus(status), Target: &d.VisualTarget{AssetID: "c"}, Targets: []d.VisualTarget{{AssetID: "c"}}, Error: "specific visual error"})
			p, _ = s.commit(ctx, p, p.Revision)
		}
		oldCount := len(p.Runs)
		var err error
		p, err = s.SaveLook(ctx, p.ID, p.Revision, "c", "l", d.LookInput{Name: "Changed", Description: "full user detail"})
		if err != nil || !p.CatalogEditedByUser || len(p.Runs) != oldCount || p.FailedTask() != nil || p.HasCancelledTask() {
			t.Fatal("visual result blocked ordinary edit", status, err)
		}
		p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
		if len(p.Runs) != oldCount+1 || (status != "missing" && p.Runs[1].Status != d.RunStatus(status)) {
			t.Fatal("old terminal task revived")
		}
	}
	s, p := singleFixture(t)
	s.Authorize = nil
	before := d.Clone(p)
	_, err := s.StartSingleVisual(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "c"})
	expectCode(t, err, "EXECUTION_NOT_AUTHORIZED")
	for _, target := range []d.VisualTarget{{AssetID: "foreign"}, {AssetID: "loc", LookID: "l"}, {AssetID: "c", LookID: "foreign"}} {
		if _, err = s.StartSingleVisual(ctx, p.ID, p.Revision, target); err == nil {
			t.Fatal("invalid ownership accepted")
		}
	}
	_, err = s.StartSingleVisual(ctx, p.ID, p.Revision-1, d.VisualTarget{AssetID: "c"})
	if !errors.Is(err, fault.ErrConflict) {
		t.Fatal(err)
	}
	after, _ := s.Store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejected start changed history/costs")
	}
	permit := ExecutionPermit{ProjectID: p.ID, SourceSHA256: p.ScriptSource.SHA256, SourceVersion: 1, Contract: d.VisualContract, InputRevision: p.Revision, BudgetMicroCNY: 5000, Targets: []d.VisualTarget{{AssetID: "c"}}}
	s.Authorize = AuthorizePermits([]ExecutionPermit{permit})
	_, err = s.StartVisual(ctx, p.ID, p.Revision)
	expectCode(t, err, "EXECUTION_NOT_AUTHORIZED")
	_, err = s.StartSingleVisual(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "c", LookID: "l"})
	expectCode(t, err, "EXECUTION_NOT_AUTHORIZED")
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	if p.LatestRun().Model != "deepseek-flash" || p.LatestRun().BudgetMicroCNY != 5000 || len(p.LatestRun().Calls) != 0 {
		t.Fatal("model/budget not frozen")
	}
	_, err = s.SaveAsset(ctx, p.ID, p.Revision, "prop", d.AssetInput{Kind: "prop", Name: "blocked"})
	expectCode(t, err, "RUN_ACTIVE")
	p, err = s.Cancel(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if p.LatestRun().Status != "cancelled" {
		t.Fatal("cancel not persisted")
	}
	_, err = s.Authorize(ctx, p, *p.LatestRun())
	if err != nil {
		t.Fatal("exact original scoped permit lost", err)
	}
	_, err = s.StartSingleVisual(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "c"})
	expectCode(t, err, "EXECUTION_NOT_AUTHORIZED") // startup revision changed; no reuse
}

func TestSingleVisualConcurrentStartupAndIndependentAssemblyWithoutModels(t *testing.T) {
	s, p := singleFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, target := range []d.VisualTarget{{AssetID: "c"}, {AssetID: "loc"}} {
		wg.Add(1)
		go func(target d.VisualTarget) {
			defer wg.Done()
			_, err := s.StartSingleVisual(context.Background(), p.ID, p.Revision, target)
			errs <- err
		}(target)
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		if err == nil {
			ok++
		}
	}
	read, _ := s.Store.Snapshot(context.Background(), p.ID)
	if ok != 1 || len(read.Runs) != 2 || len(read.LatestRun().Calls) != 0 || read.UsedMicroCNY() != 999 {
		t.Fatal("concurrent start duplicated task/costs")
	}
	s, p = singleFixture(t)
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "prop"})
	p = singleSave(t, s, p, d.VisualTarget{AssetID: "prop"}, "完整物件正文")
	p = singleFinish(t, s, p)
	p.Style = &d.ProjectStyle{ID: "fixed", PromptVersion: "v1", Prompts: d.StylePrompts{ZH: "完整固定风格"}}
	p, _ = s.commit(context.Background(), p, p.Revision)
	text, err := s.ComposeVisualPrompt(context.Background(), p.ID, d.VisualTarget{AssetID: "prop"})
	if err != nil || text != "完整物件正文\n\n风格要求:\n完整固定风格" || p.Prompt(d.VisualTarget{AssetID: "prop"}).Text != "完整物件正文" {
		t.Fatal("individual effective body cannot compose fixed style once", text, err)
	}
}
