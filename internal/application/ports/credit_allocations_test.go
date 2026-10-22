package ports_test

import (
	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/spaces"
	"testing"
)

func TestChargeAccountFreezesAccountHolderAndActorFromSpace(t *testing.T) {
	for _, test := range []struct {
		kind            d.Kind
		role            d.Role
		account, holder string
	}{{d.Personal, d.Owner, "user", ""}, {d.TeamKind, d.Owner, "team", ""}, {d.TeamKind, d.Admin, "team", "user"}, {d.TeamKind, d.Member, "team", "user"}} {
		scope := ports.SpaceScope{SpaceID: test.account, UserID: "user", Kind: test.kind, Role: test.role, Status: d.Active}
		got, err := ports.ChargeAccount(scope)
		if err != nil || got.AccountID != test.account || got.HolderID != test.holder || got.ActorID != "user" {
			t.Fatal(test, got, err)
		}
	}
	scope := ports.SpaceScope{SpaceID: "team", UserID: "user", Kind: d.TeamKind, Role: d.Member, Status: d.Disabled}
	if _, err := ports.ChargeAccount(scope); err == nil {
		t.Fatal("disabled scope selected a payer")
	}
	scope.Status = d.Expired
	scope.ReadOnly = true
	if _, err := ports.ChargeAccount(scope); err == nil {
		t.Fatal("read-only scope selected a payer")
	}
	scope.ReadOnly = false
	if _, err := ports.ChargeAccount(scope); err != nil {
		t.Fatal("expired scope with balance rejected", err)
	}
}
