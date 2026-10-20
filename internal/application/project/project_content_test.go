package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
)

func contentFixture(t *testing.T, assets []d.Asset) (*Service, d.Project) {
	t.Helper()
	m := newMemory()
	p := d.Project{ID: identity.New("p4_test"), Revision: 1, ScriptSource: d.ScriptSource{Version: 1, SHA256: "fictional", Spans: []d.SourceSpan{{ID: "s", Text: "林岚在修理间用铜钥匙开门。左眉有疤。"}}}, Original: []byte("完整虚构原文"), Assets: assets, Runs: []d.Run{{ID: "p3", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "completed", Calls: []d.Call{{ID: "historic", ReservedMicroCNY: 999}}}}}
	if err := m.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return &Service{Store: m, Agent: unitAgent{}, Authorize: freeAuthorization, Models: &modelregistry.Registry{Model: &fakeModel{}}}, p
}

func contentAssets() []d.Asset {
	return []d.Asset{{ID: "c", Kind: "character", Name: "林岚", Description: "完整 P3 说明", SourceIDs: []string{"s"}, Looks: []d.Look{{ID: "l", Name: "晚宴装", Description: "黑礼服"}}}, {ID: "loc", Kind: "location", Name: "修理间", SourceIDs: []string{"s"}}, {ID: "prop", Kind: "prop", Name: "铜钥匙", SourceIDs: []string{"s"}}}
}

func recordProjectContent(t *testing.T, s *Service, p d.Project, actions ...d.Action) d.Project {
	t.Helper()
	r := p.LatestRun()
	r.End = len(p.ScriptSource.Spans)
	for i := range actions {
		actions[i].Status = "pending"
	}
	req := ports.Request{}
	if history, err := taskDialogue(p); err == nil {
		req.Dialogue = history
	}
	raw, _ := json.Marshal(req)
	r.Calls = append(r.Calls, d.Call{ID: identity.New("call"), Stage: r.Stage, End: r.End, Status: "received", FinishReason: "tool_calls", Request: raw, Actions: actions, Usage: billing.Usage{Known: true}})
	p, err := s.commit(context.Background(), p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func tool(t *testing.T, s *Service, p d.Project, a d.Action) (d.Project, ports.ToolResult) {
	t.Helper()
	p = recordProjectContent(t, s, p, a)
	res, err := s.executeTool(context.Background(), p.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Store.Snapshot(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p, res
}

func action(name string, value any) d.Action {
	b, _ := json.Marshal(value)
	return d.Action{ID: identity.New("action"), Name: name, Arguments: string(b), Status: "pending"}
}

func saveAll() SaveVisualPrompts {
	out := SaveVisualPrompts{}
	for _, v := range []struct{ a, l, text string }{{"c", "", "林岚，左眉疤，黑发低马尾"}, {"c", "l", "林岚，左眉疤，黑礼服"}, {"loc", "", "修理间的可复用工作环境"}, {"prop", "", "铜钥匙物件本体"}} {
		out.Prompts = append(out.Prompts, VisualPromptInput{Target: VisualTargetInput{AssetID: v.a, LookID: v.l}, Text: v.text, DesignNotes: "共同面貌是设计"})
	}
	return out
}

func startVisual(t *testing.T, s *Service, p d.Project) d.Project {
	t.Helper()
	p, err := s.StartVisual(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func succeedVisual(t *testing.T, s *Service, p d.Project) d.Project {
	t.Helper()
	p = startVisual(t, s, p)
	p, _ = tool(t, s, p, action("save_visual_prompts", saveAll()))
	p, result := tool(t, s, p, action("finish_visual_prompts", struct{}{}))
	if !result.Finished {
		t.Fatalf("not complete %s", result.Text)
	}
	return p
}

func expectCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), code) {
		t.Fatalf("want %s got %v", code, err)
	}
}

func TestProjectContentOutlinePersistsRevisesReadsReceiptAndNeverChangesCatalog(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	original := d.Clone(p)
	p, err := s.StartOutline(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	expectCode(t, func() error { _, e := s.StartOutline(context.Background(), p.ID, p.Revision); return e }(), "RUN_ACTIVE")
	bad := d.OutlineInput{Protagonist: "林岚", Genre: "剧情", Summary: "错误背景", Background: "修理间", Logline: "开门"}
	p, _ = tool(t, s, p, action("save_outline", SaveOutline{Outline: bad}))
	bad.Summary = "林岚用钥匙开门，保留左眉疤说明"
	p, _ = tool(t, s, p, action("save_outline", SaveOutline{Outline: bad}))
	p, res := tool(t, s, p, action("finish_outline", struct{}{}))
	if !res.Finished || p.Outline.Summary != bad.Summary || !reflect.DeepEqual(p.Assets, original.Assets) {
		t.Fatal("outline saved incorrectly")
	}
	if p.Outline.RunID != p.LatestRun().ID || p.Outline.CallID == "" {
		t.Fatal("provenance absent")
	}
	_, err = s.StartOutline(context.Background(), p.ID, p.Revision)
	expectCode(t, err, "OUTLINE_ALREADY_STARTED")
	_, err = s.Upload(context.Background(), p.ID, p.Revision, "x.txt", []byte("换源"))
	expectCode(t, err, "SOURCE_LOCKED")
}

func TestProjectContentVisualAtomicBaseInvalidationRepairAndCorrectContentPreservation(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	p = startVisual(t, s, p)
	p, _ = tool(t, s, p, action("save_visual_prompts", saveAll()))
	correctLocation := *p.Prompt(d.VisualTarget{AssetID: "loc"})
	bad := SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput{AssetID: "c"}, Text: "左眉浅疤，保持正确发型", DesignNotes: "修订共同脸型设计"}}}
	p, res := tool(t, s, p, action("save_visual_prompts", bad))
	if !p.Prompt(d.VisualTarget{AssetID: "c", LookID: "l"}).Stale || !strings.Contains(res.Text, "invalidatedTargets") || !reflect.DeepEqual(*p.Prompt(correctLocation.Target), correctLocation) {
		t.Fatal("atomic dependency invalidation failed")
	}
	p, res = tool(t, s, p, action("finish_visual_prompts", struct{}{}))
	if res.Finished || !strings.Contains(res.Text, "nonempty") {
		t.Fatal("stale look accepted")
	}
	repair := SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput{AssetID: "c", LookID: "l"}, Text: "林岚，左眉浅疤，黑礼服，保持正确发型", DesignNotes: "采用修订基础脸型"}}}
	p, _ = tool(t, s, p, action("save_visual_prompts", repair))
	p, res = tool(t, s, p, action("finish_visual_prompts", struct{}{}))
	if !res.Finished || !p.VisualComplete(p.LatestRun()) || p.Prompt(d.VisualTarget(repair.Prompts[0].Target)).BasePrompt.CallID != p.Prompt(d.VisualTarget(bad.Prompts[0].Target)).CallID {
		t.Fatal("repaired base ref absent")
	}
}

