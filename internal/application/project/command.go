package project

import (
	"context"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func commandBoundary(p *d.Project, revision uint64) error {
	if p.Revision != revision {
		return fault.ErrConflict
	}
	if p.Active() {
		return fault.New("RUN_ACTIVE", "任务运行中")
	}
	return nil
}

func (s *Service) command(ctx context.Context, id string, revision uint64, change func(*d.Project) error) (d.Project, error) {
	p, err := s.Project(ctx, id)
	if err != nil {
		return p, err
	}
	if err = commandBoundary(&p, revision); err != nil {
		return p, err
	}
	if err = change(&p); err != nil {
		return p, err
	}
	return s.commit(ctx, p, revision)
}
