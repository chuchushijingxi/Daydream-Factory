package project

import (
	"context"

	d "drama-agent/internal/domain/project"
)

func (s *Service) SaveAsset(ctx context.Context, id string, revision uint64, assetID string, v d.AssetInput) (d.Project, error) {
	return s.command(ctx, id, revision, func(p *d.Project) error { return p.UpdateAsset(assetID, v) })
}

func (s *Service) DeleteAsset(ctx context.Context, id string, revision uint64, assetID string) (d.Project, error) {
	return s.command(ctx, id, revision, func(p *d.Project) error { return p.RemoveAsset(assetID) })
}

func (s *Service) SaveVisualPrompt(ctx context.Context, id string, revision uint64, target d.VisualTarget, text string) (d.Project, error) {
	return s.command(ctx, id, revision, func(p *d.Project) error { return p.EditVisualPrompt(target, text) })
}
