package project

import (
	"context"
	"encoding/json"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func (s *Service) contentRequest(ctx context.Context, p d.Project) (ports.Request, int, error) {
	r := p.LatestRun()
	bound, err := s.resolve(ctx, r.Model)
	if err != nil {
		return ports.Request{}, 0, err
	}
	model := bound.Model
	window, output := model.Capacity()
	if window <= 0 || output <= 0 {
		return ports.Request{}, 0, fault.ErrCapacity
	}
	history, err := taskDialogue(p)
	if err != nil {
		return ports.Request{}, 0, err
	}
	req := ports.Request{System: outlinePrompt, Tools: TaskTools(r.ExecutionContract()), MaxOutputTokens: output, Dialogue: history}
	var material any = struct {
		ScriptSource []d.SourceSpan `json:"source"`
		Outline      *d.Outline     `json:"outline"`
	}{p.ScriptSource.Spans, p.Outline}
	if r.ExecutionContract() == d.VisualContract {
		req.System = visualPrompt
		scope, assetIDs := map[d.VisualTarget]bool{}, map[string]bool{}
		for _, t := range r.Targets {
			scope[t], assetIDs[t.AssetID] = true, true
		}
		assets := make([]d.Asset, 0, len(p.Assets))
		for _, a := range p.Assets {
			if !assetIDs[a.ID] {
				continue
			}
			if a.Kind == "character" && a.Character == nil {
				a.Character = &d.Character{}
			}
			if a.Looks == nil {
				a.Looks = []d.Look{}
			}
			if r.Target != nil {
				looks := []d.Look{}
				for _, look := range a.Looks {
					if scope[d.VisualTarget{AssetID: a.ID, LookID: look.ID}] {
						looks = append(looks, look)
					}
				}
				a.Looks = looks
			}
			assets = append(assets, textAsset(a))
		}
		prompts := []visualPromptMaterial{}
		for _, v := range p.VisualPrompts {
			relevant := scope[v.Target] || (assetIDs[v.Target.AssetID] && v.Target.LookID == "")
			if relevant && (v.RunID == r.ID || p.ValidPrompt(&v, "") || r.Contract == d.CatalogExtractionContract && v.SourceVersion == p.ScriptSource.Version && !v.Stale) {
				prompts = append(prompts, promptModelMaterial(&p, v, r.ID))
			}
		}
		source := p.ScriptSource.Spans
		coverage := "complete_script"
		if r.Target != nil || r.Contract == d.CatalogExtractionContract {
			source, err = linkedEvidence(p, assets)
			if err != nil {
				return ports.Request{}, 0, err
			}
			coverage = "linked_evidence_only_not_complete"
		}
		material = struct {
			ScriptSource    []d.SourceSpan         `json:"source"`
			SourceVersion   uint64                 `json:"sourceVersion"`
			SourceSpanCount int                    `json:"sourceSpanCount"`
			SourceCoverage  string                 `json:"sourceCoverage"`
			Targets         []d.VisualTarget       `json:"targets"`
			Target          *d.VisualTarget        `json:"target,omitempty"`
			Assets          []d.Asset              `json:"assets"`
			Prompts         []visualPromptMaterial `json:"visualPrompts"`
		}{source, p.ScriptSource.Version, len(p.ScriptSource.Spans), coverage, r.Targets, r.Target, assets, prompts}
	}
	raw, err := json.Marshal(material)
	if err != nil {
		return ports.Request{}, 0, err
	}
	req.Input = string(raw)
	req, err = modelSourceRequest(p, req)
	if err != nil {
		return req, 0, err
	}
	tools, _ := json.Marshal(req.Tools)
	// 必需材料与补查观察保持完整；单项视觉任务以已引用证据起步，仍可显式检索或读取更多。
	// 工具 JSON 按字节保守抵扣 token 窗口（字节数≥token 数，不会虚增可用容量），与目录请求同一口径。
	req.MaxOutputTokens = min(output, window-inputSize(req)-len(tools))
	if req.MaxOutputTokens <= 0 {
		return ports.Request{}, 0, fault.ErrCapacity
	}
	return req, len(p.ScriptSource.Spans), nil
}
