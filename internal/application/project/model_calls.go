package project

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

const interruptionCode = "MODEL_RESPONSE_INTERRUPTED"

const maxInterruptedAttempts = 3

func interruptedCall(c d.Call) bool {
	return c.Status == "unknown" && c.ErrorCode == interruptionCode
}

func interruptionCount(r *d.Run) int {
	n := 0
	for i := len(r.Calls) - 1; i >= 0; i-- {
		c := r.Calls[i]
		if !interruptedCall(c) || c.Stage != r.Stage || c.Start != r.Cursor || c.End != r.End {
			break
		}
		n++
	}
	return n
}

// 这里只重试已明确分类的运输中断；模型规划与选工具仍交给 Eino。
// 次数从已保存调用倒查，重启不重置；每次未知费用预留独立保留。
func (s *Service) generate(ctx context.Context, id, runID string, messages []ports.Message) (ports.Response, error) {
	for {
		p, err := s.Store.Snapshot(ctx, id)
		if err != nil {
			return ports.Response{}, err
		}
		r := p.LatestRun()
		if r == nil || r.ID != runID || r.Status != "running" || r.SourceVersion != p.ScriptSource.Version {
			return ports.Response{}, fault.ErrCancelled
		}
		n := interruptionCount(r)
		if n >= maxInterruptedAttempts {
			err = s.fail(ctx, id, runID, interruptionCode, "response interrupted after three attempts; partial output discarded; saved catalog and unknown cost reservations retained")
			if err != nil {
				return ports.Response{}, err
			}
			return ports.Response{}, errors.New("model response retry limit reached")
		}
		if n > 0 {
			timer := time.NewTimer(time.Duration(n) * s.TransportRetryInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ports.Response{}, ctx.Err()
			case <-timer.C:
			}
		}
		response, err := s.generateAttempt(ctx, id, runID, messages)
		var interrupted interface{ ResponseInterrupted() bool }
		if err == nil || !errors.As(err, &interrupted) || !interrupted.ResponseInterrupted() || ctx.Err() != nil {
			return response, err
		}
	}
}

