package project

import (
	"context"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

type resolvedModelKey struct{}

// 执行上下文固定本任务的模型与价格，不复制 Service，也不共享任务间的可变绑定。
func (s *Service) resolve(ctx context.Context, id string) (ports.ResolvedModel, error) {
	if bound, ok := ctx.Value(resolvedModelKey{}).(ports.ResolvedModel); ok && (id == "" || bound.ID == id) {
		return bound, nil
	}
	if s.Models == nil {
		return ports.ResolvedModel{}, fault.ErrModelUnavailable
	}
	return s.Models.Resolve(ctx, id)
}

func (s *Service) configurationGuard(ctx context.Context) (ports.Lease, error) {
	if s.Models == nil {
		return ports.Lease{Context: ctx, Release: func() {}}, nil
	}
	return s.Models.Acquire(ctx)
}

func (s *Service) AvailableModels(ctx context.Context) (ports.ModelOptions, error) {
	if s.Models == nil {
		return ports.ModelOptions{Analysis: []ports.ModelOption{}, Image: []ports.ModelOption{}, Video: []ports.ModelOption{}}, nil
	}
	return s.Models.Options(ctx)
}

func (s *Service) modelID(p d.Project) string {
	if p.ProjectSettings != nil {
		return p.ProjectSettings.LLMModel
	}
	return ""
}
