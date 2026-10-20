package project

import (
	"context"
	"errors"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// A live/dead predecessor may still own the Redis lease. Recovery must wait
// for ownership instead of treating a skipped acquisition as completed work.
var errExecutionBusy = errors.New("project execution lease is held")

func callByID(p *d.Project, runID, callID string) (*d.Run, *d.Call) {
	for i := range p.Runs {
		if p.Runs[i].ID == runID {
			for j := range p.Runs[i].Calls {
				if p.Runs[i].Calls[j].ID == callID {
					return &p.Runs[i], &p.Runs[i].Calls[j]
				}
			}
		}
	}
	return nil, nil
}

func (s *Service) fail(ctx context.Context, id, runID, code, message string) error {
	return s.update(ctx, id, func(p *d.Project) error {
		r := p.LatestRun()
		if r != nil && r.ID == runID && r.Status == "running" {
			ended := time.Now().UTC()
			r.EndedAt = &ended
			r.Status = "failed"
			r.ErrorCode = code
			r.Error = message
		}
		return nil
	})
}

// Execute 管理项目隔离和持久化恢复；Eino 负责选择及迭代工具。
func (s *Service) Execute(ctx context.Context, id string) error {
	release, acquired, err := s.Store.Acquire(ctx, id)
	if err != nil {
		return err
	}
	if !acquired {
		return errExecutionBusy
	}
	defer release.Release()
	ctx = release.Context
	p, err := s.Store.Snapshot(ctx, id)
	if err != nil {
		return err
	}
	r := p.LatestRun()
	if r == nil || r.Status != "running" {
		return nil
	}
	if err = s.ensureCredit(ctx, p, r); err != nil {
		code := errorCode(err)
		if f, ok := err.(*fault.Error); ok {
			code = f.Code
		}
		return s.fail(ctx, id, r.ID, code, err.Error())
	}
	if !d.KnownContract(r.Contract) || r.SourceVersion != p.ScriptSource.Version {
		return s.fail(ctx, id, r.ID, "STALE_RUN", "run contract or source version is stale")
	}
	bound, err := s.resolve(ctx, r.Model)
	if err != nil {
		code := "MODEL_UNAVAILABLE"
		if e, ok := err.(*fault.Error); ok {
			code = e.Code
		}
		return s.fail(ctx, id, r.ID, code, err.Error())
	}
	ctx = context.WithValue(ctx, resolvedModelKey{}, bound)
	runID := r.ID
	if len(r.Calls) > 0 {
		c := r.Calls[len(r.Calls)-1]
		if c.Status == "pending" || (c.Status == "unknown" && !interruptedCall(c)) {
			return s.fail(ctx, id, runID, "MODEL_RESULT_UNKNOWN", "started request has no complete persisted response; it will not be resent")
		}
		if c.Status == "received" {
			if !validTaskActions(r.ExecutionContract(), c.FinishReason, c.Actions) {
				return s.rejectResponse(ctx, id, runID, c.ID)
			}
			for _, a := range c.Actions {
				if a.Status == "pending" {
					if _, err = s.executeTool(ctx, id, a.ID); err != nil {
						return err
					}
				}
			}
		}
	}
	for {
		advanced, err := s.executePhase(ctx, id, runID)
		if err != nil || !advanced {
			return err
		}
	}
}

// executePhase 驱动当前阶段的一次完整 Agent 会话并核对结果；返回 advanced 表示
// 阶段已推进，由 Execute 继续下一阶段。回执上下文随本次调用结束释放，
// 不在 Execute 的阶段循环内累积。
func (s *Service) executePhase(ctx context.Context, id, runID string) (advanced bool, err error) {
	p, err := s.Store.Snapshot(ctx, id)
	if err != nil || p.LatestRun().Status != "running" || p.LatestRun().ID != runID {
		return false, err
	}
	if s.Agent == nil {
		return false, s.fail(ctx, id, runID, "MODEL_UNAVAILABLE", fault.ErrModelUnavailable.Error())
	}
	if (p.LatestRun().ExecutionContract() == d.VisualContract || p.LatestRun().Contract == d.StoryboardContract) && s.Evidence != nil {
		// 在任何收费文本请求前备好完整派生索引；P3、空目录失败与只读浏览都不会进入这里。
		if err = s.Evidence.Ensure(ctx, ports.EvidenceDocument{ProjectID: p.ID, Source: p.ScriptSource}); err != nil {
			code := "EVIDENCE_UNAVAILABLE"
			if e, ok := err.(*fault.Error); ok {
				code = e.Code
			}
			return false, s.fail(ctx, id, runID, code, err.Error())
		}
	}
	phase := p.LatestRun().Stage
	history, err := taskDialogue(p)
	if err != nil {
		return false, s.fail(ctx, id, runID, "PROTOCOL_REJECTED", err.Error())
	}
	err = s.Agent.Run(ctx, ports.AgentSession{
		RejectUnknownTools: p.LatestRun().ExecutionContract() != d.CatalogExtractionContract,
		Dialogue:           history,
		Tools:              runTaskTools(p.LatestRun()),
		Instruction:        taskInstruction(p.LatestRun().ExecutionContract()),
		Generate: func(callCtx context.Context, messages []ports.Message) (ports.Response, error) {
			return s.generate(callCtx, id, runID, messages)
		},
		Execute: func(toolCtx context.Context, actionID string) (ports.ToolResult, error) {
			return s.executeTool(toolCtx, id, actionID)
		},
	})
	saveCtx, cancel := s.receiptContext(ctx)
	defer cancel()
	if err != nil {
		if e := s.fail(saveCtx, id, runID, "AGENT_FAILED", err.Error()); e != nil {
			return false, e
		}
		return false, err
	}
	p, err = s.Store.Snapshot(saveCtx, id)
	if err != nil {
		return false, err
	}
	// 自动分集完成可已原子接续另一个 Run；旧 Agent 不得用旧身份执行新任务。
	if p.LatestRun().ID != runID {
		return false, nil
	}
	if p.LatestRun().Status == "running" && p.LatestRun().Stage != phase {
		// 阶段推进后由下一次调用重建材料与新工具集。
		return true, nil
	}
	if p.LatestRun().Status == "running" {
		return false, s.fail(saveCtx, id, runID, "MODEL_DID_NOT_FINISH", "model stopped without completing the catalog through the tool")
	}
	return false, nil
}
