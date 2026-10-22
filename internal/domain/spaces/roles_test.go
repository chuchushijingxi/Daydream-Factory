package spaces_test

import (
	"testing"

	"drama-agent/internal/domain/spaces"
)

func TestRoleActions(t *testing.T) {
	for _, test := range []struct {
		action               spaces.Action
		owner, admin, member bool
	}{
		{spaces.Recharge, true, false, false},
		{spaces.ManageRoles, true, false, false},
		{spaces.TransferOwnership, true, false, false},
		{spaces.InviteMembers, true, true, false},
		{spaces.RemoveMembers, true, true, false},
		{spaces.DecideJoinRequest, true, true, false},
		{spaces.AllocateCredits, true, true, false},
		{spaces.ReclaimCredits, true, true, false},
		{spaces.ManageSettings, true, true, false},
		{spaces.TransferContent, true, true, false},
		{spaces.ViewOverview, true, true, false},
		{spaces.ViewAudit, true, true, false},
		{spaces.ViewMembers, true, true, true},
		{spaces.LeaveTeam, false, true, true},
		{"unknown", false, false, false},
	} {
		for role, want := range map[spaces.Role]bool{spaces.Owner: test.owner, spaces.Admin: test.admin, spaces.Member: test.member, "operator": false} {
			if got := role.Can(test.action); got != want {
				t.Errorf("%s/%s = %t, want %t", role, test.action, got, want)
			}
		}
	}
}

func TestRemoveRequiresOrdinaryMember(t *testing.T) {
	for _, actor := range []spaces.Role{spaces.Owner, spaces.Admin, spaces.Member, "operator"} {
		for _, target := range []spaces.Role{spaces.Owner, spaces.Admin, spaces.Member, ""} {
			want := (actor == spaces.Owner || actor == spaces.Admin) && target == spaces.Member
			if got := actor.CanRemove(target); got != want {
				t.Errorf("remove %s/%s = %t, want %t", actor, target, got, want)
			}
		}
	}
}
