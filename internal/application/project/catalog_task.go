package project

import (
	"context"
	"time"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

func (s *Service) Start(ctx context.Context, id string, revision uint64) (d.Project, error) {
	release, err := s.configurationGuard(ctx)
	if err != nil {
		return d.Project{}, err
	}
	defer release.Release()
	ctx = release.Context
	p, err := s.Project(ctx, id)
	if err != nil {
		return p, err
	}
	if err = commandBoundary(&p, revision); err != nil {
		return p, err
	}
	if p.Storyboard != nil && !p.IndexReady() {
		return p, fault.New("EPISODE_INDEX_REQUIRED", "解析中的分集识别尚未完成")
	}
	ready := p.CatalogReady()
	old := p.LatestCatalogRun()
	if p.CatalogEditedByUser && !ready {
		return p, fault.New("CATALOG_USER_EDITED", "用户管理的目录不能被重新提取覆盖")
	}
	if failed := p.FailedTask(); failed != nil && failed.Contract != d.CatalogExtractionContract && failed.Contract != d.HistoricalCatalogContract {
		return p, fault.New("PROJECT_FAILED", failed.Error)
	}
	if old != nil {
		for _, call := range old.Calls {
			if call.Status == "pending" || call.Status == "unknown" && !interruptedCall(call) {
				return p, fault.New("MODEL_RESULT_UNKNOWN", "已有请求结果未知，保留证据且不重新提交")
			}
		}
	}
	run := d.Run{ID: identity.New("run"), Contract: d.CatalogExtractionContract, SourceVersion: p.ScriptSource.Version, Status: "running", Stage: "extract", InputRevision: revision, Calls: []d.Call{}, CreatedAt: time.Now().UTC()}
	if ready {
		if len(p.Assets) == 0 {
			return p, fault.New("ASSETS_NOT_READY", d.EmptyVisualError)
		}
		run.Stage = "visualPrompt"
		run.Targets = p.PendingVisualTargets()
		if len(run.Targets) == 0 {
			return p, fault.New("ASSETS_UP_TO_DATE", "当前资产及视觉提示词已齐全，可在详情中审核、编辑或单项重新生成")
		}
	} else if old != nil && old.Contract == d.CatalogExtractionContract && (old.Status == "failed" || old.Status == "cancelled") {
		run.Stage, run.Cursor = old.Stage, old.Cursor
	}
	if err = s.prepareRun(ctx, p, &run); err != nil {
		return p, err
	}
	if old == nil {
		p.Assets = []d.Asset{}
		p.VisualPrompts = []d.VisualPrompt{}
	}
	p.Runs = append(p.Runs, run)
	return s.commit(ctx, p, revision)
}
