package project

import (
	"context"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func (s *Service) SaveSettings(ctx context.Context, id string, revision uint64, v d.ProjectSettings) (d.Project, error) {
	p, err := s.editingProject(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Revision != revision {
		return p, fault.ErrConflict
	}
	if p.ProjectSettings != nil {
		if *p.ProjectSettings == v {
			return p, nil
		}
		if !canCompleteSettings(*p.ProjectSettings, v) {
			return p, fault.New("SETTINGS_LOCKED", "已确认的项目参数固定，未选择项可以补齐")
		}
	}
	if p.Active() {
		return p, fault.New("RUN_ACTIVE", "任务运行中")
	}
	release, err := s.configurationGuard(ctx)
	if err != nil {
		return p, err
	}
	defer release.Release()
	ctx = release.Context
	if err = s.validateSettings(ctx, v, p.ProjectSettings); err != nil {
		return p, err
	}
	p.ProjectSettings = &v
	return s.commit(ctx, p, revision)
}

// 首次选择固定；补齐空字段不能重选其他已经确认的值。
func canCompleteSettings(old, next d.ProjectSettings) bool {
	for _, pair := range [][2]string{{old.AspectRatio, next.AspectRatio}, {old.LLMModel, next.LLMModel}, {old.ImageModel, next.ImageModel}, {old.VideoModel, next.VideoModel}} {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
	}
	return true
}

func (s *Service) validateSettings(ctx context.Context, v d.ProjectSettings, existing *d.ProjectSettings) error {
	switch v.AspectRatio {
	case "", "16:9", "9:16", "1:1", "4:3":
	default:
		return fault.ErrInvalid
	}
	opts, err := s.AvailableModels(ctx)
	if err != nil {
		return err
	}
	old := d.ProjectSettings{}
	if existing != nil {
		old = *existing
	}
	for _, pair := range []struct {
		id, previous string
		options      []ports.ModelOption
	}{
		{v.LLMModel, old.LLMModel, opts.Analysis}, {v.ImageModel, old.ImageModel, opts.Image}, {v.VideoModel, old.VideoModel, opts.Video},
	} {
		if pair.id == "" || pair.id == pair.previous {
			continue
		}
		found := false
		for _, o := range pair.options {
			found = found || o.ID == pair.id
		}
		if !found {
			for _, known := range opts.Catalog {
				if known.ID == pair.id {
					return fault.ErrModelUnavailable
				}
			}
			return fault.ErrInvalid
		}
	}
	if v.LLMModel != "" && v.LLMModel != old.LLMModel {
		bound, err := s.resolve(ctx, v.LLMModel)
		if err != nil {
			return err
		}
		if bound.Model == nil {
			return fault.ErrModelUnavailable
		}
	}
	return nil
}
