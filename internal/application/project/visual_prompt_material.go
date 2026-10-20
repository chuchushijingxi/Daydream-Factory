package project

import d "drama-agent/internal/domain/project"

// 模型需要完整正文与实际可用性；调用引用只在内部及真实回执中追溯。
// valid 必须先按原始 Artifact 校验，不能因隐藏基础引用而把旧附加视为有效。
type visualPromptMaterial struct {
	d.VisualPromptInput
	SourceVersion uint64 `json:"sourceVersion"`
	Stale         bool   `json:"stale"`
	Origin        string `json:"origin,omitempty"`
	Valid         bool   `json:"valid"`
}

func promptModelMaterial(p *d.Project, v d.VisualPrompt, runID string) visualPromptMaterial {
	return visualPromptMaterial{
		VisualPromptInput: v.VisualPromptInput,
		SourceVersion:     v.SourceVersion,
		Stale:             v.Stale,
		Origin:            v.Origin,
		Valid:             p.ValidPrompt(&v, runID) || p.ValidPrompt(&v, ""),
	}
}