func TestProjectContentSameResponseSaveAndFinishMustDeliverActualReceipt(t *testing.T) {
	for _, contract := range []string{d.OutlineContract, d.VisualContract} {
		t.Run(contract, func(t *testing.T) {
			s, p := contentFixture(t, contentAssets())
			var err error
			var save d.Action
			finish := action("finish_visual_prompts", struct{}{})
			if contract == d.OutlineContract {
				p, err = s.StartOutline(context.Background(), p.ID, p.Revision)
				save = action("save_outline", SaveOutline{})
				finish.Name = "finish_outline"
			} else {
				p, err = s.StartVisual(context.Background(), p.ID, p.Revision)
				save = action("save_visual_prompts", saveAll())
			}
			if err != nil {
				t.Fatal(err)
			}
			p = recordProjectContent(t, s, p, save, finish)
			if _, err = s.executeTool(context.Background(), p.ID, save.ID); err != nil {
				t.Fatal(err)
			}
			res, err := s.executeTool(context.Background(), p.ID, finish.ID)
			if err != nil {
				t.Fatal(err)
			}
			if res.Finished || !strings.Contains(res.Text, "next model request") {
				t.Fatal("finish bypassed feedback")
			}
			p, _ = s.Store.Snapshot(context.Background(), p.ID)
			p, res = tool(t, s, p, action(finish.Name, struct{}{}))
			if !res.Finished {
				t.Fatal("finish after receipts refused", res.Text)
			}
		})
	}
}

