package project

import (
	"context"

	d "drama-agent/internal/domain/project"
)

func (s *Service) SaveLook(ctx context.Context, id string, revision uint64, assetID, lookID string, v d.LookInput) (d.Project, error) {
	return s.command(ctx, id, revision, func(p *d.Project) error { return p.UpdateLook(assetID, lookID, v) })
}

func (s *Service) DeleteLook(ctx context.Context, id string, revision uint64, assetID, lookID string) (d.Project, error) {
	return s.command(ctx, id, revision, func(p *d.Project) error { return p.RemoveLook(assetID, lookID) })
}
