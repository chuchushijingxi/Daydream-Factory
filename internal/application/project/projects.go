package project

import (
	"context"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// Project 只公开仍可见的项目；仓储快照保留删除后的原始记录，
// 供已取消请求的迟到响应定位原任务并结算费用。
func (s *Service) Project(ctx context.Context, id string) (d.Project, error) {
	p, err := s.Store.Snapshot(ctx, id)
	if err == nil && p.DeletedAt != nil {
		err = fault.ErrNotFound
	}
	return p, err
}

// ReadProject 仅用于当前读视图；内存仓储可直接提供完整独立快照。
func (s *Service) ReadProject(ctx context.Context, id string) (d.Project, error) {
	if reader, ok := s.Store.(ports.ProjectReader); ok {
		p, err := reader.ReadProject(ctx, id)
		if err == nil && p.DeletedAt != nil {
			err = fault.ErrNotFound
		}
		return p, err
	}
	return s.Project(ctx, id)
}

// 删除只移除可见性，保留原文、调用账目和未知预留。
// CAS 拒绝并发启动或修改后的陈旧删除请求。
func (s *Service) DeleteProject(ctx context.Context, id string, revision uint64) (d.Project, error) {
	p, err := s.Project(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Revision != revision {
		return p, fault.ErrConflict
	}
	if p.Active() {
		return p, fault.New("RUN_ACTIVE", "任务运行中，不能删除项目")
	}
	now := time.Now().UTC()
	p.DeletedAt = &now
	return s.commit(ctx, p, revision)
}

// ListProjects 通过摘要查询浏览项目，不展开原文或调用历史。
func (s *Service) ListProjects(ctx context.Context) ([]ports.ProjectSummary, error) {
	return s.Store.ListSummaries(ctx)
}