func TestProjectContentSuccessfulCRUDNoModelCascadeAndLocalInvalidation(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	p = succeedVisual(t, s, p)
	completedID := p.LatestRun().ID
	count := len(p.LatestRun().Calls)
	fees := p.UsedMicroCNY()
	ctx := context.Background()
	p, err := s.SaveLook(ctx, p.ID, p.Revision, "c", "l", d.LookInput{Name: "冬装", Description: "多行\n完整要求"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Prompt(d.VisualTarget{AssetID: "c"}).Stale || !p.Prompt(d.VisualTarget{AssetID: "c", LookID: "l"}).Stale {
		t.Fatal("look invalidated parent")
	}
	p, err = s.SaveAsset(ctx, p.ID, p.Revision, "c", d.AssetInput{Kind: "character", Name: " 林岚 ", Aliases: []string{}, Description: "完整用户资料\n", Character: &d.Character{Appearance: "左眉疤"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Asset("c").Looks) != 1 || !p.Prompt(d.VisualTarget{AssetID: "c"}).Stale || p.Prompt(d.VisualTarget{AssetID: "loc"}).Stale {
		t.Fatal("character isolation failed")
	}
	for _, kind := range []string{"character", "location", "prop"} {
		v := d.AssetInput{Kind: kind, Name: "同名", Aliases: []string{}}
		if kind == "character" {
			v.Character = &d.Character{}
		}
		p, err = s.SaveAsset(ctx, p.ID, p.Revision, "", v)
		if err != nil {
			t.Fatal(err)
		}
		id := p.Assets[len(p.Assets)-1].ID
		if p.Prompt(d.VisualTarget{AssetID: id}) != nil || len(p.Asset(id).Looks) != 0 || p.Asset(id).Origin != "user" {
			t.Fatal("new asset got fake prompt")
		}
	}
	p, err = s.SaveLook(ctx, p.ID, p.Revision, "c", "", d.LookInput{Name: "新增"})
	if err != nil {
		t.Fatal(err)
	}
	l := p.Asset("c").Looks[1]
	if p.Prompt(d.VisualTarget{AssetID: "c", LookID: l.ID}) != nil {
		t.Fatal("new look got fake prompt")
	}
	_, err = s.SaveLook(ctx, p.ID, p.Revision, "loc", "l", d.LookInput{Name: "x"})
	expectCode(t, err, "INVALID_INPUT")
	_, err = s.SaveLook(ctx, p.ID, p.Revision, "c", "foreign", d.LookInput{Name: "x"})
	expectCode(t, err, "NOT_FOUND")
	p, err = s.DeleteLook(ctx, p.ID, p.Revision, "c", "l")
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.DeleteLook(ctx, p.ID, p.Revision, "c", l.ID)
	if err != nil || len(p.Asset("c").Looks) != 0 || p.Prompt(d.VisualTarget{AssetID: "c"}) == nil {
		t.Fatal("last look removed base")
	}
	p, err = s.DeleteAsset(ctx, p.ID, p.Revision, "c")
	if err != nil {
		t.Fatal(err)
	}
	if p.Prompt(d.VisualTarget{AssetID: "c"}) != nil || p.Asset("loc") == nil {
		t.Fatal("delete cascade failed")
	}
	prepared, startErr := s.Start(ctx, p.ID, p.Revision)
	if startErr != nil || prepared.LatestRun().Stage != "visualPrompt" || !reflect.DeepEqual(prepared.Assets, p.Assets) {
		t.Fatal("asset preparation overwrote user catalog", startErr)
	}
	p = prepared
	p, err = s.Cancel(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Upload(ctx, p.ID, p.Revision, "x.txt", nil)
	expectCode(t, err, "CATALOG_USER_EDITED")
	completedCalls := 0
	for _, run := range p.Runs {
		if run.ID == completedID {
			completedCalls = len(run.Calls)
		}
	}
	if completedCalls != count || p.UsedMicroCNY() != fees || registry(s).Model.(*fakeModel).count != 0 {
		t.Fatal("ordinary CRUD called model")
	}
}

func TestProjectContentAllPrematureWritesRefused(t *testing.T) {
	for _, state := range []string{"missing", "running", "failed", "cancelled", "partial"} {
		t.Run(state, func(t *testing.T) {
			s, p := contentFixture(t, contentAssets())
			if state == "missing" {
				p.Runs = nil
			} else if state == "partial" {
				p.Runs = append(p.Runs, d.Run{ID: "visual", Contract: d.VisualContract, SourceVersion: 1, Status: "running", Targets: []d.VisualTarget{{AssetID: "c"}}})
			} else {
				p.Runs[0].Status, p.Runs[0].Error = d.RunStatus(state), "specific failure"
			}
			p, _ = s.commit(context.Background(), p, p.Revision)
			before := d.Clone(p)
			v := d.AssetInput{Kind: "prop", Name: "x", Aliases: []string{}}
			cmds := []func() error{func() error { _, e := s.SaveAsset(context.Background(), p.ID, p.Revision, "", v); return e }, func() error { _, e := s.SaveAsset(context.Background(), p.ID, p.Revision, "prop", v); return e }, func() error { _, e := s.DeleteAsset(context.Background(), p.ID, p.Revision, "prop"); return e }, func() error {
				_, e := s.SaveLook(context.Background(), p.ID, p.Revision, "c", "", d.LookInput{Name: "x"})
				return e
			}, func() error { _, e := s.DeleteLook(context.Background(), p.ID, p.Revision, "c", "l"); return e }}
			for _, f := range cmds {
				if f() == nil {
					t.Fatal("write accepted")
				}
			}
			after, _ := s.Store.Snapshot(context.Background(), p.ID)
			if !reflect.DeepEqual(after, before) {
				t.Fatal("refused write mutated data")
			}
		})
	}
}

func TestProjectContentEmptyVisualConcurrentZeroCallsFeesPermitAndModel(t *testing.T) {
	s, p := contentFixture(t, []d.Asset{})
	registry(s).
		Model = nil
	s.Agent = nil
	s.Authorize = nil
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.StartVisual(ctx, p.ID, p.Revision); results <- err }()
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatal("concurrent first admitted twice")
	}
	after, _ := s.Store.Snapshot(ctx, p.ID)
	r := after.LatestRun()
	if len(after.Runs) != 2 || r.Status != "failed" || r.Error != d.EmptyVisualError || r.Model != "" || r.BudgetMicroCNY != 0 || len(r.Calls) != 0 || after.UsedMicroCNY() != 999 || after.LatestRunByContract(d.CatalogExtractionContract).Status != "completed" {
		t.Fatal("empty branch lost evidence", after)
	}
	_, err := s.StartVisual(ctx, p.ID, after.Revision)
	expectCode(t, err, "VISUAL_PROMPTS_ALREADY_STARTED")
	_, err = s.StartOutline(ctx, p.ID, after.Revision)
	expectCode(t, err, "PROJECT_FAILED")
}

func TestProjectContentTargetToolIsolationAndAtomicRejection(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	p = startVisual(t, s, p)
	before := d.Clone(p)
	bad := saveAll()
	bad.Prompts[1].Target.AssetID = "foreign"
	p, res := tool(t, s, p, action("save_visual_prompts", bad))
	if strings.Contains(res.Text, `"ok":true`) || len(p.VisualPrompts) != 0 || !reflect.DeepEqual(p.Assets, before.Assets) {
		t.Fatal("bad target partially saved")
	}
	bad = saveAll()
	bad.Prompts = append(bad.Prompts, bad.Prompts[0])
	p, res = tool(t, s, p, action("save_visual_prompts", bad))
	if strings.Contains(res.Text, `"ok":true`) {
		t.Fatal("duplicate accepted")
	}
	p, res = tool(t, s, p, action("save_catalog", CatalogChangeSet{Assets: catalogInputs([]d.Asset{})}))
	if strings.Contains(res.Text, `"ok":true`) || len(p.Assets) != 3 {
		t.Fatal("visual task wrote catalog")
	}
	p, res = tool(t, s, p, action("save_visual_prompts", SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput{AssetID: "c", LookID: "l"}, Text: "附加"}}}))
	if strings.Contains(res.Text, `"ok":true`) {
		t.Fatal("look without base accepted")
	}
}

