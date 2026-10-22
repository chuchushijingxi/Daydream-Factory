package project

import (
	"context"
	"time"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

func (s *Service) StartOutline(ctx context.Context, id string, revision uint64) (d.Project, error) {
	return s.startTask(ctx, id, revision, d.OutlineContract, nil)
}

func (s *Service) StartVisual(ctx context.Context, id string, revision uint64) (d.Project, error) {
	return s.startTask(ctx, id, revision, d.VisualContract, nil)
}

func (s *Service) StartSingleVisual(ctx context.Context, id string, revision uint64, target d.VisualTarget) (d.Project, error) {
	return s.startTask(ctx, id, revision, d.VisualContract, &target)
}

// singleVisualTargets 冻结单项视觉的目标集合：附加形象的基础正文失效时先补基础目标，再补目标本身。
// 报价预览与实际启动共用同一构造；两处各写一份时，漂移会在启动摘要比对处变成用户可见的报价失败。
func singleVisualTargets(p *d.Project, target d.VisualTarget) ([]d.VisualTarget, error) {
	if err := p.ValidateTarget(target); err != nil {
		return nil, err
	}
	targets := []d.VisualTarget{}
	if target.LookID != "" {
		base := d.VisualTarget{AssetID: target.AssetID}
		if !p.ValidPrompt(p.Prompt(base), "") {
			targets = append(targets, base)
		}
	}
	return append(targets, target), nil
}

func (s *Service) startTask(ctx context.Context, id string, revision uint64, contract string, target *d.VisualTarget) (d.Project, error) {
	release, err := s.configurationGuard(ctx)
	if err != nil {
		return d.Project{}, err
	}
	defer release.Release()
	ctx = release.Context
	p, err := s.editingProject(ctx, id)
	if err != nil {
		return p, err
	}
	if err = commandBoundary(&p, revision); err != nil {
		return p, err
	}
	if contract == d.OutlineContract && p.LatestRunByContract(contract) != nil {
		return p, fault.New("OUTLINE_ALREADY_STARTED", "该任务仅允许首次启动")
	}
	if contract == d.VisualContract && target == nil {
		if failed := p.FailedTask(); failed != nil && failed.Contract == d.VisualContract && len(failed.Targets) == 0 {
			return p, fault.New("VISUAL_PROMPTS_ALREADY_STARTED", "空目录视觉失败已保存，不创建第二项任务")
		}
	}
	if err = p.EnsureNotFailed(); err != nil {
		return p, err
	}
	if p.HasCancelledTask() {
		return p, fault.New("ASSETS_NOT_READY", "任务已取消，不开放后续生成")
	}
	stage := "outline"
	targets := []d.VisualTarget{}
	if contract == d.VisualContract {
		stage = "visualPrompt"
		if !p.CatalogReady() {
			return p, fault.New("ASSETS_NOT_READY", "资产提取尚未完成")
		}
		if target == nil {
			targets = p.PendingVisualTargets()
			if len(targets) == 0 && len(p.Assets) > 0 {
				return p, fault.New("VISUAL_PROMPTS_UP_TO_DATE", "当前资产均已有有效视觉提示词，可在详情中单项重新生成")
			}
		} else {
			joined, err := singleVisualTargets(&p, *target)
			if err != nil {
				return p, err
			}
			targets = joined
		}
	}
	run := d.Run{ID: identity.New("run"), Contract: contract, SourceVersion: p.ScriptSource.Version, InputRevision: revision, Status: "running", Stage: stage, Calls: []d.Call{}, Targets: targets, Target: target, CreatedAt: time.Now().UTC()}
	if contract == d.VisualContract && len(p.Assets) == 0 {
		run.Status, run.ErrorCode, run.Error = "failed", "INVALID_INPUT", d.EmptyVisualError
		ended := time.Now().UTC()
		run.EndedAt = &ended
		p.Runs = append(p.Runs, run)
		return s.commit(ctx, p, revision)
	}
	if err = s.prepareRun(ctx, p, &run); err != nil {
		return p, err
	}
	p.Runs = append(p.Runs, run)
	return s.commit(ctx, p, revision)
}

func (s *Service) prepareRun(ctx context.Context, p d.Project, r *d.Run) error {
	bound, err := s.resolve(ctx, s.modelID(p))
	if err != nil {
		return err
	}
	r.Model = bound.ID
	if bound.Model == nil || s.Agent == nil {
		return fault.ErrModelUnavailable
	}
	budget, err := s.permit(ctx, p, *r)
	if err != nil {
		return err
	}
	if err = s.freezeCredit(ctx, p, r); err != nil {
		return err
	}
	r.BudgetMicroCNY = budget
	if budget > 0 && p.LimitMicroCNY > 0 && p.UsedMicroCNY() >= p.LimitMicroCNY {
		return fault.ErrBudget
	}
	return nil
}
