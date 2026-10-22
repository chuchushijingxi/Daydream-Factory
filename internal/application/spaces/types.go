package spaces

import (
	"encoding/json"
	"time"

	d "drama-agent/internal/domain/spaces"
)

type ModelDiscount struct {
	ModelInstanceID string `json:"modelInstanceId"`
	Percent         int    `json:"percent"`
}
type TeamInput struct {
	OwnerID        string          `json:"ownerId"`
	Name           string          `json:"name"`
	MemberLimit    int             `json:"memberLimit"`
	ExpiresAt      time.Time       `json:"expiresAt"`
	GrantAmount    int64           `json:"grantAmount"`
	ModelDiscounts []ModelDiscount `json:"modelDiscounts"`
}
type TeamPatch struct {
	Revision       uint64           `json:"revision"`
	ExpiresAt      *time.Time       `json:"expiresAt,omitempty"`
	MemberLimit    *int             `json:"memberLimit,omitempty"`
	GrantAmount    *int64           `json:"grantAmount,omitempty"`
	OwnerID        *string          `json:"ownerId,omitempty"`
	Disabled       *bool            `json:"disabled,omitempty"`
	ModelDiscounts *[]ModelDiscount `json:"modelDiscounts,omitempty"`
}
type SettingsInput struct {
	Revision uint64   `json:"revision"`
	Name     string   `json:"name"`
	Policy   d.Policy `json:"policy"`
}
type SpaceSummary struct {
	ID          string     `json:"id"`
	Kind        d.Kind     `json:"kind"`
	Name        string     `json:"name"`
	Role        d.Role     `json:"role"`
	MemberCount int        `json:"memberCount"`
	MemberLimit int        `json:"memberLimit"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	Status      d.Status   `json:"status"`
	ReadOnly    bool       `json:"readOnly"`
}
type TeamView struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	OwnerID        string          `json:"ownerId"`
	OwnerName      string          `json:"ownerName"`
	OwnerEmail     string          `json:"ownerEmail,omitempty"`
	MemberCount    int             `json:"memberCount"`
	MemberLimit    int             `json:"memberLimit"`
	ExpiresAt      time.Time       `json:"expiresAt"`
	Disabled       bool            `json:"disabled"`
	GrantAmount    int64           `json:"grantAmount"`
	Policy         d.Policy        `json:"policy"`
	OpenedAt       time.Time       `json:"openedAt"`
	NextGrantAt    time.Time       `json:"nextGrantAt"`
	Revision       uint64          `json:"revision"`
	Status         d.Status        `json:"status"`
	Balance        int64           `json:"balance"`
	ModelDiscounts []ModelDiscount `json:"modelDiscounts"`
}
type Detail struct {
	SpaceSummary
	Team *TeamView `json:"team,omitempty"`
}
type User struct {
	ID, Name, Email string
	Enabled         bool
}
type Member struct {
	UserID   string     `json:"userId"`
	Name     string     `json:"name"`
	Email    string     `json:"email,omitempty"`
	Role     d.Role     `json:"role"`
	JoinedAt *time.Time `json:"joinedAt,omitempty"`
}
type Invite struct {
	ID            string     `json:"id"`
	SpaceID       string     `json:"spaceId"`
	TeamName      string     `json:"teamName"`
	Kind          string     `json:"kind"`
	TokenDigest   string     `json:"-"`
	InviteeUserID string     `json:"inviteeUserId,omitempty"`
	CreatedBy     string     `json:"createdBy"`
	CreatedAt     time.Time  `json:"createdAt"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	RevokedAt     *time.Time `json:"revokedAt,omitempty"`
	Status        string     `json:"status"`
}
type InviteLink struct {
	Invite
	Token string `json:"token"`
}
type InvitePreview struct {
	ID        string    `json:"id"`
	SpaceID   string    `json:"spaceId"`
	TeamName  string    `json:"teamName"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type JoinRequest struct {
	ID        string     `json:"id"`
	SpaceID   string     `json:"spaceId"`
	UserID    string     `json:"userId"`
	UserName  string     `json:"userName"`
	Message   string     `json:"message"`
	Status    string     `json:"status"`
	DecidedBy string     `json:"decidedBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	DecidedAt *time.Time `json:"decidedAt,omitempty"`
}
type OwnershipTransfer struct {
	ID          string     `json:"id"`
	SpaceID     string     `json:"spaceId"`
	FromOwnerID string     `json:"fromOwnerId"`
	ToUserID    string     `json:"toUserId"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	AcceptedAt  *time.Time `json:"acceptedAt,omitempty"`
}
type AuditEvent struct {
	ID        int64           `json:"id"`
	SpaceID   string          `json:"spaceId"`
	ActorID   string          `json:"actorId"`
	ActorKind string          `json:"actorKind"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"createdAt"`
}
type AuditPage struct {
	Items      []AuditEvent `json:"items"`
	NextCursor int64        `json:"nextCursor,omitempty"`
}
type DailyConsumption struct {
	Date    string `json:"date"`
	Credits int64  `json:"credits"`
}
type MemberStatistics struct {
	HolderBalance int64  `json:"holderBalance"`
	UserID        string `json:"userId"`
	Name          string `json:"name"`
	Credits       int64  `json:"credits"`
	Images        int64  `json:"images"`
	Videos        int64  `json:"videos"`
	Projects      int64  `json:"projects"`
}
type Overview struct {
	Team        TeamView           `json:"team"`
	Range       string             `json:"range"`
	Start       time.Time          `json:"start"`
	End         time.Time          `json:"end"`
	Allocated   int64              `json:"allocated"`
	Unallocated int64              `json:"unallocated"`
	Daily       []DailyConsumption `json:"daily"`
	Members     []MemberStatistics `json:"members"`
	Recent      []AuditEvent       `json:"recent"`
}
type TransferInput struct {
	FromUserID string        `json:"fromUserId"`
	ToUserID   string        `json:"toUserId"`
	All        bool          `json:"all"`
	Resources  []ResourceRef `json:"resources"`
}
type ResourceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type TransferReceipt struct {
	ResourceRef
	Transferred bool   `json:"transferred"`
	ErrorCode   string `json:"errorCode,omitempty"`
}
