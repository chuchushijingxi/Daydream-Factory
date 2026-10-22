package project

import (
	"context"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func (s *Service) SaveStyle(ctx context.Context, id string, revision uint64, styleID, version string) (d.Project, error) {
	p, err := s.editingProject(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Revision != revision {
		return p, fault.ErrConflict
	}
	if p.Style != nil {
		if p.Style.ID == styleID && p.Style.PromptVersion == version {
			return p, nil
		}
		return p, fault.New("STYLE_LOCKED", "项目风格保存后固定")
	}
	if p.Active() {
		return p, fault.New("RUN_ACTIVE", "任务运行中")
	}
	for _, style := range s.Styles {
		if style.ID == styleID {
			if style.PromptVersion != version {
				return p, fault.New("STYLE_VERSION_CONFLICT", "风格库版本已变化")
			}
			snapshot := style.ProjectStyle
			p.Style = &snapshot
			return s.commit(ctx, p, revision)
		}
	}
	return p, fault.ErrInvalid
}
