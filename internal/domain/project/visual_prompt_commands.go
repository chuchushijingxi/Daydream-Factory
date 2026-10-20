package project

import (
	"fmt"
	"strings"

	"drama-agent/internal/domain/fault"
)

// VisualPromptChange 保存实际前后值，回执与聚合的采用结果一致。
type VisualPromptChange struct {
	Target VisualTarget  `json:"target"`
	Before *VisualPrompt `json:"before"`
	After  *VisualPrompt `json:"after"`
}

// 先验证全部目标并在副本采用；任一附加形象缺基础依赖则整体拒绝。
func (p *Project) SaveVisualPrompts(r *Run, ref ActionRef, prompts []VisualPromptInput) ([]VisualPromptChange, []VisualTarget, error) {
	// 只采用本聚合当前源的运行任务。取消后的迟到响应仍可结算，不能写正文。
	if r == nil || r.Status != RunRunning {
		return nil, nil, fault.ErrCancelled
	}
	owned := false
	for i := range p.Runs {
		owned = owned || &p.Runs[i] == r
	}
	if !owned || r.ExecutionContract() != VisualContract || r.SourceVersion != p.ScriptSource.Version || ref.RunID != r.ID || ref.CallID == "" || ref.ActionID == "" {
		return nil, nil, fault.ErrInvalid
	}
	if len(prompts) == 0 {
		return nil, nil, fault.ErrInvalid
	}
	candidate := Clone(*p)
	scope := map[string]bool{}
	for _, t := range r.Targets {
		scope[t.Key()] = true
	}
	seen := map[string]bool{}
	for _, v := range prompts {
		if strings.TrimSpace(v.Target.AssetID) == "" || strings.TrimSpace(v.Text) == "" || seen[v.Target.Key()] {
			return nil, nil, fault.ErrInvalid
		}
		if err := p.ValidateTarget(v.Target); err != nil {
			return nil, nil, err
		}
		if !scope[v.Target.Key()] {
			return nil, nil, fault.ErrInvalid
		}
		seen[v.Target.Key()] = true
	}
	invalidated := []VisualTarget{}
	// 先保存基础再保存附加形象，供应商返回顺序不影响依赖的一致性。
	for _, v := range prompts {
		if v.Target.LookID != "" {
			continue
		}
		artifact := VisualPrompt{VisualPromptInput: v, SourceVersion: p.ScriptSource.Version, ActionRef: ref}
		replacePrompt(&candidate, artifact)
		for i := range candidate.VisualPrompts {
			child := &candidate.VisualPrompts[i]
			if child.Target.AssetID == v.Target.AssetID && child.Target.LookID != "" && !child.Stale {
				child.Stale = true
				invalidated = append(invalidated, child.Target)
			}
		}
	}
	for _, v := range prompts {
		if v.Target.LookID == "" {
			continue
		}
		base := candidate.Prompt(VisualTarget{AssetID: v.Target.AssetID})
		if !candidate.ValidPrompt(base, r.ID) && !candidate.ValidPrompt(base, "") {
			return nil, nil, fmt.Errorf("%w: attached look needs the effective parent base prompt", fault.ErrInvalid)
		}
		b := base.ActionRef
		replacePrompt(&candidate, VisualPrompt{VisualPromptInput: v, SourceVersion: p.ScriptSource.Version, ActionRef: ref, BasePrompt: &b})
	}
	changes := []VisualPromptChange{}
	for _, v := range prompts {
		changes = append(changes, VisualPromptChange{Target: v.Target, Before: p.Prompt(v.Target), After: candidate.Prompt(v.Target)})
	}
	p.VisualPrompts = candidate.VisualPrompts
	return changes, invalidated, nil
}

func replacePrompt(p *Project, v VisualPrompt) {
	for i := range p.VisualPrompts {
		if p.VisualPrompts[i].Target == v.Target {
			p.VisualPrompts[i] = v
			return
		}
	}
	p.VisualPrompts = append(p.VisualPrompts, v)
}

// EditVisualPrompt 是普通用户命令，不是模型调用。
func (p *Project) EditVisualPrompt(target VisualTarget, text string) error {
	if err := p.ensureAssetsEditable(); err != nil {
		return err
	}
	if err := p.ValidateTarget(target); err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return fault.ErrInvalid
	}
	v := VisualPrompt{VisualPromptInput: VisualPromptInput{Target: target, Text: text}, SourceVersion: p.ScriptSource.Version, Origin: "user"}
	if target.LookID != "" {
		base := p.Prompt(VisualTarget{AssetID: target.AssetID})
		if !p.ValidPrompt(base, "") {
			return fault.New("BASE_PROMPT_REQUIRED", "请先补齐角色基础提示词")
		}
		ref := base.ActionRef
		v.BasePrompt = &ref
	} else {
		p.invalidatePrompts(target.AssetID, "", false)
	}
	replacePrompt(p, v)
	p.CatalogEditedByUser = true
	return nil
}
