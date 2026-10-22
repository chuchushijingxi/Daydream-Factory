package spaces

import d "drama-agent/internal/domain/spaces"

type ShareInput struct {
	Grantee    string   `json:"grantee"`
	Permission d.Access `json:"permission"`
}
type SharingView struct {
	SpaceID      string    `json:"spaceId"`
	ResourceKind string    `json:"resourceKind"`
	ResourceID   string    `json:"resourceId"`
	Shares       []d.Share `json:"shares"`
	CanManage    bool      `json:"canManage"`
}
