package project

import (
	"context"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func (s *Service) Cancel(ctx context.Context, id string, revision uint64) (d.Project, error) {
	p, err := s.editingProject(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Revision != revision {
		return p, fault.ErrConflict
	}
	if err = p.LatestRun().Cancel(); err != nil {
		return p, err
	}
	return s.commit(ctx, p, revision)
}
