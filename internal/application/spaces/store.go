package spaces

import (
	"context"
	"time"

	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	n "drama-agent/internal/domain/notifications"
	d "drama-agent/internal/domain/spaces"
)

// SpaceTx 锁定团队行后执行；事实、审计和系统消息共享同一 PG 提交边界。
// 开通时团队行尚不存在，创建空间、团队和 owner 成员也必须同事务提交。
type Store interface {
	SpaceTx(context.Context, string, func(Tx) error) error
	MySpaces(context.Context, string) ([]SpaceSummary, error)
	TeamView(context.Context, string) (TeamView, error)
	TeamList(context.Context, string, int, int) (ports.Page[TeamView], error)
	SpaceMembers(context.Context, string) ([]Member, error)
	SpaceInvites(context.Context, string) ([]Invite, error)
	PendingInvites(context.Context, string) ([]Invite, error)
	SpaceInvite(context.Context, string) (Invite, error)
	InviteByDigest(context.Context, string) (Invite, error)
	SpaceJoinRequests(context.Context, string) ([]JoinRequest, error)
	SpaceAudit(context.Context, string, int64, int) (AuditPage, error)
	SpaceOverview(context.Context, string, string, time.Time) (Overview, error)
	LookupTeam(context.Context, string) (string, error)
	DueTeamReminders(context.Context, time.Time, int) ([]d.Team, error)
	// 每个候选团队单独持行锁并以SKIP LOCKED领取，回调和调度推进同事务。
	RunDueTeamGrants(context.Context, time.Time, int, func(Tx) error) (int, error)
	// 每项独立事务并重新核验管理员、接收成员和原负责人；逐项审计，保留 creator。
	TransferSpaceContent(context.Context, string, string, TransferInput) ([]TransferReceipt, error)
}

type Tx interface {
	// SharingResource locks the real resource in this team; unsupported canvas is NOT_FOUND.
	SharingResource(context.Context, string, string) (d.Resource, error)
	ResourceShares(context.Context, string, string) ([]d.Share, error)
	SaveResourceShare(context.Context, string, string, d.Share, string) error
	DeleteResourceShare(context.Context, string, string, string) error

	EnsureCreditAccount(context.Context, string) error
	AddCredits(context.Context, c.Change) (c.Entry, error)
	CreditBalances(context.Context, string) (c.Balances, error)
	HolderCredits(context.Context, string, string) (c.HolderAllocation, error)
	AllocateCredits(context.Context, c.AllocationChange) (c.AllocationEvent, error)
	ReclaimCredits(context.Context, c.AllocationChange) (c.AllocationEvent, error)
	ReleaseCredits(context.Context, c.AllocationChange) (c.AllocationEvent, error)
	GrantSchedule(context.Context) (d.GrantSchedule, error)
	SaveGrantSchedule(context.Context, d.GrantSchedule) error
	Team(context.Context) (d.Team, error)
	// CreateTeam 同时创建 space、team、owner 成员和模型折扣。
	CreateTeam(context.Context, d.Team, []ModelDiscount) error
	SaveTeam(context.Context, d.Team, uint64) error
	SetTeamDiscounts(context.Context, []ModelDiscount) error
	Member(context.Context, string) (Member, error)
	Members(context.Context) ([]Member, error)
	MemberCount(context.Context) (int, error)
	AddMember(context.Context, string, d.Role, time.Time) error
	RemoveMember(context.Context, string) error
	SetMemberRole(context.Context, string, d.Role) error
	FindUser(context.Context, string) (User, error)
	Invite(context.Context, string) (Invite, error)
	SaveInvite(context.Context, Invite) error
	JoinRequest(context.Context, string) (JoinRequest, error)
	SaveJoinRequest(context.Context, JoinRequest) error
	OwnershipTransfer(context.Context, string) (OwnershipTransfer, error)
	PendingOwnershipTransfers(context.Context) ([]OwnershipTransfer, error)
	SaveOwnershipTransfer(context.Context, OwnershipTransfer) error
	AppendAudit(context.Context, AuditEvent) error
	SystemNotify(context.Context, n.SystemInput) error
	RecordReminder(context.Context, string, string) (bool, error)
}
