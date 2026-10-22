package project

import (
	"context"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
)

type AccessView struct {
	SpaceID   string        `json:"spaceId"`
	OwnerID   string        `json:"ownerId"`
	CreatorID string        `json:"creatorId"`
	Access    spaces.Access `json:"access"`
	CanShare  bool          `json:"canShare"`
}

// AccessInfo只投影当前资源权限，不污染领域payload或跨用户进度缓存。
func (s *Service) AccessInfo(ctx context.Context, id string) (AccessView, error) {
	p, err := s.ReadProject(ctx, id)
	if err != nil {
		return AccessView{}, err
	}
	if s.scope == nil || s.Access == nil {
		return AccessView{}, fault.New("AUTH_REQUIRED", "缺少当前用户空间权限")
	}
	resource := spaces.Resource{SpaceID: p.SpaceID, OwnerID: p.OwnerID, Kind: spaces.ProjectResource, ID: p.ID}
	permission, scope, err := s.Access.Access(ports.WithoutSpaceRequest(ctx), ports.Actor{UserID: s.scope.UserID, Role: ports.RoleUser}, resource)
	if err != nil {
		return AccessView{}, err
	}
	if permission == spaces.None {
		return AccessView{}, fault.ErrNotFound
	}
	canShare := spaces.CanManageSharing(scope, resource)
	return AccessView{SpaceID: p.SpaceID, OwnerID: p.OwnerID, CreatorID: p.CreatorID, Access: permission, CanShare: canShare}, nil
}
