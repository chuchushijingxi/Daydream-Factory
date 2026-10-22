// Package spaces owns space identity, team roles and resource access rules.
package spaces

import "time"

type Kind string

const (
	Personal Kind = "personal"
	TeamKind Kind = "team"
)

type Role string

const (
	Owner  Role = "owner"
	Admin  Role = "admin"
	Member Role = "member"
)

type Policy string

const (
	Isolated Policy = "isolated"
	ReadAll  Policy = "read_all"
	EditAll  Policy = "edit_all"
)

type Status string

const (
	Active   Status = "active"
	Expired  Status = "expired"
	Disabled Status = "disabled"
)

type Access int

const (
	None Access = iota
	Read
	Edit
)

const (
	ProjectResource      = "project"
	CanvasResource       = "canvas"
	UploadResource       = "upload"
	MediaResource        = "media"
	AgentSessionResource = "agent_session"
)

type Team struct {
	ID          string
	Name        string
	OwnerID     string
	MemberLimit int
	ExpiresAt   time.Time
	Disabled    bool
	GrantAmount int64
	Policy      Policy
	OpenedAt    time.Time
	Revision    uint64
}

// Scope 的成员身份由应用端口核验；请求头里的空间 ID 本身不构成 Scope。
type Scope struct {
	SpaceID  string
	Kind     Kind
	UserID   string
	Role     Role
	Policy   Policy
	Status   Status
	ReadOnly bool
}

type Resource struct {
	SpaceID string
	OwnerID string
	Kind    string
	ID      string
}

type Share struct {
	Grantee    string `json:"grantee"`
	Permission Access `json:"permission"`
}

func (t Team) Status(at time.Time) Status {
	if t.Disabled {
		return Disabled
	}
	if !at.Before(t.ExpiresAt) {
		return Expired
	}
	return Active
}

// 到期后仍可用完已发积分；只读判断使用账户总余额，包含成员名下未用额。
func (t Team) ReadOnly(at time.Time, balance int64) bool {
	return t.Status(at) == Expired && balance == 0
}

func (r Role) Valid() bool { return r == Owner || r == Admin || r == Member }

// Decide 只消费已核验的空间身份和资源事实，不访问存储或判断平台运营角色。
func Decide(s Scope, r Resource, shares []Share) Access {
	if s.SpaceID == "" || s.UserID == "" || r.OwnerID == "" || r.SpaceID != s.SpaceID {
		return None
	}
	var access Access
	switch s.Kind {
	case Personal:
		if r.OwnerID == s.UserID {
			access = Edit
		}
	case TeamKind:
		if !s.Role.Valid() || s.Status == Disabled {
			return None
		}
		if s.Role == Owner || s.Role == Admin || r.OwnerID == s.UserID {
			access = Edit
		} else if r.Kind != AgentSessionResource {
			switch s.Policy {
			case ReadAll:
				access = Read
			case EditAll:
				access = Edit
			}
			if r.Kind == ProjectResource || r.Kind == CanvasResource {
				for _, share := range shares {
					if (share.Grantee == s.UserID || share.Grantee == "*") && (share.Permission == Read || share.Permission == Edit) {
						access = max(access, share.Permission)
					}
				}
			}
		}
	}
	if s.ReadOnly {
		access = min(access, Read)
	}
	return access
}
