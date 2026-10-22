package spaces

import (
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	d "drama-agent/internal/domain/spaces"
	"testing"
	"time"
)

func TestOpenCreatesAccountAndFirstGrantInSameTransaction(t *testing.T) {
	for _, amount := range []int64{0, 500} {
		ctx, s, store := fixture()
		store.state.team = d.Team{}
		store.state.members = map[string]Member{}
		store.state.allocations = map[string]c.HolderAllocation{}
		store.state.grantSchedule = d.GrantSchedule{}
		view, err := s.Open(ctx, ports.Actor{UserID: "operator", Role: ports.RoleOperator}, TeamInput{OwnerID: "owner", Name: "新团队", MemberLimit: 5, ExpiresAt: s.now().Add(180 * 24 * time.Hour), GrantAmount: amount})
		if err != nil {
			t.Fatal(err)
		}
		if store.state.wallet.Balance != amount || store.state.grantSchedule.Sequence != 1 || !store.state.grantSchedule.NextAt.Equal(s.now().Add(d.GrantInterval)) {
			t.Fatal(view, store.state.wallet, store.state.grantSchedule)
		}
		want := 0
		if amount > 0 {
			want = 1
		}
		if len(store.state.creditOperations) != want {
			t.Fatal("zero grant wrote a ledger entry or first grant missing", store.state.creditOperations)
		}
	}
}
func TestGrantScheduleAdvancesAndSameCycleDoesNotRepeat(t *testing.T) {
	ctx, s, store := fixture()
	opened := s.now()
	store.state.team.ExpiresAt = opened.Add(200 * 24 * time.Hour)
	store.state.team.GrantAmount = 100
	store.at = opened.Add(d.GrantInterval)
	if count, err := s.Schedule(ctx, store.at, 20); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if store.state.wallet.Balance != 100 || store.state.grantSchedule.Sequence != 2 {
		t.Fatal(store.state.wallet, store.state.grantSchedule)
	}
	if count, err := s.Schedule(ctx, store.at, 20); err != nil || count != 0 {
		t.Fatal("same cycle repeated", count, err)
	}
	store.state.team.ExpiresAt = store.at.Add(-24 * time.Hour)
	store.at = opened.Add(3 * d.GrantInterval)
	before := store.state.wallet.Balance
	if _, err := s.Schedule(ctx, store.at, 20); err != nil {
		t.Fatal(err)
	}
	if store.state.wallet.Balance != before {
		t.Fatal("expired contract granted credits")
	}
}
func TestExpiredRenewalGrantsOnceAndActiveRenewalKeepsCadence(t *testing.T) {
	ctx, s, store := fixture()
	at := s.now()
	store.state.team.ExpiresAt = at.Add(-24 * time.Hour)
	store.state.team.GrantAmount = 100
	store.state.grantSchedule = d.GrantSchedule{NextAt: at.Add(-40 * 24 * time.Hour), Sequence: 7}
	fundFixture(store, 50)
	expires := at.Add(90 * 24 * time.Hour)
	op := ports.Actor{UserID: "operator", Role: ports.RoleOperator}
	if _, err := s.OperatorUpdate(ctx, op, store.state.team.ID, TeamPatch{Revision: 1, ExpiresAt: &expires}); err != nil {
		t.Fatal(err)
	}
	if store.state.wallet.Balance != 150 || store.state.grantSchedule.Sequence != 8 || !store.state.grantSchedule.NextAt.Equal(at.Add(d.GrantInterval)) {
		t.Fatal(store.state.wallet, store.state.grantSchedule)
	}
	schedule := store.state.grantSchedule
	expires = at.Add(120 * 24 * time.Hour)
	if _, err := s.OperatorUpdate(ctx, op, store.state.team.ID, TeamPatch{Revision: 2, ExpiresAt: &expires}); err != nil {
		t.Fatal(err)
	}
	if store.state.wallet.Balance != 150 || store.state.grantSchedule != schedule {
		t.Fatal("early renewal changed cadence or granted twice")
	}
}
func TestEnableActiveContractSkipsDisabledCyclesWithoutGrantingOrResetting(t *testing.T) {
	ctx, s, store := fixture()
	at := s.now()
	store.state.team.Disabled = true
	store.state.team.ExpiresAt = at.Add(200 * 24 * time.Hour)
	store.state.team.GrantAmount = 100
	store.state.grantSchedule = d.GrantSchedule{NextAt: at.Add(-45 * 24 * time.Hour), Sequence: 4}
	fundFixture(store, 75)
	disabled := false
	if _, err := s.OperatorUpdate(ctx, ports.Actor{UserID: "operator", Role: ports.RoleOperator}, store.state.team.ID, TeamPatch{Revision: 1, Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if store.state.wallet.Balance != 75 || len(store.state.creditOperations) != 0 || store.state.grantSchedule.Sequence != 6 || !store.state.grantSchedule.NextAt.Equal(at.Add(15*24*time.Hour)) {
		t.Fatal(store.state.wallet, store.state.grantSchedule)
	}
	if count, err := s.Schedule(ctx, at, 20); err != nil || count != 0 {
		t.Fatal("enable backfilled disabled periods", count, err)
	}
	store.at = at.Add(15 * 24 * time.Hour)
	if count, err := s.Schedule(ctx, store.at, 20); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if store.state.wallet.Balance != 175 {
		t.Fatal("next original-cadence grant missing")
	}
}

func TestDisabledExpiredRenewalDefersGrantAndKeepsOriginalCadenceUntilEnable(t *testing.T) {
	ctx, s, store := fixture()
	at := s.now()
	store.state.team.Disabled = true
	store.state.team.ExpiresAt = at.Add(-24 * time.Hour)
	store.state.team.GrantAmount = 100
	original := d.GrantSchedule{NextAt: at.Add(-45 * 24 * time.Hour), Sequence: 4}
	store.state.grantSchedule = original
	fundFixture(store, 75)
	expires := at.Add(90 * 24 * time.Hour)
	op := ports.Actor{UserID: "operator", Role: ports.RoleOperator}
	if _, err := s.OperatorUpdate(ctx, op, store.state.team.ID, TeamPatch{Revision: 1, ExpiresAt: &expires}); err != nil {
		t.Fatal(err)
	}
	if !store.state.team.Disabled || store.state.wallet.Balance != 75 || len(store.state.creditOperations) != 0 || store.state.grantSchedule != original {
		t.Fatal("renewing a disabled contract granted or reset its schedule")
	}
	if _, err := s.Schedule(ctx, at, 20); err != nil {
		t.Fatal(err)
	}
	if store.state.wallet.Balance != 75 {
		t.Fatal("disabled renewed contract was scheduled")
	}
	enabled := false
	if _, err := s.OperatorUpdate(ctx, op, store.state.team.ID, TeamPatch{Revision: 2, Disabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	if store.state.wallet.Balance != 75 || store.state.grantSchedule.Sequence != 6 || !store.state.grantSchedule.NextAt.Equal(at.Add(15*24*time.Hour)) {
		t.Fatal("later enable backfilled or reset cadence", store.state.grantSchedule)
	}
}
func TestRenewAndEnableSamePatchGrantsOnlyOneNewPeriod(t *testing.T) {
	ctx, s, store := fixture()
	at := s.now()
	store.state.team.Disabled = true
	store.state.team.ExpiresAt = at.Add(-24 * time.Hour)
	store.state.team.GrantAmount = 100
	store.state.grantSchedule = d.GrantSchedule{NextAt: at.Add(-45 * 24 * time.Hour), Sequence: 4}
	fundFixture(store, 75)
	expires, disabled := at.Add(90*24*time.Hour), false
	if _, err := s.OperatorUpdate(ctx, ports.Actor{UserID: "operator", Role: ports.RoleOperator}, store.state.team.ID, TeamPatch{Revision: 1, ExpiresAt: &expires, Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if store.state.wallet.Balance != 175 || len(store.state.creditOperations) != 1 || store.state.grantSchedule.Sequence != 5 || !store.state.grantSchedule.NextAt.Equal(at.Add(d.GrantInterval)) {
		t.Fatal("renew+enable did not grant exactly one period", store.state.wallet, store.state.grantSchedule)
	}
	if count, err := s.Schedule(ctx, at, 20); err != nil || count != 0 {
		t.Fatal("renew+enable left a duplicate due cycle", count, err)
	}
}