func TestProjectContentFullMaterialsNoSettingsOutlineOrStyleAndCapacity(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	p.ProjectSettings = &d.ProjectSettings{AspectRatio: "9:16", ImageModel: "secret-image", VideoModel: "secret-video"}
	p.Style = &d.ProjectStyle{Prompts: d.StylePrompts{ZH: "secret-style"}}
	p.Outline = &d.Outline{OutlineInput: d.OutlineInput{Summary: "secret-outline"}}
	p, _ = s.commit(context.Background(), p, p.Revision)
	p = startVisual(t, s, p)
	req, end, err := s.request(context.Background(), p)
	if err != nil || end != len(p.ScriptSource.Spans) {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-style", "secret-outline", "secret-image", "secret-video", "9:16"} {
		if strings.Contains(req.Input, secret) {
			t.Fatal("configuration leaked", secret)
		}
	}
	if !strings.Contains(req.Input, "完整 P3 说明") || !strings.Contains(req.Input, p.ScriptSource.Spans[0].Text) {
		t.Fatal("incomplete material")
	}
	m := registry(s).Model.(*fakeModel)
	m.window = 100
	m.output = 20
	if _, _, err = s.request(context.Background(), p); !errors.Is(err, fault.ErrCapacity) {
		t.Fatal("capacity truncated", err)
	}
}

