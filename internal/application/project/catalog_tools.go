package project

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func validActions(finish string, actions []d.Action) bool {
	valid := finish == "tool_calls" && len(actions) > 0
	seen := map[string]bool{}
	for _, a := range actions {
		if a.ID == "" || seen[a.ID] || (a.Name != "save_catalog" && a.Name != "finish_batch") {
			valid = false
		}
		seen[a.ID] = true
	}
	return valid
}

func (s *Service) rejectResponse(ctx context.Context, id, runID, callID string) error {
	return s.update(ctx, id, func(q *d.Project) error {
		rc, cc := callByID(q, runID, callID)
		if cc == nil {
			return fault.ErrNotFound
		}
		cc.Status, cc.ErrorCode, cc.Error = "rejected", "PROTOCOL_REJECTED", "invalid or missing explicit function calls"
		if cc.FinishReason == "length" {
			cc.ErrorCode, cc.Error = "MODEL_OUTPUT_TRUNCATED", "provider exhausted output capacity; incomplete output was not adopted"
		}
		if rc.Status == "running" {
			rc.Status, rc.ErrorCode, rc.Error = "failed", cc.ErrorCode, cc.Error
			ended := time.Now().UTC()
			rc.EndedAt = &ended
		}
		return nil
	})
}

func (s *Service) executeTool(ctx context.Context, id, actionID string) (ports.ToolResult, error) {
	p, err := s.Store.Snapshot(ctx, id)
	if err != nil {
		return ports.ToolResult{}, err
	}
	if err = s.checkAgentAccess(ctx, p); err != nil {
		return ports.ToolResult{}, s.stopAgentForPermission(ctx, id, err)
	}
	tools := s.agentTools()
	var result ports.ToolResult
	r := p.LatestRun()
	if r != nil && len(r.Calls) > 0 {
		format := r.Calls[len(r.Calls)-1].SourceReferenceFormat
		if format != "" && format != modelSourceReferenceFormat {
			return result, fmt.Errorf("%w: unsupported source reference format", fault.ErrInvalid)
		}
	}
	if r != nil && isStoryboard(r.Contract) {
		result, err = tools.executeStoryboardTool(ctx, id, actionID)
	} else if r != nil && r.ExecutionContract() != d.CatalogExtractionContract {
		result, err = tools.executeContentTool(ctx, id, actionID)
	} else {
		result, err = tools.executeCatalogTool(ctx, id, actionID)
	}
	if err != nil || result.Text == "" {
		return result, s.stopAgentForPermission(ctx, id, err)
	}
	// 存储保持真实内部 ID；仅给模型的同一回执视图采用原 Call 的表示。
	if r != nil && len(r.Calls) > 0 {
		result.Text, err = modelSourceReceipt(p, r.Calls[len(r.Calls)-1], result.Text)
	}
	return result, err
}

func (s *Service) executeCatalogTool(ctx context.Context, id, actionID string) (ports.ToolResult, error) {
	var result ports.ToolResult
	err := s.update(ctx, id, func(p *d.Project) error {
		r := p.LatestRun()
		if r == nil || r.Status != "running" || r.SourceVersion != p.ScriptSource.Version {
			return fault.ErrCancelled
		}
		if len(r.Calls) == 0 {
			return fault.ErrInvalid
		}
		c := &r.Calls[len(r.Calls)-1]
		var action *d.Action
		for i := range c.Actions {
			if c.Actions[i].ID == actionID {
				action = &c.Actions[i]
				break
			}
		}
		if action == nil {
			return fault.ErrInvalid
		}
		if action.Status != "pending" {
			result.Text = action.Result
			return nil
		}
		var toolErr error
		var changes []catalogChange
		if c.Stage != r.Stage || c.Start != r.Cursor || c.End != r.End {
			toolErr = fmt.Errorf("%w: tool belongs to a completed input scope", fault.ErrConflict)
		} else if action.Name == "save_catalog" {
			arguments, e := stripModelFence(action.Arguments)
			if e == nil {
				arguments, e = sourceArguments(*p, *c, arguments)
			}
			var draft CatalogChangeSet
			if e == nil {
				draft, e = Decode(arguments)
			}
			toolErr = e
			if toolErr == nil {
				assets, e := d.AdoptCatalogChanges(draft.DomainAssets(), draft.RemoveIDs, p.Assets, p.ScriptSource)
				toolErr = e
				if e == nil {
					changes = catalogChanges(p.Assets, assets)
					p.Assets, c.After = assets, assets
				}
			}
		} else if action.Name == "finish_batch" {
			// 同一响应里被拒绝的写入必须先反馈，不能直接推进目标范围。
			// 这保护执行回执连续性，不替模型判定目录的剧情理解。
			for _, prior := range c.Actions {
				if prior.ID == action.ID {
					break
				}
				if prior.Status == "rejected" {
					toolErr = fmt.Errorf("%w: previous tool in this response was rejected; inspect its feedback before finishing", fault.ErrInvalid)
					break
				}
			}
			var empty map[string]json.RawMessage
			if json.Unmarshal([]byte(action.Arguments), &empty) != nil || empty == nil || len(empty) != 0 {
				toolErr = fmt.Errorf("%w: finish_batch takes an empty object", fault.ErrInvalid)
			}
			if toolErr == nil && r.Stage == "extract" {
				saved := false
				for _, prior := range r.Calls {
					if prior.Stage == r.Stage && prior.Start == r.Cursor {
						for _, a := range prior.Actions {
							if a.Name == "save_catalog" && a.Status == "applied" {
								saved = true
							}
						}
					}
				}
				if !saved {
					toolErr = fmt.Errorf("%w: no saved catalog for the current input", fault.ErrInvalid)
				}
			}
			if toolErr == nil {
				r.Cursor, r.End = c.End, 0
				if r.Cursor == len(p.ScriptSource.Spans) {
					if r.Stage == "extract" {
						r.Stage, r.Cursor = "correct", 0
					} else {
						r.Stage, r.Cursor, r.End = "visualPrompt", 0, 0
						r.Targets = p.PendingVisualTargets()
						if len(r.Targets) == 0 {
							if len(p.Assets) == 0 {
								r.Status, r.ErrorCode, r.Error = "failed", "INVALID_INPUT", d.EmptyVisualError
								ended := time.Now().UTC()
								r.EndedAt = &ended
							} else if err := r.Complete(); err != nil {
								return err
							}
						}
						// 结束本次 Eino 会话；由 Execute 以新阶段的工具集开启下一段。
						result.Finished = true
					}
				}
			}
		} else {
			toolErr = fault.ErrInvalid
		}
		payload := map[string]any{"ok": toolErr == nil, "savedCount": len(p.Assets), "status": r.Status, "stage": r.Stage}
		if toolErr == nil && action.Name == "save_catalog" {
			payload["changes"] = changes
		}
		if toolErr != nil {
			action.Status = "rejected"
			payload["error"] = toolErr.Error()
		} else {
			action.Status = "applied"
		}
		encoded, _ := json.Marshal(payload)
		action.Result, result.Text = string(encoded), string(encoded)
		finished, rejected := true, false
		for _, a := range c.Actions {
			finished = finished && a.Status != "pending"
			rejected = rejected || a.Status == "rejected"
		}
		if finished {
			if rejected {
				c.Status = "rejected"
			} else {
				c.Status = "applied"
			}
		}
		return nil
	})
	return result, err
}