func (s *Service) generateAttempt(ctx context.Context, id, expectedRunID string, messages []ports.Message) (ports.Response, error) {
	p, err := s.Store.Snapshot(ctx, id)
	if err != nil {
		return ports.Response{}, err
	}
	r := p.LatestRun()
	if r == nil || r.ID != expectedRunID || r.Status != "running" || r.SourceVersion != p.ScriptSource.Version {
		return ports.Response{}, fault.ErrCancelled
	}
	dialogue, err := selectTaskDialogue(p, messages)
	if err != nil {
		return ports.Response{}, err
	}
	request, end, err := s.request(ctx, p)
	request.Dialogue = dialogue
	if err != nil {
		code := "PROGRAM_FAILED"
		if errors.Is(err, fault.ErrCapacity) {
			code = "CAPACITY_EXCEEDED"
		}
		if errors.Is(err, fault.ErrModelUnavailable) {
			code = "MODEL_UNAVAILABLE"
		}
		if saveErr := s.fail(ctx, id, r.ID, code, err.Error()); saveErr != nil {
			return ports.Response{}, saveErr
		}
		return ports.Response{}, err
	}
	budget, permitErr := s.permit(ctx, p, *r)
	if permitErr != nil {
		if e := s.fail(ctx, id, r.ID, "EXECUTION_NOT_AUTHORIZED", permitErr.Error()); e != nil {
			return ports.Response{}, e
		}
		return ports.Response{}, permitErr
	}
	toolsJSON, _ := json.Marshal(request.Tools)
	bound, err := s.resolve(ctx, r.Model)
	if err != nil {
		return ports.Response{}, err
	}
	pricing := bound.Pricing
	inputReserve := pricing.Reserve(inputSize(request)+len(toolsJSON), 0)
	// 用户手动启动且没有设置上限时只按模型真实容量执行，不继承旧实验余额。
	// 有明确有限策略的任务仍按该任务的已存上限核对，重启不重置。
	if r.BudgetMicroCNY > 0 || budget > 0 {
		ceiling := r.BudgetMicroCNY
		if ceiling <= 0 || budget > 0 && budget < ceiling {
			ceiling = budget
		}
		remaining := ceiling - RunUsed(r)
		if p.LimitMicroCNY > 0 {
			remaining = min(remaining, p.LimitMicroCNY-p.UsedMicroCNY())
		}
		affordable := pricing.AffordableOutput(remaining-inputReserve, request.MaxOutputTokens)
		if affordable <= 0 {
			if saveErr := s.fail(ctx, id, r.ID, "BUDGET_EXHAUSTED", "explicit remaining allowance cannot cover input and output; request not sent"); saveErr != nil {
				return ports.Response{}, saveErr
			}
			return ports.Response{}, fault.ErrBudget
		}
		request.MaxOutputTokens = min(request.MaxOutputTokens, int(affordable))
	}
	reserve := inputReserve + pricing.Reserve(0, request.MaxOutputTokens)
	call := d.Call{SourceReferenceFormat: modelSourceReferenceFormat, ConnectionID: bound.ConnectionID, RequestModelID: bound.RequestModelID, ID: identity.New("call"), Stage: r.Stage, Start: r.Cursor, End: end, Status: "pending", Before: p.Assets, ReservedMicroCNY: reserve, Pricing: &pricing, CreatedAt: time.Now().UTC()}
	if r.ExecutionContract() != d.CatalogExtractionContract {
		call.Before = nil
	}
	runID := r.ID
	r.End = end
	r.Calls = append(r.Calls, call)
	// 先提交调用意图和独立预留，再执行实际外发；提交失败不能调用供应商。
	if _, err = s.commit(ctx, p, p.Revision); err != nil {
		return ports.Response{}, err
	}
	response, callErr := bound.Model.Generate(ctx, request, func(raw json.RawMessage) error {
		return s.update(ctx, id, func(q *d.Project) error {
			rc, cc := callByID(q, runID, call.ID)
			if cc == nil || rc.Status != "running" || q.LatestRun().ID != runID || q.ScriptSource.Version != rc.SourceVersion {
				return fault.ErrCancelled
			}
			cc.Request = append(json.RawMessage(nil), raw...)
			return nil
		})
	})
	if response.Usage.Known {
		if _, ok := pricing.CheckedCost(response.Usage); !ok {
			response.Usage.Known = false
			callErr = fault.New("MODEL_USAGE_INVALID", "供应商用量无法可靠计价，保留本次预留")
		}
	}
	// 取消后仍保存完整原答与用量；采用业务结果前必须成功结算到原调用。
	saveCtx, cancel := s.receiptContext(ctx)
	defer cancel()
	err = s.update(saveCtx, id, func(q *d.Project) error {
		_, cc := callByID(q, runID, call.ID)
		if cc == nil {
			return fault.ErrNotFound
		}
		cc.Text, cc.Reasoning, cc.Response, cc.FinishReason, cc.Usage = response.Text, response.Reasoning, string(response.Raw), response.FinishReason, response.Usage
		ended := time.Now().UTC()
		cc.CompletedAt = &ended
		if metadata, ok := bound.Model.(ports.ResponseMetadata); ok {
			cc.RequestID = metadata.ResponseRequestID(response.Raw)
		}
		cc.ProviderState = response.ProviderState
		cc.Actions = response.Actions
		if response.Usage.Known {
			cc.CostMicroCNY = pricing.Cost(response.Usage)
		}
		if callErr != nil {
			// 运输失败时丢弃全部可执行输出，即使适配器已解析出部分字段。
			// 原始响应仍作为证据保存，不能采用部分目录或工具动作。
			cc.Text, cc.Reasoning, cc.FinishReason, cc.Actions = "", "", "", nil
			cc.ProviderState = ""
			cc.Status, cc.ErrorCode, cc.Error = "unknown", "MODEL_PROVIDER_FAILED", callErr.Error()
			var interrupted interface{ ResponseInterrupted() bool }
			if errors.As(callErr, &interrupted) && interrupted.ResponseInterrupted() {
				cc.ErrorCode = interruptionCode
			}
			var unsent interface{ RequestNotSent() bool }
			if errors.As(callErr, &unsent) && unsent.RequestNotSent() {
				cc.NotSent = true
				cc.Status, cc.Usage.Known, cc.CostMicroCNY = "rejected", true, 0
			}
		} else {
			cc.Status = "received"
		}
		return nil
	})
	if err != nil {
		return ports.Response{}, err
	}
	if callErr != nil {
		var interrupted interface{ ResponseInterrupted() bool }
		if errors.As(callErr, &interrupted) && interrupted.ResponseInterrupted() {
			return ports.Response{}, callErr // 中断回执提交成功后才允许限次重试
		}
		if err = s.fail(saveCtx, id, runID, "MODEL_PROVIDER_FAILED", callErr.Error()); err != nil {
			return ports.Response{}, err
		}
		return ports.Response{}, callErr
	}
	if !validTaskActions(r.ExecutionContract(), response.FinishReason, response.Actions) {
		if err = s.rejectResponse(saveCtx, id, runID, call.ID); err != nil {
			return ports.Response{}, err
		}
		return ports.Response{}, fault.ErrInvalid
	}
	return response, nil
}