func TestProjectContentFixedStyleSnapshotAndCentralAssembly(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	s.Styles = []d.StyleDefinition{{ProjectStyle: d.ProjectStyle{ID: "style", PromptVersion: "v1", Prompts: d.StylePrompts{ZH: "完整画风\n原文", EN: "full style"}}}}
	p, err := s.SaveStyle(context.Background(), p.ID, p.Revision, "style", "v1")
	if err != nil {
		t.Fatal(err)
	}
	rev := p.Revision
	p, err = s.SaveStyle(context.Background(), p.ID, p.Revision, "style", "v1")
	if err != nil || p.Revision != rev {
		t.Fatal("style idempotency")
	}
	_, err = s.SaveStyle(context.Background(), p.ID, p.Revision, "other", "v1")
	expectCode(t, err, "STYLE_LOCKED")
	s.Styles[0].Prompts.ZH = "新版"
	p = succeedVisual(t, s, p)
	text, err := s.ComposeVisualPrompt(context.Background(), p.ID, d.VisualTarget{AssetID: "c"})
	if err != nil || text != p.Prompt(d.VisualTarget{AssetID: "c"}).Text+"\n\n风格要求:\n完整画风\n原文" {
		t.Fatal("style altered or double appended", err, text)
	}
}

type brokenCommit struct {
	ports.ProjectStore
	err error
}

func (b brokenCommit) Commit(context.Context, d.Project, uint64) error { return b.err }

func TestProjectContentCommitRollbackAndStaleInput(t *testing.T) {
	s, p := contentFixture(t, []d.Asset{})
	before := d.Clone(p)
	s.Store = brokenCommit{ProjectStore: s.Store, err: fmt.Errorf("test PG commit failed")}
	if _, err := s.StartVisual(context.Background(), p.ID, p.Revision); err == nil {
		t.Fatal("save failed reported success")
	}
	after, _ := s.Store.Snapshot(context.Background(), p.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("partial save")
	}
	if _, err := s.StartVisual(context.Background(), p.ID, p.Revision+1); !errors.Is(err, fault.ErrConflict) {
		t.Fatal("stale revision bypassed empty branch")
	}
}

func TestProjectContentPermitAndModelSelectionFrozenActualConsumption(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	s.Authorize = nil
	if _, err := s.StartVisual(context.Background(), p.ID, p.Revision); err == nil {
		t.Fatal("key inferred authorization")
	}
	s.Authorize = AuthorizePermits([]ExecutionPermit{{ProjectID: p.ID, SourceSHA256: p.ScriptSource.SHA256, SourceVersion: 1, Contract: d.VisualContract, InputRevision: p.Revision, BudgetMicroCNY: 50000, Targets: p.PendingVisualTargets()}})
	one, two := &fakeModel{}, &fakeModel{}
	registry(s).
		Models = map[string]ports.Model{"one": one, "two": two}
	registry(s).
		OptionsValue = ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "one", Name: "one"}, {ID: "two", Name: "two"}}}
	registry(s).
		DefaultModel = "one"
	p.ProjectSettings = &d.ProjectSettings{LLMModel: "two"}
	p, _ = s.commit(context.Background(), p, p.Revision)
	s.Authorize = AuthorizePermits([]ExecutionPermit{{ProjectID: p.ID, SourceSHA256: p.ScriptSource.SHA256, SourceVersion: 1, Contract: d.VisualContract, InputRevision: p.Revision, BudgetMicroCNY: 50000, Targets: p.PendingVisualTargets()}})
	p = startVisual(t, s, p)
	registry(s).
		DefaultModel = "one"
	if p.LatestRun().Model != "two" || modelForTest(s, p.LatestRun()) != two || p.LatestRun().BudgetMicroCNY != 50000 {
		t.Fatal("model not frozen")
	}
	s.Authorize = nil
	if err := s.Execute(context.Background(), p.ID); err == nil {
		t.Fatal("permission removal ignored")
	}
	q, _ := s.Store.Snapshot(context.Background(), p.ID)
	if two.count != 0 || len(q.LatestRun().Calls) != 0 {
		t.Fatal("sent without permit")
	}
}

