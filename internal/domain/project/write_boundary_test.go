package project

import (
	"errors"
	"reflect"
	"testing"

	"drama-agent/internal/domain/fault"
)

func writeBoundaryFixture() Project {
	return Project{ScriptSource: ScriptSource{Version: 1}, Assets: []Asset{{ID: "character", Kind: "character", Name: "角色", Looks: []Look{{ID: "look", Name: "附加"}}}, {ID: "prop", Kind: "prop", Name: "铜钥匙"}}, Runs: []Run{{ID: "catalog", Contract: CatalogExtractionContract, SourceVersion: 1, Status: RunCompleted}, {ID: "visual", Contract: VisualContract, SourceVersion: 1, Status: RunRunning, Targets: []VisualTarget{{AssetID: "prop"}}}}}
}

func TestAggregateRejectsAllUserAssetWritesWhileTaskActive(t *testing.T) {
	commands := map[string]func(*Project) error{
		"update asset": func(p *Project) error {
			return p.UpdateAsset("prop", AssetInput{Kind: "prop", Name: "新版铜钥匙", Aliases: []string{}})
		},
		"remove asset": func(p *Project) error { return p.RemoveAsset("prop") },
		"update look":  func(p *Project) error { return p.UpdateLook("character", "look", LookInput{Name: "新附加"}) },
		"remove look":  func(p *Project) error { return p.RemoveLook("character", "look") },
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			p := writeBoundaryFixture()
			before := Clone(p)
			err := command(&p)
			var coded *fault.Error
			if !errors.As(err, &coded) || coded.Code != "RUN_ACTIVE" || !reflect.DeepEqual(before, p) {
				t.Fatal("运行输入被普通写入改变", err)
			}
		})
	}
}

func TestAggregateVisualWritesProtectTaskStateOwnershipSourceAndReference(t *testing.T) {
	cases := map[string]func(*Project, *ActionRef) *Run{
		"cancelled":         func(p *Project, _ *ActionRef) *Run { p.LatestRun().Status = RunCancelled; return p.LatestRun() },
		"failed":            func(p *Project, _ *ActionRef) *Run { p.LatestRun().Status = RunFailed; return p.LatestRun() },
		"completed":         func(p *Project, _ *ActionRef) *Run { p.LatestRun().Status = RunCompleted; return p.LatestRun() },
		"wrong source":      func(p *Project, _ *ActionRef) *Run { p.LatestRun().SourceVersion = 2; return p.LatestRun() },
		"wrong contract":    func(p *Project, _ *ActionRef) *Run { p.LatestRun().Contract = OutlineContract; return p.LatestRun() },
		"detached snapshot": func(p *Project, _ *ActionRef) *Run { copy := *p.LatestRun(); return &copy },
		"wrong reference":   func(p *Project, ref *ActionRef) *Run { ref.RunID = "foreign"; return p.LatestRun() },
		"missing action":    func(p *Project, ref *ActionRef) *Run { ref.ActionID = ""; return p.LatestRun() },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			p := writeBoundaryFixture()
			ref := ActionRef{RunID: "visual", CallID: "call", ActionID: "action"}
			run := setup(&p, &ref)
			before := Clone(p)
			_, _, err := p.SaveVisualPrompts(run, ref, []VisualPromptInput{{Target: VisualTarget{AssetID: "prop"}, Text: "完整正文"}})
			if err == nil || !reflect.DeepEqual(before, p) {
				t.Fatal("非法状态或引用导致正文采用", err)
			}
		})
	}
	p := writeBoundaryFixture()
	_, _, err := p.SaveVisualPrompts(p.LatestRun(), ActionRef{RunID: "visual", CallID: "call", ActionID: "action"}, []VisualPromptInput{{Target: VisualTarget{AssetID: "prop"}, Text: "完整正文"}})
	if err != nil || len(p.VisualPrompts) != 1 {
		t.Fatal("合法任务未采用", err)
	}
}
