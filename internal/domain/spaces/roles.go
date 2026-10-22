package spaces

type Action string

const (
	Recharge          Action = "recharge"
	ManageRoles       Action = "manage_roles"
	TransferOwnership Action = "transfer_ownership"
	InviteMembers     Action = "invite_members"
	RemoveMembers     Action = "remove_members"
	DecideJoinRequest Action = "decide_join_request"
	AllocateCredits   Action = "allocate_credits"
	ReclaimCredits    Action = "reclaim_credits"
	ManageSettings    Action = "manage_settings"
	TransferContent   Action = "transfer_content"
	ViewOverview      Action = "view_overview"
	ViewAudit         Action = "view_audit"
	ViewMembers       Action = "view_members"
	LeaveTeam         Action = "leave_team"
)

// Can 判断角色能力；到期、人数上限等用例前提由对应领域行为另行核验。
func (r Role) Can(action Action) bool {
	if !r.Valid() {
		return false
	}
	switch action {
	case Recharge, ManageRoles, TransferOwnership:
		return r == Owner
	case InviteMembers, RemoveMembers, DecideJoinRequest, AllocateCredits, ReclaimCredits,
		ManageSettings, TransferContent, ViewOverview, ViewAudit:
		return r == Owner || r == Admin
	case ViewMembers:
		return true
	case LeaveTeam:
		return r != Owner
	default:
		return false
	}
}

// 管理员需先被所有者降为成员才能移出，所有者本人需先转让所有权。
func (r Role) CanRemove(target Role) bool {
	return r.Can(RemoveMembers) && target == Member
}

// HoldsCredits 表示该角色只花本人名下的分配积分；所有者直接使用团队未分配积分。
func (r Role) HoldsCredits() bool {
	return r == Member || r == Admin
}

// CanAllocateTo 判断能否向目标分配或收回积分：管理者可管成员，只有所有者可管管理员。
func (r Role) CanAllocateTo(target Role) bool {
	if !r.Can(AllocateCredits) || !target.HoldsCredits() {
		return false
	}
	return target == Member || r == Owner
}
