package spaces

import (
	"context"
	"errors"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
	d "drama-agent/internal/domain/spaces"
)

type rechargeSpacesStore struct {
	Store
	rows []SpaceSummary
}

func (s rechargeSpacesStore) MySpaces(context.Context, string) ([]SpaceSummary, error) {
	return s.rows, nil
}

func TestRechargeTargetsOnlyOfferPersonalAndActiveOwnedTeams(t *testing.T) {
	service := &Service{Store: rechargeSpacesStore{rows: []SpaceSummary{
		{ID: "u1", Kind: d.Personal, Role: d.Owner, Status: d.Active},
		{ID: "team_owned", Kind: d.TeamKind, Name: "自有团队", Role: d.Owner, Status: d.Active},
		{ID: "team_admin", Kind: d.TeamKind, Name: "管理团队", Role: d.Admin, Status: d.Active},
		{ID: "team_member", Kind: d.TeamKind, Name: "成员团队", Role: d.Member, Status: d.Active},
		{ID: "team_expired", Kind: d.TeamKind, Name: "到期团队", Role: d.Owner, Status: d.Expired},
		{ID: "team_disabled", Kind: d.TeamKind, Name: "停用团队", Role: d.Owner, Status: d.Disabled},
	}}}
	targets := service.RechargeTargets()
	actor := ports.Actor{UserID: "u1", Role: ports.RoleUser}
	list, err := targets.List(context.Background(), actor)
	if err != nil || len(list) != 2 || list[0] != (p.Target{Kind: "personal", ID: "u1", Name: "个人空间"}) || list[1] != (p.Target{Kind: "team", ID: "team_owned", Name: "自有团队"}) {
		t.Fatal(list, err)
	}
	resolved, err := targets.Resolve(context.Background(), actor, p.Target{Kind: "team", ID: "team_owned"})
	if err != nil || resolved.AccountID != "team_owned" || resolved.Target.Name != "自有团队" {
		t.Fatal(resolved, err)
	}
	resolved, err = targets.Resolve(context.Background(), actor, p.Target{Kind: "personal", ID: "u1"})
	if err != nil || resolved.AccountID != "u1" {
		t.Fatal(resolved, err)
	}
	for id, code := range map[string]string{
		"team_admin": "PAYMENT_TARGET_FORBIDDEN", "team_member": "PAYMENT_TARGET_FORBIDDEN", "team_unknown": "PAYMENT_TARGET_FORBIDDEN",
		"team_expired": "PAYMENT_TARGET_INACTIVE", "team_disabled": "PAYMENT_TARGET_INACTIVE",
	} {
		_, err = targets.Resolve(context.Background(), actor, p.Target{Kind: "team", ID: id})
		var f *fault.Error
		if !errors.As(err, &f) || f.Code != code {
			t.Fatal(id, err)
		}
	}
	if _, err = targets.Resolve(context.Background(), actor, p.Target{Kind: "personal", ID: "u2"}); err == nil {
		t.Fatal("other personal space accepted")
	}
}
