package project

import (
	"errors"
	"reflect"
	"testing"

	"drama-agent/internal/domain/fault"
)

func TestVisualPromptDependencyFailureDoesNotPartiallyAdopt(t *testing.T) {
	p := Project{
		ScriptSource:  ScriptSource{Version: 1},
		Assets:        []Asset{{ID: "base", Kind: "character"}, {ID: "other", Kind: "character", Looks: []Look{{ID: "look"}}}},
		Runs:          []Run{{ID: "visual", Contract: VisualContract, SourceVersion: 1, Status: RunRunning, Targets: []VisualTarget{{AssetID: "base"}, {AssetID: "other", LookID: "look"}}}},
		VisualPrompts: []VisualPrompt{},
	}
	before := Clone(p)
	_, _, err := p.SaveVisualPrompts(p.LatestRun(), ActionRef{RunID: "visual", CallID: "call", ActionID: "action"}, []VisualPromptInput{
		{Target: VisualTarget{AssetID: "base"}, Text: "第一项可保存"},
		{Target: VisualTarget{AssetID: "other", LookID: "look"}, Text: "缺少父级基础依赖"},
	})
	if !errors.Is(err, fault.ErrInvalid) || !reflect.DeepEqual(p, before) {
		t.Fatal("后续依赖失败时不能采用前面的基础正文", err, p.VisualPrompts)
	}
}

func TestAssetChangesInvalidateOnlyTheirOwnVisualDependents(t *testing.T) {
	p := Project{
		ScriptSource: ScriptSource{Version: 1},
		Assets:       []Asset{{ID: "character", Kind: "character", Name: "角色", Looks: []Look{{ID: "look", Name: "形象"}}}, {ID: "prop", Kind: "prop", Name: "道具"}},
		Runs:         []Run{{Contract: CatalogExtractionContract, SourceVersion: 1, Status: RunCompleted}},
		VisualPrompts: []VisualPrompt{
			{VisualPromptInput: VisualPromptInput{Target: VisualTarget{AssetID: "character"}, Text: "基础正文"}},
			{VisualPromptInput: VisualPromptInput{Target: VisualTarget{AssetID: "character", LookID: "look"}, Text: "附加正文"}},
			{VisualPromptInput: VisualPromptInput{Target: VisualTarget{AssetID: "prop"}, Text: "正确道具正文"}},
		},
	}
	if err := p.UpdateLook("character", "look", LookInput{Name: "新形象", Description: "完整改动"}); err != nil {
		t.Fatal(err)
	}
	if p.VisualPrompts[0].Stale || !p.VisualPrompts[1].Stale || p.VisualPrompts[2].Stale || !p.CatalogEditedByUser {
		t.Fatal("附加形象更新影响了无关正文")
	}
	if err := p.RemoveAsset("character"); err != nil {
		t.Fatal(err)
	}
	if len(p.Assets) != 1 || len(p.VisualPrompts) != 1 || p.VisualPrompts[0].Text != "正确道具正文" || p.VisualPrompts[0].Stale {
		t.Fatal("删除角色应移除全部子正文，保留无关正确内容")
	}
}

func TestTerminalRunCannotCompleteOrCancelAgain(t *testing.T) {
	for _, status := range []RunStatus{RunCompleted, RunFailed, RunCancelled} {
		r := Run{Status: status}
		if !errors.Is(r.Complete(), fault.ErrConflict) || !errors.Is(r.Cancel(), fault.ErrConflict) || r.Status != status {
			t.Fatal("终结任务被转换或复活", status, r.Status)
		}
	}
}
