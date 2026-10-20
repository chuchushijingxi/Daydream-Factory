package project

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// 只压缩程序自己的结构化回执，不从模型自由诊断推断修改对象。
func latestFeedback(p d.Project, dialogue []ports.Message) ([]json.RawMessage, map[string]bool, error) {
	feedback := []json.RawMessage{}
	support := map[string]bool{}
	observed := map[string]bool{}
	for _, message := range dialogue {
		if message.Role == "tool" {
			observed[message.ToolCallID] = true
		}
	}
	r := p.LatestRun()
	if len(r.Calls) == 0 {
		return feedback, support, nil
	}
	last := len(r.Calls) - 1
	for last >= 0 && interruptedCall(r.Calls[last]) {
		last--
	}
	if last < 0 {
		return feedback, support, nil
	}
	for _, action := range r.Calls[last].Actions {
		if action.Result == "" {
			continue
		}
		var receipt map[string]json.RawMessage
		if err := json.Unmarshal([]byte(action.Result), &receipt); err != nil {
			return nil, nil, fmt.Errorf("invalid saved tool receipt: %w", err)
		}
		if raw, ok := receipt["changes"]; ok {
			var changes []catalogChange
			if err := json.Unmarshal(raw, &changes); err != nil {
				return nil, nil, fmt.Errorf("invalid saved tool changes: %w", err)
			}
			for i := range changes {
				change := &changes[i]
				for _, item := range []*d.Asset{change.Before, change.After} {
					if item != nil {
						for _, id := range item.SourceIDs {
							support[id] = true
						}
					}
				}
				if change.After != nil {
					for _, current := range p.Assets {
						if reflect.DeepEqual(current, *change.After) {
							change.AfterID = current.ID
							change.After = nil
							break
						}
					}
				}
			}
			receipt["changes"], _ = json.Marshal(changes)
		}
		encoded, _ := json.Marshal(receipt)
		if !observed[action.ID] {
			feedback = append(feedback, encoded)
		}
	}
	return feedback, support, nil
}

func (s *Service) request(ctx context.Context, p d.Project) (ports.Request, int, error) {
	r := p.LatestRun()
	if isStoryboard(r.Contract) {
		return s.storyboardRequest(ctx, p)
	}
	if r.ExecutionContract() != d.CatalogExtractionContract {
		return s.contentRequest(ctx, p)
	}
	bound, err := s.resolve(ctx, r.Model)
	if err != nil {
		return ports.Request{}, 0, err
	}
	model := bound.Model
	window, output := model.Capacity()
	if window <= 0 || output <= 0 {
		return ports.Request{}, 0, fault.ErrCapacity
	}
	prompt := ExtractPrompt
	if r.Stage == "correct" {
		prompt = CorrectPrompt
	}
	dialogue, err := taskDialogue(p)
	if err != nil {
		return ports.Request{}, 0, err
	}
	tools := Tools()
	toolsJSON, _ := json.Marshal(tools)
	feedback, feedbackSource, err := latestFeedback(p, dialogue)
	if err != nil {
		return ports.Request{}, 0, err
	}
	build := func(end int) (ports.Request, error) {
		selected := map[string]bool{}
		for _, call := range r.Calls {
			if interruptedCall(call) {
				continue
			}
			if call.Stage != r.Stage || call.Start != r.Cursor || call.End != r.End {
				continue
			}
			for _, assets := range [][]d.Asset{call.Before, call.After} {
				for _, a := range assets {
					for _, id := range a.SourceIDs {
						selected[id] = true
					}
				}
			}
		}
		for id := range feedbackSource {
			selected[id] = true
		}
		targets := []string{}
		for _, span := range p.ScriptSource.Spans[r.Cursor:end] {
			selected[span.ID] = true
			targets = append(targets, span.ID)
		}
		for _, a := range p.Assets {
			for _, id := range a.SourceIDs {
				selected[id] = true
			}
		}
		// 相邻原文仅补充连贯语境，程序不裁决场景或剧情边界。
		for _, i := range []int{0, r.Cursor - 1, end} {
			if i >= 0 && i < len(p.ScriptSource.Spans) {
				selected[p.ScriptSource.Spans[i].ID] = true
			}
		}
		sources := []d.SourceSpan{}
		for _, span := range p.ScriptSource.Spans {
			if selected[span.ID] {
				sources = append(sources, span)
			}
		}
		materials := make([]d.Asset, 0, len(p.Assets))
		for _, asset := range p.Assets {
			materials = append(materials, textAsset(asset))
		}
		input, _ := json.Marshal(struct {
			SourceVersion uint64            `json:"sourceVersion"`
			TargetSpanIDs []string          `json:"targetSpanIds"`
			ScriptSource  []d.SourceSpan    `json:"source"`
			Assets        []d.Asset         `json:"assets"`
			Feedback      []json.RawMessage `json:"lastToolResults,omitempty"`
		}{p.ScriptSource.Version, targets, sources, materials, feedback})
		return modelSourceRequest(p, ports.Request{System: prompt, Input: string(input), MaxOutputTokens: output, Tools: tools, Dialogue: dialogue})
	}
	// UTF-8 字节数作为保守 token 上界，不猜测回答长度或虚增模型窗口。
	fits := func(req ports.Request) bool { return inputSize(req)+len(toolsJSON)+output <= window }
	if r.End > r.Cursor {
		req, err := build(r.End)
		if err != nil {
			return req, 0, err
		}
		req.MaxOutputTokens = min(output, window-inputSize(req)-len(toolsJSON))
		if req.MaxOutputTokens <= 0 {
			return ports.Request{}, 0, fault.ErrCapacity
		}
		return req, r.End, nil
	}
	end := len(p.ScriptSource.Spans)
	req, err := build(end)
	if err != nil {
		return req, 0, err
	}
	if !fits(req) {
		lo, hi := r.Cursor+1, end
		minimum, err := build(lo)
		if err != nil {
			return req, 0, err
		}
		if !fits(minimum) {
			// 先保证目标及支持原文完整装入真实窗口，再使用剩余输出容量。
			// 二分当前目标范围不删段落、不摘要，也不按预估回答长度切碎材料。
			end = len(p.ScriptSource.Spans)
			for {
				req, err = build(end)
				if err != nil {
					return req, 0, err
				}
				if inputSize(req)+len(toolsJSON) < window || end == lo {
					break
				}
				end = lo + (end-lo)/2
			}
			req.MaxOutputTokens = min(output, window-inputSize(req)-len(toolsJSON))
			if req.MaxOutputTokens <= 0 {
				return ports.Request{}, 0, fmt.Errorf("%w: catalog, supporting source and one paragraph exhaust verified capacity", fault.ErrCapacity)
			}
			return req, end, nil
		}
		for lo < hi {
			mid := (lo + hi + 1) / 2
			candidate, err := build(mid)
			if err != nil {
				return req, 0, err
			}
			if fits(candidate) {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		end = lo
		req, err = build(end)
		if err != nil {
			return req, 0, err
		}
	}
	req.MaxOutputTokens = min(output, window-inputSize(req)-len(toolsJSON))
	return req, end, nil
}
