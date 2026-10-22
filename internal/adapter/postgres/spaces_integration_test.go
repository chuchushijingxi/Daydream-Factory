package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	n "drama-agent/internal/domain/notifications"
	p "drama-agent/internal/domain/project"
	d "drama-agent/internal/domain/spaces"
)

type teamFixture struct {
	store                            *Store
	service                          *app.Service
	team                             app.TeamView
	operator, owner, one, two, three ports.Actor
	at                               time.Time
}

func newTeamPG(t *testing.T, limit int) *teamFixture {
	t.Helper()
	store, r, _ := runtimeFixture(t)
	ctx := context.Background()
	f := &teamFixture{store: store, at: time.Now().UTC().Truncate(time.Microsecond)}
	actors := []*ports.Actor{&f.operator, &f.owner, &f.one, &f.two, &f.three}
	ids := []string{}
	for i, target := range actors {
		id := identity.New("team_user")
		role := ports.RoleUser
		if i == 0 {
			role = ports.RoleOperator
		}
		*target = ports.Actor{UserID: id, Role: role}
		ids = append(ids, id)
		if _, err := store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,$3,$4,'fixture')`, id, id+"@example.invalid", "团队测试"+strconv.Itoa(i), role); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.spaces(id,kind) VALUES($1,'personal')`, id); err != nil {
			t.Fatal(err)
		}
	}
	f.service = &app.Service{Store: store, Resolver: store, Limiter: r, Now: func() time.Time { return f.at }}

	t.Cleanup(func() {
		ctx := context.Background()
		// 本fixture的唯一ID范围，不清空共享测试表。
		for _, table := range []string{"space_shares", "team_reminders", "team_audit_events", "team_ownership_transfers", "team_join_requests", "team_invites", "team_model_discounts", "team_members"} {
			_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.`+table+` WHERE space_id=$1`, f.team.ID)
		}
		for _, table := range []string{"credit_allocation_events", "credit_allocations", "credit_ledger"} {
			_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.`+table+` WHERE account_id=$1`, f.team.ID)
		}
		_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.credit_accounts WHERE account_id=$1`, f.team.ID)
		for _, table := range []string{"tasks", "uploaded_files", "projects"} {
			_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.`+table+` WHERE space_id=$1`, f.team.ID)
		}
		_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.notification_receipts WHERE user_id=ANY($1)`, ids)
		_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.notification_messages WHERE recipient_user_id=ANY($1) OR author_id=ANY($1)`, ids)
		_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.teams WHERE space_id=$1`, f.team.ID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.spaces WHERE id=$1 OR id=ANY($2)`, f.team.ID, ids)
		_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.users WHERE id=ANY($1)`, ids)
		for _, id := range ids {
			_, _ = store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.runtime_events WHERE key=$1`, "notifications:"+id)
		}
	})
	var err error
	f.team, err = f.service.Open(ctx, f.operator, app.TeamInput{OwnerID: f.owner.UserID, Name: "PG团队 " + f.owner.UserID[len(f.owner.UserID)-10:], MemberLimit: limit, ExpiresAt: f.at.AddDate(0, 0, 20), GrantAmount: 600, ModelDiscounts: []app.ModelDiscount{{ModelInstanceID: "model-fixture", Percent: 85}}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var f *fault.Error
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func (f *teamFixture) join(t *testing.T, a ports.Actor) {
	t.Helper()
	link, err := f.service.CreateInviteLink(context.Background(), f.owner, f.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.AcceptLink(context.Background(), a, link.Token); err != nil {
		t.Fatal(err)
	}
}
func TestTeamPGConcurrentInvitationSeatsAndPendingRequests(t *testing.T) {
	f := newTeamPG(t, 2)
	ctx := context.Background()
	link, err := f.service.CreateInviteLink(ctx, f.owner, f.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for _, a := range []ports.Actor{f.one, f.two} {
		wg.Add(1)
		go func(a ports.Actor) { defer wg.Done(); _, e := f.service.AcceptLink(ctx, a, link.Token); errorsCh <- e }(a)
	}
	wg.Wait()
	close(errorsCh)
	successes := 0
	for e := range errorsCh {
		if e == nil {
			successes++
		} else {
			requireCode(t, e, "TEAM_MEMBER_LIMIT")
		}
	}
	if successes != 1 {
		t.Fatal("concurrent accept exceeded seats", successes)
	}
	members, err := f.store.SpaceMembers(ctx, f.team.ID)
	if err != nil || len(members) != 2 {
		t.Fatal(members, err)
	}
	if _, err = f.service.CreateInviteLink(ctx, f.owner, f.team.ID); err == nil {
		t.Fatal("full team issued invite")
	}
	errorsCh = make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := f.service.RequestJoin(ctx, f.three, f.team.ID, "申请加入")
			errorsCh <- e
		}()
	}
	wg.Wait()
	close(errorsCh)
	successes = 0
	for e := range errorsCh {
		if e == nil {
			successes++
		} else {
			requireCode(t, e, "JOIN_REQUEST_EXISTS")
		}
	}
	if successes != 1 {
		t.Fatal("duplicate pending requests", successes)
	}
	reqs, err := f.store.SpaceJoinRequests(ctx, f.team.ID)
	if err != nil || len(reqs) != 1 {
		t.Fatal(reqs, err)
	}
	requireCode(t, f.service.DecideJoin(ctx, f.owner, f.team.ID, reqs[0].ID, true), "TEAM_MEMBER_LIMIT")
	if err = f.service.DecideJoin(ctx, f.owner, f.team.ID, reqs[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if reqs, err = f.store.SpaceJoinRequests(ctx, f.team.ID); err != nil || len(reqs) != 0 {
		t.Fatal("decided request stayed in the pending list", reqs, err)
	}
	count := 0
	if err = f.store.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.notification_messages WHERE recipient_user_id=$1 AND kind='system'`, f.three.UserID).Scan(&count); err != nil || count != 1 {
		t.Fatal("application result notification missing", count, err)
	}
}
func TestTeamPGRolesAtomicOwnershipLifecycleAndQueries(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	f.join(t, f.one)
	f.join(t, f.two)
	if err := f.service.SetRole(ctx, f.owner, f.team.ID, f.one.UserID, d.Admin); err != nil {
		t.Fatal(err)
	}
	requireCode(t, f.service.SetRole(ctx, f.one, f.team.ID, f.two.UserID, d.Admin), "ROLE_FORBIDDEN")
	requireCode(t, f.service.RemoveMember(ctx, f.owner, f.team.ID, f.one.UserID), "ROLE_FORBIDDEN")
	if _, err := f.store.Resolve(ctx, f.operator, f.team.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("operator bypassed membership", err)
	}
	memberView, err := f.service.Members(ctx, f.two, f.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range memberView {
		if v.Email != "" || v.JoinedAt != nil {
			t.Fatal("member received private roster metadata", v)
		}
	}
	transfer, err := f.service.BeginOwnership(ctx, f.owner, f.team.ID, f.two.UserID)
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.service.AcceptOwnership(ctx, f.two, f.team.ID, transfer.ID) }()
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("ownership accepted more than once", success)
	}
	team, err := f.store.TeamView(ctx, f.team.ID)
	if err != nil || team.OwnerID != f.two.UserID {
		t.Fatal(team, err)
	}
	var owners int
	if err = f.store.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.team_members WHERE space_id=$1 AND role='owner'`, f.team.ID).Scan(&owners); err != nil || owners != 1 {
		t.Fatal("owner uniqueness", owners, err)
	}
	old, err := f.store.Resolve(ctx, f.owner, f.team.ID)
	if err != nil || old.Role != d.Admin {
		t.Fatal("previous owner not admin", old, err)
	}
	small := 1
	team, err = f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: team.Revision, MemberLimit: &small})
	if err != nil || team.MemberCount != 3 || team.MemberLimit != 1 {
		t.Fatal("lowering capacity removed members", team, err)
	}
	disabled := true
	team, err = f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: team.Revision, Disabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []ports.Actor{f.owner, f.one, f.two} {
		if _, err = f.store.Resolve(ctx, actor, f.team.ID); !errors.Is(err, fault.ErrNotFound) {
			t.Fatal("disabled team accessible", err)
		}
	}
	disabled = false
	team, err = f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: team.Revision, Disabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	list, err := f.service.List(ctx, f.two)
	if err != nil || len(list) != 2 {
		t.Fatal("personal and team listing", list, err)
	}
	search, err := f.service.OperatorList(ctx, f.operator, f.two.UserID, 1, 10)
	if err != nil || search.Total != 1 || search.Items[0].ID != f.team.ID {
		t.Fatal("operator owner search", search, err)
	}
	_, err = f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: team.Revision - 1, Disabled: &disabled})
	if !errors.Is(err, fault.ErrConflict) {
		t.Fatal("stale CAS accepted", err)
	}
	expires := f.at.Add(-time.Hour)
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.teams SET expires_at=$2 WHERE space_id=$1`, f.team.ID, expires); err != nil {
		t.Fatal(err)
	}
	scope, err := f.store.Resolve(ctx, f.two, f.team.ID)
	if err != nil || scope.Status != d.Expired || scope.ReadOnly {
		t.Fatal("expired team scope stage2", scope, err)
	}
	_, err = f.service.InviteEmail(ctx, f.two, f.team.ID, f.three.UserID+"@example.invalid")
	requireCode(t, err, "TEAM_EXPIRED")
}
func TestTeamPGReminderRenewalContentTransferAndAtomicNotification(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	f.join(t, f.one)
	f.join(t, f.two)
	project := p.Project{ID: identity.New("team_project"), SpaceID: f.team.ID, OwnerID: f.one.UserID, CreatorID: f.one.UserID, Revision: 1, Title: "保留标题", Original: []byte("保留完整原文")}
	if err := f.store.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	receipts, err := f.store.TransferSpaceContent(ctx, f.team.ID, f.owner.UserID, app.TransferInput{FromUserID: f.one.UserID, ToUserID: f.two.UserID, Resources: []app.ResourceRef{{Kind: "project", ID: project.ID}}})
	if err != nil || len(receipts) != 1 || receipts[0].Transferred {
		t.Fatal("active member transferred", receipts, err)
	}
	if err = f.service.RemoveMember(ctx, f.owner, f.team.ID, f.one.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Resolve(ctx, f.one, f.team.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("departed member resolved", err)
	}
	receipts, err = f.service.TransferContent(ctx, f.owner, f.team.ID, app.TransferInput{FromUserID: f.one.UserID, ToUserID: f.two.UserID, Resources: []app.ResourceRef{{Kind: "project", ID: project.ID}, {Kind: "upload", ID: "missing-file"}}})
	if err != nil || len(receipts) != 2 || !receipts[0].Transferred || receipts[1].Transferred {
		t.Fatal("per-item transfer receipts", receipts, err)
	}
	got, err := f.store.Snapshot(ctx, project.ID)
	if err != nil || got.OwnerID != f.two.UserID || got.CreatorID != f.one.UserID || got.SpaceID != f.team.ID || string(got.Original) != "保留完整原文" || got.Revision != 2 {
		t.Fatal("transfer altered creation or source", got, err)
	}
	f.at = time.Now().UTC().Add(time.Millisecond)
	overview, err := f.service.Overview(ctx, f.owner, f.team.ID, "7d")
	if err != nil || overview.Team.MemberCount != 2 || len(overview.Members) != 3 || len(overview.Recent) == 0 {
		t.Fatal("overview did not retain former creator", overview, err)
	}
	projectCount := int64(0)
	for _, member := range overview.Members {
		projectCount += member.Projects
	}
	if projectCount != 1 {
		t.Fatal("project creation range statistic", projectCount)
	}
	before, err := f.store.TeamView(ctx, f.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = f.store.SpaceTx(ctx, f.team.ID, func(tx app.Tx) error {
		team, e := tx.Team(ctx)
		if e != nil {
			return e
		}
		team.Name = "应该回滚"
		team.Revision++
		if e = tx.SaveTeam(ctx, team, before.Revision); e != nil {
			return e
		}
		return tx.SystemNotify(ctx, n.SystemInput{OperationID: identity.New("bad_event"), RecipientUserID: "missing-user", Title: "测试", Body: "无法投递的通知", Link: n.Link{Type: "team", SpaceID: f.team.ID}})
	})
	if err == nil {
		t.Fatal("invalid notification unexpectedly committed")
	}
	after, err := f.store.TeamView(ctx, f.team.ID)
	if err != nil || after.Name != before.Name || after.Revision != before.Revision {
		t.Fatal("notification failure partially committed team", after, err)
	}
	expiry := f.at.AddDate(0, 0, 7)
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.teams SET expires_at=$2 WHERE space_id=$1`, f.team.ID, expiry); err != nil {
		t.Fatal(err)
	}
	counts := make(chan int, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); sent, e := f.service.Schedule(ctx, f.at, 100); counts <- sent; failures <- e }()
	}
	wg.Wait()
	close(counts)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	sent := 0
	for n := range counts {
		sent += n
	}
	if sent != 1 {
		t.Fatal("concurrent scheduler duplicated reminder", sent)
	}
	if sent, err = f.service.Schedule(ctx, f.at, 100); err != nil || sent != 0 {
		t.Fatal("reminder repeated", sent, err)
	}
	view, _ := f.store.TeamView(ctx, f.team.ID)
	renewed := expiry.AddDate(0, 0, 30)
	if _, err = f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: view.Revision, ExpiresAt: &renewed}); err != nil {
		t.Fatal(err)
	}
	f.at = f.at.AddDate(0, 0, 30)
	if sent, err = f.service.Schedule(ctx, f.at, 100); err != nil || sent != 2 {
		t.Fatal("renewed period reminder and ordinary grant were not processed once", sent, err)
	}
	var stored int
	if err = f.store.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.team_reminders WHERE space_id=$1 AND kind='expires_7d'`, f.team.ID).Scan(&stored); err != nil || stored != 2 {
		t.Fatal(stored, err)
	}
	raw, _ := json.Marshal(overview)
	if string(raw) == "" {
		t.Fatal("overview serialization")
	}
}

func TestTeamPGEmailInvitesExpirationAndStaleOwnership(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	invite, err := f.service.InviteEmail(ctx, f.owner, f.team.ID, f.one.UserID+"@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.InviteEmail(ctx, f.owner, f.team.ID, f.one.UserID+"@example.invalid"); err == nil {
		t.Fatal("duplicate pending email invite")
	} else {
		requireCode(t, err, "TEAM_INVITE_EXISTS")
	}
	pending, err := f.service.PendingInvites(ctx, f.one)
	if err != nil || len(pending) != 1 || pending[0].ID != invite.ID {
		t.Fatal(pending, err)
	}
	if _, err = f.service.AcceptInvite(ctx, f.two, invite.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("other user accepted email invite", err)
	}
	if err = f.service.DeclineInvite(ctx, f.one, invite.ID); err != nil {
		t.Fatal(err)
	}
	invite, err = f.service.InviteEmail(ctx, f.owner, f.team.ID, f.one.UserID+"@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.AcceptInvite(ctx, f.one, invite.ID); err != nil {
		t.Fatal(err)
	}
	old, err := f.service.InviteEmail(ctx, f.owner, f.team.ID, f.two.UserID+"@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.team_invites SET expires_at=$2 WHERE id=$1`, old.ID, f.at.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	replacement, err := f.service.InviteEmail(ctx, f.owner, f.team.ID, f.two.UserID+"@example.invalid")
	if err != nil || replacement.ID == old.ID {
		t.Fatal("expired invitation blocked replacement", replacement, err)
	}
	stale, err := f.store.SpaceInvite(ctx, old.ID)
	if err != nil || stale.Status != "expired" {
		t.Fatal(stale, err)
	}
	if _, err = f.service.AcceptInvite(ctx, f.two, replacement.ID); err != nil {
		t.Fatal(err)
	}
	transfer, err := f.service.BeginOwnership(ctx, f.owner, f.team.ID, f.one.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Leave(ctx, f.one, f.team.ID); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = f.store.pool.QueryRow(ctx, `SELECT status FROM drama_catalog_v1.team_ownership_transfers WHERE id=$1`, transfer.ID).Scan(&status); err != nil || status != "cancelled" {
		t.Fatal("departure kept invalid pending ownership", status, err)
	}
	transfer, err = f.service.BeginOwnership(ctx, f.owner, f.team.ID, f.two.UserID)
	if err != nil {
		t.Fatal("new valid ownership proposal blocked", err)
	}
	view, _ := f.store.TeamView(ctx, f.team.ID)
	if _, err = f.service.OperatorUpdate(ctx, f.operator, f.team.ID, app.TeamPatch{Revision: view.Revision, OwnerID: &f.two.UserID}); err != nil {
		t.Fatal(err)
	}
	if err = f.store.pool.QueryRow(ctx, `SELECT status FROM drama_catalog_v1.team_ownership_transfers WHERE id=$1`, transfer.ID).Scan(&status); err != nil || status != "cancelled" {
		t.Fatal("operator replacement kept old pending ownership", status, err)
	}
	link, err := f.service.CreateInviteLink(ctx, f.two, f.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.RevokeInviteLink(ctx, f.two, f.team.ID, link.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.AcceptLink(ctx, f.three, link.Token); err == nil {
		t.Fatal("revoked link accepted")
	}
}

// Background Agent continuations carry a frozen actor rather than a Cookie.
// Fresh space authorization must still honor the durable account enable state.
func TestTeamPGSpaceResolverRejectsDisabledFrozenActors(t *testing.T) {
	f := newTeamPG(t, 3)
	ctx := context.Background()
	f.join(t, f.one)
	actor := f.one
	resource := d.Resource{SpaceID: f.team.ID, OwnerID: actor.UserID, Kind: d.ProjectResource, ID: identity.New("frozen_actor_resource")}
	// HTTP request-local memoization is deliberately discarded by each tool step.
	request := ports.WithRequestedSpace(ctx, f.team.ID)
	for _, space := range []string{actor.UserID, f.team.ID} {
		if _, err := f.store.Resolve(request, actor, space); err != nil {
			t.Fatal("enabled actor", space, err)
		}
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.users SET enabled=false WHERE id=$1`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	for _, space := range []string{"", actor.UserID, f.team.ID} {
		_, err := f.store.Resolve(ports.WithoutSpaceRequest(request), actor, space)
		requireCode(t, err, "ACCOUNT_DISABLED")
	}
	permission, _, err := f.store.Access(ports.WithoutSpaceRequest(request), actor, resource)
	if permission != d.None {
		t.Fatal("disabled actor received content access", permission)
	}
	requireCode(t, err, "ACCOUNT_DISABLED")
	// A deleted/missing durable identity must not acquire a scope from an orphan.
	orphan := identity.New("orphan_actor")
	if _, err = f.store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.spaces(id,kind) VALUES($1,'personal')`, orphan); err != nil {
		t.Fatal(err)
	}
	defer f.store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.spaces WHERE id=$1`, orphan)
	if _, err = f.store.Resolve(ctx, ports.Actor{UserID: orphan, Role: ports.RoleUser}, ""); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("orphan identity resolved", err)
	}
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.users SET enabled=true WHERE id=$1`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	for _, space := range []string{actor.UserID, f.team.ID} {
		if _, err = f.store.Resolve(ports.WithoutSpaceRequest(request), actor, space); err != nil {
			t.Fatal("reenabled actor", space, err)
		}
	}
	// Account disable must not prevent trusted worker persistence of a receipt for
	// an already accepted operation; this internal store path never re-resolves.
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.users SET enabled=false WHERE id=$1`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	project := p.Project{ID: identity.New("trusted_receipt"), SpaceID: f.team.ID, OwnerID: actor.UserID, CreatorID: actor.UserID, Revision: 1, Title: "已受理任务"}
	if err = f.store.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	project.Title = "受理后回执"
	project.Revision++
	if err = f.store.ForProjectSpace(f.team.ID).Commit(ctx, project, 1); err != nil {
		t.Fatal("trusted persistence consulted actor or page lease", err)
	}
}
