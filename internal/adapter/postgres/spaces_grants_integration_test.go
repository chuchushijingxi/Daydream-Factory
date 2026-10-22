package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	app "drama-agent/internal/application/spaces"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/spaces"
)

func grantState(t *testing.T, f *teamFixture) (d.GrantSchedule, int64) {
	t.Helper()
	var schedule d.GrantSchedule
	if e := f.store.SpaceTx(context.Background(), f.team.ID, func(tx app.Tx) error {
		var err error
		schedule, err = tx.GrantSchedule(context.Background())
		return err
	}); e != nil {
		t.Fatal(e)
	}
	v, e := f.store.Balance(context.Background(), f.team.ID)
	if e != nil {
		t.Fatal(e)
	}
	return schedule, v
}
func TestTeamGrantsPGInitialConcurrentScheduleRenewalAndEnable(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	schedule, balance := grantState(t, f)
	if balance != 600 || schedule.Sequence != 1 || !schedule.NextAt.Equal(f.at.Add(d.GrantInterval)) {
		t.Fatal("initial contract grant", schedule, balance)
	}
	extended := f.at.AddDate(1, 0, 0)
	if _, e := f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: f.team.Revision, ExpiresAt: &extended}); e != nil {
		t.Fatal(e)
	}
	unchanged, balance := grantState(t, f)
	if unchanged != schedule || balance != 600 {
		t.Fatal("early renewal changed grant rhythm", unchanged, balance)
	}
	f.at = schedule.NextAt
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := f.service.Schedule(ctx, f.at, 100); results <- e }()
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	schedule, balance = grantState(t, f)
	if balance != 1200 || schedule.Sequence != 2 {
		t.Fatal("concurrent periodic grant duplicated", schedule, balance)
	}
	restarted := &app.Service{Store: f.store, Resolver: f.store, Limiter: f.service.Limiter, Now: f.service.Now}
	if _, e := restarted.Schedule(ctx, f.at, 100); e != nil {
		t.Fatal(e)
	}
	_, balance = grantState(t, f)
	if balance != 1200 {
		t.Fatal("restart repeated grant", balance)
	}
	team, _ := f.store.TeamView(ctx, f.team.ID)
	disabled := true
	if _, e := f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: team.Revision, Disabled: &disabled}); e != nil {
		t.Fatal(e)
	}
	f.at = f.at.Add(70 * 24 * time.Hour)
	if _, e := f.service.Schedule(ctx, f.at, 100); e != nil {
		t.Fatal(e)
	}
	_, balance = grantState(t, f)
	if balance != 1200 {
		t.Fatal("disabled team granted", balance)
	}
	team, _ = f.store.TeamView(ctx, f.team.ID)
	disabled = false
	if _, e := f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: team.Revision, Disabled: &disabled}); e != nil {
		t.Fatal(e)
	}
	enabled, balance := grantState(t, f)
	if balance != 1200 || !enabled.NextAt.After(f.at) || !enabled.NextAt.Equal(schedule.NextAt.Add(2*d.GrantInterval)) || enabled.Sequence != schedule.Sequence+2 {
		t.Fatal("N3 did not skip old rhythm without grant", enabled, balance)
	}
	f.at = extended.Add(time.Hour)
	if _, e := f.service.Schedule(ctx, f.at, 100); e != nil {
		t.Fatal(e)
	}
	_, balance = grantState(t, f)
	if balance != 1200 {
		t.Fatal("expired team granted", balance)
	}
	team, _ = f.store.TeamView(ctx, f.team.ID)
	renewed := f.at.AddDate(1, 0, 0)
	if _, e := f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: team.Revision, ExpiresAt: &renewed}); e != nil {
		t.Fatal(e)
	}
	after, balance := grantState(t, f)
	if balance != 1800 || after.Sequence != enabled.Sequence+1 || !after.NextAt.Equal(f.at.Add(d.GrantInterval)) {
		t.Fatal("Q3 expired renewal did not grant once and re-anchor", after, balance)
	}
}
func TestTeamCreditsPGRolesReleaseRefundAndReadOnly(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	f.join(t, f.one)
	allocation := app.AllocationInput{CommandID: identity.New("allocate"), Amount: 300}
	if _, e := f.service.Allocate(ctx, f.owner, f.team.ID, f.one.UserID, allocation); e != nil {
		t.Fatal(e)
	}
	if _, e := f.service.Allocate(ctx, f.owner, f.team.ID, f.one.UserID, allocation); e != nil {
		t.Fatal(e)
	}
	var audits int
	if e := f.store.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.team_audit_events WHERE space_id=$1 AND action='credits_allocate'`, f.team.ID).Scan(&audits); e != nil || audits != 1 {
		t.Fatal("allocation replay duplicated management audit", audits, e)
	}
	debit, e := f.store.Deduct(ctx, c.Change{AccountID: f.team.ID, HolderID: f.one.UserID, Amount: 100, OperationID: identity.New("debit"), Source: "fixture", Subject: f.one.UserID, Reason: "generation"})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.service.SetRole(ctx, f.owner, f.team.ID, f.one.UserID, d.Admin); e != nil {
		t.Fatal(e)
	}
	// 管理员同样只花名下分配额：升级保留余额，返还回到本人名下，降级也不清零。
	held, e := f.store.HolderCredits(ctx, f.team.ID, f.one.UserID)
	if e != nil || !held.Open || held.Balance != 200 {
		t.Fatal("upgrade did not keep the holder balance", held, e)
	}
	if _, e = f.store.Refund(ctx, c.Change{OriginalDeductionID: debit.ID, HolderID: debit.HolderID, Amount: 100, OperationID: identity.New("refund"), Subject: debit.Subject, Reason: "generation-incomplete"}); e != nil {
		t.Fatal(e)
	}
	held, e = f.store.HolderCredits(ctx, f.team.ID, f.one.UserID)
	if e != nil || !held.Open || held.Balance != 300 {
		t.Fatal("refund after upgrade did not return to the admin holder", held, e)
	}
	if e = f.service.SetRole(ctx, f.owner, f.team.ID, f.one.UserID, d.Member); e != nil {
		t.Fatal(e)
	}
	held, e = f.store.HolderCredits(ctx, f.team.ID, f.one.UserID)
	if e != nil || !held.Open || held.Balance != 300 {
		t.Fatal("downgrade did not keep the holder balance", held, e)
	}
	if _, e = f.service.Allocate(ctx, f.owner, f.team.ID, f.one.UserID, app.AllocationInput{CommandID: identity.New("allocate"), Amount: 50}); e != nil {
		t.Fatal(e)
	}
	if e = f.service.Leave(ctx, f.one, f.team.ID); e != nil {
		t.Fatal(e)
	}
	v := allocationInvariant(t, f.store, f.team.ID)
	if v.Allocated != 0 || v.Wallet.Balance != 600 {
		t.Fatal("departure did not return unused allocation", v)
	}
	if _, e = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.teams SET expires_at=$2 WHERE space_id=$1`, f.team.ID, time.Now().UTC().Add(-time.Hour)); e != nil {
		t.Fatal(e)
	}
	scope, e := f.store.Resolve(ctx, f.owner, f.team.ID)
	if e != nil || scope.ReadOnly {
		t.Fatal("expired with credits became read-only", scope, e)
	}
	if _, e = f.store.Deduct(ctx, c.Change{AccountID: f.team.ID, Amount: 600, OperationID: identity.New("debit"), Source: "fixture", Subject: f.owner.UserID, Reason: "generation"}); e != nil {
		t.Fatal(e)
	}
	scope, e = f.store.Resolve(ctx, f.owner, f.team.ID)
	if e != nil || !scope.ReadOnly {
		t.Fatal("expired total-zero was not read-only", scope, e)
	}
	list, e := f.store.MySpaces(ctx, f.owner.UserID)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, space := range list {
		if space.ID == f.team.ID {
			found = space.ReadOnly
		}
	}
	if !found {
		t.Fatal("space list did not project read-only")
	}
	if _, e = f.service.Schedule(ctx, time.Now().UTC(), 100); e != nil {
		t.Fatal(e)
	}
	var notices int
	if e = f.store.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.team_reminders WHERE space_id=$1 AND kind='read_only'`, f.team.ID).Scan(&notices); e != nil || notices != 1 {
		t.Fatal("read-only notice missing", notices, e)
	}
}

func TestTeamGrantsPGDisabledExpiredRenewalDefersUntilOriginalRhythm(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	before, balance := grantState(t, f)
	if balance != 600 {
		t.Fatal(balance)
	}
	disabled := true
	if _, e := f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: f.team.Revision, Disabled: &disabled}); e != nil {
		t.Fatal(e)
	}
	f.at = f.at.Add(40 * 24 * time.Hour)
	view, _ := f.store.TeamView(ctx, f.team.ID)
	renewed := f.at.AddDate(1, 0, 0)
	if _, e := f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: view.Revision, ExpiresAt: &renewed}); e != nil {
		t.Fatal(e)
	}
	after, balance := grantState(t, f)
	if balance != 600 || !after.NextAt.Equal(before.NextAt) || after.Sequence != before.Sequence {
		t.Fatal("N4 disabled renewal granted or reset rhythm", after, balance)
	}
	f.at = f.at.Add(10 * 24 * time.Hour)
	view, _ = f.store.TeamView(ctx, f.team.ID)
	disabled = false
	if _, e := f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: view.Revision, Disabled: &disabled}); e != nil {
		t.Fatal(e)
	}
	enabled, balance := grantState(t, f)
	if balance != 600 || enabled.Sequence != 2 || !enabled.NextAt.Equal(before.NextAt.Add(d.GrantInterval)) || !enabled.NextAt.After(f.at) {
		t.Fatal("N4 later enable caught up or changed anchor", enabled, balance)
	}
	f.at = enabled.NextAt
	if _, e := f.service.Schedule(ctx, f.at, 100); e != nil {
		t.Fatal(e)
	}
	final, balance := grantState(t, f)
	if balance != 1200 || final.Sequence != 3 || !final.NextAt.Equal(enabled.NextAt.Add(d.GrantInterval)) {
		t.Fatal("N4 following ordinary grant was not one cycle", final, balance)
	}
	var skipped int
	if e := f.store.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE operation_id=$1`, "team-grant:"+f.team.ID+":2").Scan(&skipped); e != nil || skipped != 0 {
		t.Fatal("disabled cycle was backfilled", skipped, e)
	}
}
func TestTeamGrantsPGExpiredRenewalAndEnableTogetherIssuesOnce(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	disabled := true
	if _, e := f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: f.team.Revision, Disabled: &disabled}); e != nil {
		t.Fatal(e)
	}
	f.at = f.at.Add(40 * 24 * time.Hour)
	view, _ := f.store.TeamView(ctx, f.team.ID)
	renewed := f.at.AddDate(1, 0, 0)
	disabled = false
	if _, e := f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: view.Revision, ExpiresAt: &renewed, Disabled: &disabled}); e != nil {
		t.Fatal(e)
	}
	after, balance := grantState(t, f)
	if balance != 1200 || after.Sequence != 2 || !after.NextAt.Equal(f.at.Add(d.GrantInterval)) {
		t.Fatal("combined renewal/enable did not follow Q3", after, balance)
	}
}
