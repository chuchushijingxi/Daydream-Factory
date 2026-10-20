package project

import (
	"strings"

	"drama-agent/internal/domain/fault"
)

type VisualTarget struct {
	AssetID string `json:"assetId"`
	LookID  string `json:"lookId,omitempty"`
}

func (t VisualTarget) Key() string { return t.AssetID + "\x00" + t.LookID }

type VisualPromptInput struct {
	Target      VisualTarget `json:"target"`
	Text        string       `json:"text"`
	DesignNotes string       `json:"designNotes"`
}

type VisualPrompt struct {
	VisualPromptInput
	SourceVersion uint64 `json:"sourceVersion"`
	ActionRef
	BasePrompt *ActionRef `json:"basePrompt"`
	Stale      bool       `json:"stale"`
	Origin     string     `json:"origin,omitempty"`
}

func (p *Project) Prompt(t VisualTarget) *VisualPrompt {
	for i := range p.VisualPrompts {
		if p.VisualPrompts[i].Target == t {
			return &p.VisualPrompts[i]
		}
	}
	return nil
}

func (p *Project) ValidateTarget(t VisualTarget) error {
	a := p.Asset(t.AssetID)
	if a == nil {
		return fault.ErrNotFound
	}
	if t.LookID != "" {
		if a.Kind != "character" {
			return fault.ErrInvalid
		}
		for _, l := range a.Looks {
			if l.ID == t.LookID {
				return nil
			}
		}
		return fault.ErrNotFound
	}
	return nil
}

// 有效性只核对来源、任务、冻结范围和基础 ActionRef，不评价正文的剧情或设计质量。
// runID 非空仅供本任务核对尚未终结的正文；公开使用必须追溯到 completed 任务。
func (p *Project) ValidPrompt(v *VisualPrompt, runID string) bool {
	if v == nil || v.Stale || (runID != "" && v.RunID != runID) || v.SourceVersion != p.ScriptSource.Version || strings.TrimSpace(v.Text) == "" || p.ValidateTarget(v.Target) != nil {
		return false
	}
	if runID == "" && v.Origin != "user" {
		completed := false
		for _, r := range p.Runs {
			if r.ID == v.RunID && r.ExecutionContract() == VisualContract && r.SourceVersion == p.ScriptSource.Version && r.Status == "completed" {
				for _, t := range r.Targets {
					completed = completed || t == v.Target
				}
			}
		}
		if !completed {
			return false
		}
	}
	if v.Target.LookID != "" {
		// 基础正文修订后旧附加必须失效，不能只凭同一个角色 ID 继续使用。
		b := p.Prompt(VisualTarget{AssetID: v.Target.AssetID})
		return b != nil && (p.ValidPrompt(b, runID) || p.ValidPrompt(b, "")) && v.BasePrompt != nil && *v.BasePrompt == b.ActionRef
	}
	return v.BasePrompt == nil
}

func (p *Project) PendingVisualTargets() []VisualTarget {
	out := []VisualTarget{}
	for _, a := range p.Assets {
		targets := []VisualTarget{{AssetID: a.ID}}
		for _, l := range a.Looks {
			targets = append(targets, VisualTarget{AssetID: a.ID, LookID: l.ID})
		}
		for _, t := range targets {
			if !p.ValidPrompt(p.Prompt(t), "") {
				out = append(out, t)
			}
		}
	}
	return out
}

func (p *Project) VisualComplete(r *Run) bool {
	if r == nil || len(r.Targets) == 0 {
		return false
	}
	for _, t := range r.Targets {
		if !p.ValidPrompt(p.Prompt(t), r.ID) {
			return false
		}
	}
	return true
}