func TestProjectContentSelectedModelGeneratesAndNewStartupCannotReusePermit(t *testing.T) {
	s, p := contentFixture(t, []d.Asset{{ID: "prop", Kind: "prop", Name: "钥匙"}})
	ctx := context.Background()
	one, two := &fakeModel{}, &fakeModel{}
	registry(s).
		Models = map[string]ports.Model{"one": one, "two": two}
	registry(s).
		OptionsValue = ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "one", Name: "one"}, {ID: "two", Name: "two"}}}
	registry(s).
		DefaultModel = "one"
	p, err := s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{LLMModel: "two"})
	if err != nil {
		t.Fatal(err)
	}
	s.Authorize = AuthorizePermits([]ExecutionPermit{{ProjectID: p.ID, SourceSHA256: p.ScriptSource.SHA256, SourceVersion: p.ScriptSource.Version, Contract: d.VisualContract, InputRevision: p.Revision, BudgetMicroCNY: 10000000, Targets: p.PendingVisualTargets()}})
	p = startVisual(t, s, p)
	registry(s).
		DefaultModel = "one"
	two.generate = func(req ports.Request) (ports.Response, error) {
		if two.count == 1 {
			return ports.Response{FinishReason: "tool_calls", Usage: billing.Usage{Known: true}, Raw: json.RawMessage(`{}`), Actions: []d.Action{action("save_visual_prompts", SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput{AssetID: "prop"}, Text: "钥匙本体", DesignNotes: ""}}})}}, nil
		}
		return ports.Response{FinishReason: "tool_calls", Usage: billing.Usage{Known: true}, Raw: json.RawMessage(`{}`), Actions: []d.Action{action("finish_visual_prompts", struct{}{})}}, nil
	}
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Store.Snapshot(ctx, p.ID)
	if one.count != 0 || two.count != 2 || p.LatestRun().Status != "completed" {
		t.Fatal("selected frozen model not actually consumed")
	}
	_, err = s.StartOutline(ctx, p.ID, p.Revision)
	expectCode(t, err, "EXECUTION_NOT_AUTHORIZED")
}

func TestProjectContentCurrentSourceLatestP3CannotFallbackToOldSuccess(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	p.Runs = append(p.Runs, d.Run{ID: "new-p3", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "failed", Error: "真实提取故障"}, d.Run{ID: "other", Contract: d.OutlineContract, SourceVersion: 1, Status: "completed"})
	p, _ = s.commit(context.Background(), p, p.Revision)
	if p.LatestRunByContract(d.CatalogExtractionContract).ID != "new-p3" || p.FailedTask().Error != "真实提取故障" {
		t.Fatal("old success masked failure")
	}
	_, err := s.StartVisual(context.Background(), p.ID, p.Revision)
	expectCode(t, err, "PROJECT_FAILED")
	p.ScriptSource.Version = 2
	p.Runs = p.Runs[:1]
	p, _ = s.commit(context.Background(), p, p.Revision)
	_, err = s.StartVisual(context.Background(), p.ID, p.Revision)
	expectCode(t, err, "ASSETS_NOT_READY")
}

func TestProjectContentCRUDConflictsCrossProjectAndCommitFailurePreserveEverything(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	p = succeedVisual(t, s, p)
	before := d.Clone(p)
	ctx := context.Background()
	v := d.AssetInput{Kind: "prop", Name: "x", Aliases: []string{}}
	_, err := s.SaveAsset(ctx, p.ID, p.Revision-1, "prop", v)
	expectCode(t, err, "CONFLICT")
	_, err = s.SaveAsset(ctx, p.ID, p.Revision, "foreign-project-asset", v)
	expectCode(t, err, "NOT_FOUND")
	_, err = s.SaveLook(ctx, p.ID, p.Revision, "foreign-project-character", "l", d.LookInput{Name: "x"})
	expectCode(t, err, "NOT_FOUND")
	s.Store = brokenCommit{ProjectStore: s.Store, err: fmt.Errorf("controlled save fault")}
	if _, err = s.SaveAsset(ctx, p.ID, p.Revision, "prop", v); err == nil {
		t.Fatal("failed commit reported save")
	}
	after, _ := s.Store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("CRUD failed commit partially invalidated")
	}
}

func TestProjectContentNonemptyConcurrentFirstAndTwoOrdinaryWritersOnlyOneCommit(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.StartVisual(ctx, p.ID, p.Revision); errs <- err }()
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatal("nonempty concurrent first created two tasks")
	}
	p, _ = s.Store.Snapshot(ctx, p.ID)
	p, _ = tool(t, s, p, action("save_visual_prompts", saveAll()))
	p, result := tool(t, s, p, action("finish_visual_prompts", struct{}{}))
	if !result.Finished {
		t.Fatal(result.Text)
	}
	errs = make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.SaveLook(ctx, p.ID, p.Revision, "c", "", d.LookInput{Name: "同名"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	ok = 0
	for err := range errs {
		if err == nil {
			ok++
		}
	}
	after, _ := s.Store.Snapshot(ctx, p.ID)
	if ok != 1 || len(after.Asset("c").Looks) != 2 {
		t.Fatal("two ordinary writers committed")
	}
}

func TestProjectContentSettingsUnknownUnavailableAndNoInvalidation(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	p = succeedVisual(t, s, p)
	before := d.Clone(p)
	ctx := context.Background()
	_, err := s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{AspectRatio: "2:3"})
	expectCode(t, err, "INVALID_INPUT")
	_, err = s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{LLMModel: "unknown"})
	expectCode(t, err, "INVALID_INPUT")
	registry(s).
		OptionsValue = ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "deepseek-flash", Name: "DeepSeek"}}}
	model := registry(s).Model
	registry(s).
		Model = nil
	_, err = s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{LLMModel: "deepseek-flash"})
	expectCode(t, err, "MODEL_UNAVAILABLE")
	registry(s).
		Model = model
	p, err = s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{AspectRatio: "9:16", LLMModel: "deepseek-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.VisualPrompts, before.VisualPrompts) {
		t.Fatal("settings invalidated text")
	}
	saved := d.Clone(p)
	registry(s).
		Model = nil
	registry(s).
		OptionsValue = ports.ModelOptions{}
	replay, err := s.SaveSettings(ctx, p.ID, p.Revision, *p.ProjectSettings)
	if err != nil || !reflect.DeepEqual(replay, saved) {
		t.Fatal("identical confirmation not read only", err)
	}
	for _, v := range []d.ProjectSettings{{}, {AspectRatio: "16:9"}, {LLMModel: "unknown"}, {ImageModel: "x"}, {VideoModel: "x"}} {
		_, err = s.SaveSettings(ctx, p.ID, p.Revision, v)
		expectCode(t, err, "SETTINGS_LOCKED")
	}
	_, err = s.SaveSettings(ctx, p.ID, p.Revision-1, *p.ProjectSettings)
	expectCode(t, err, "CONFLICT")
	after, _ := s.Store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(after, saved) {
		t.Fatal("locked edit changed project")
	}
}

func TestProjectContentSettingsFirstConfirmationAtomicAndConcurrent(t *testing.T) {
	s, p := contentFixture(t, contentAssets())
	ctx := context.Background()
	original := d.Clone(p)
	store := s.Store
	s.Store = brokenCommit{ProjectStore: store, err: errors.New("controlled commit interruption")}
	if _, err := s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{AspectRatio: "16:9"}); err == nil {
		t.Fatal("failed commit reported confirmed")
	}
	after, _ := store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(after, original) {
		t.Fatal("failed confirmation partially saved")
	}
	s.Store = store
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, ratio := range []string{"16:9", "9:16"} {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			_, err := s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{AspectRatio: v})
			errs <- err
		}(ratio)
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			expectCode(t, err, "CONFLICT")
		}
	}
	after, _ = store.Snapshot(ctx, p.ID)
	if success != 1 || after.ProjectSettings == nil || after.Revision != p.Revision+1 || !reflect.DeepEqual(after.Runs, p.Runs) || after.UsedMicroCNY() != p.UsedMicroCNY() {
		t.Fatal("confirmation race changed protected results")
	}
	s, p = contentFixture(t, nil)
	p, err := s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{AspectRatio: "1:1"})
	if err != nil || p.ProjectSettings.AspectRatio != "1:1" {
		t.Fatal("empty choice could not be completed", err)
	}
	_, err = s.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{AspectRatio: "16:9"})
	expectCode(t, err, "SETTINGS_LOCKED")
}
