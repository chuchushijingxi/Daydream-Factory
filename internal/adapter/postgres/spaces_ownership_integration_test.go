package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/spaces"
	n "drama-agent/internal/domain/notifications"
)

func ownershipPending(t *testing.T, f *teamFixture) []app.OwnershipTransfer {
	t.Helper()
	var out []app.OwnershipTransfer
	if err := f.store.SpaceTx(context.Background(), f.team.ID, func(tx app.Tx) error {
		var err error
		out, err = tx.PendingOwnershipTransfers(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}
func assertOneOwner(t *testing.T, f *teamFixture, user string) {
	t.Helper()
	var count int
	var owner string
	if err := f.store.pool.QueryRow(context.Background(), `SELECT count(*),min(user_id) FROM drama_catalog_v1.team_members WHERE space_id=$1 AND role='owner'`, f.team.ID).Scan(&count, &owner); err != nil || count != 1 || owner != user {
		t.Fatal("owner membership not unique", count, owner, err)
	}
	team, err := f.store.TeamView(context.Background(), f.team.ID)
	if err != nil || team.OwnerID != user {
		t.Fatal("team owner differs from membership", team, err)
	}
}
func TestTeamPGOwnershipProposalReplacesPriorAndSameRecipient(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	f.join(t, f.one)
	f.join(t, f.two)
	first, err := f.service.BeginOwnership(ctx, f.owner, f.team.ID, f.one.UserID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.BeginOwnership(ctx, f.owner, f.team.ID, f.two.UserID)
	if err != nil {
		t.Fatal("replacement was rejected", err)
	}
	requireCode(t, f.service.AcceptOwnership(ctx, f.one, f.team.ID, first.ID), "TEAM_OWNERSHIP_CONFLICT")
	third, err := f.service.BeginOwnership(ctx, f.owner, f.team.ID, f.two.UserID)
	if err != nil || third.ID == second.ID {
		t.Fatal("same-recipient proposal was not replaced", third, err)
	}
	requireCode(t, f.service.AcceptOwnership(ctx, f.two, f.team.ID, second.ID), "TEAM_OWNERSHIP_CONFLICT")
	pending := ownershipPending(t, f)
	if len(pending) != 1 || pending[0].ID != third.ID {
		t.Fatal("latest proposal not sole pending", pending)
	}
	for _, id := range []string{first.ID, second.ID} {
		if err = f.store.SpaceTx(ctx, f.team.ID, func(tx app.Tx) error {
			v, e := tx.OwnershipTransfer(ctx, id)
			if e != nil {
				return e
			}
			if v.Status != "cancelled" || v.AcceptedAt != nil {
				t.Fatal("superseded proposal projected as accepted", v)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var cancelled bool
		if err = f.store.pool.QueryRow(ctx, `SELECT status='cancelled' AND decided_at IS NOT NULL FROM drama_catalog_v1.team_ownership_transfers WHERE id=$1`, id).Scan(&cancelled); err != nil || !cancelled {
			t.Fatal("cancellation timestamp missing", cancelled, err)
		}
	}
	var superseded int
	if err = f.store.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.team_audit_events WHERE space_id=$1 AND action='ownership_superseded'`, f.team.ID).Scan(&superseded); err != nil || superseded != 2 {
		t.Fatal("replacement audit missing", superseded, err)
	}
	if err = f.service.AcceptOwnership(ctx, f.two, f.team.ID, third.ID); err != nil {
		t.Fatal("latest proposal did not accept", err)
	}
	assertOneOwner(t, f, f.two.UserID)
	if pending = ownershipPending(t, f); len(pending) != 0 {
		t.Fatal("accepted proposal remained pending", pending)
	}
}

var errOwnershipNoticeFixture = errors.New("injected ownership notification failure")

type failingOwnershipNoticeStore struct{ app.Store }

func (s failingOwnershipNoticeStore) SpaceTx(ctx context.Context, id string, apply func(app.Tx) error) error {
	return s.Store.SpaceTx(ctx, id, func(tx app.Tx) error { return apply(failingOwnershipNoticeTx{tx}) })
}

type failingOwnershipNoticeTx struct{ app.Tx }

func (t failingOwnershipNoticeTx) SystemNotify(ctx context.Context, in n.SystemInput) error {
	if err := t.Tx.SystemNotify(ctx, in); err != nil {
		return err
	}
	if strings.HasPrefix(in.OperationID, "team-ownership:") {
		return errOwnershipNoticeFixture
	}
	return nil
}
func TestTeamPGOwnershipReplacementNotificationRollbackKeepsOldProposal(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	f.join(t, f.one)
	f.join(t, f.two)
	original, err := f.service.BeginOwnership(ctx, f.owner, f.team.ID, f.one.UserID)
	if err != nil {
		t.Fatal(err)
	}
	count := func(query string) int {
		t.Helper()
		var n int
		if e := f.store.pool.QueryRow(ctx, query, f.team.ID).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	const auditCount = `SELECT count(*) FROM drama_catalog_v1.team_audit_events WHERE space_id=$1`
	const messageCount = `SELECT count(*) FROM drama_catalog_v1.notification_messages WHERE kind='system' AND link->>'spaceId'=$1`
	beforeAudit, beforeMessages := count(auditCount), count(messageCount)
	failing := *f.service
	failing.Store = failingOwnershipNoticeStore{Store: f.store}
	if _, err = failing.BeginOwnership(ctx, f.owner, f.team.ID, f.two.UserID); !errors.Is(err, errOwnershipNoticeFixture) {
		t.Fatal("notification failure not propagated", err)
	}
	pending := ownershipPending(t, f)
	if len(pending) != 1 || pending[0].ID != original.ID {
		t.Fatal("failed replacement lost original pending", pending)
	}
	var records int
	if err = f.store.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.team_ownership_transfers WHERE space_id=$1`, f.team.ID).Scan(&records); err != nil || records != 1 {
		t.Fatal("replacement record partially committed", records, err)
	}
	if count(auditCount) != beforeAudit || count(messageCount) != beforeMessages {
		t.Fatal("replacement audit or notification partially committed")
	}
	if err = f.service.AcceptOwnership(ctx, f.one, f.team.ID, original.ID); err != nil {
		t.Fatal("rollback invalidated original confirmation", err)
	}
	assertOneOwner(t, f, f.one.UserID)
}
func TestTeamPGConcurrentOwnershipProposalsAndConfirmation(t *testing.T) {
	t.Run("two-proposals", func(t *testing.T) {
		f := newTeamPG(t, 4)
		ctx := context.Background()
		f.join(t, f.one)
		f.join(t, f.two)
		type result struct {
			transfer app.OwnershipTransfer
			err      error
		}
		results := make(chan result, 2)
		var wg sync.WaitGroup
		for _, target := range []ports.Actor{f.one, f.two} {
			wg.Add(1)
			go func(target ports.Actor) {
				defer wg.Done()
				v, e := f.service.BeginOwnership(ctx, f.owner, f.team.ID, target.UserID)
				results <- result{v, e}
			}(target)
		}
		wg.Wait()
		close(results)
		proposals := []app.OwnershipTransfer{}
		for v := range results {
			if v.err != nil {
				t.Fatal(v.err)
			}
			proposals = append(proposals, v.transfer)
		}
		pending := ownershipPending(t, f)
		if len(pending) != 1 {
			t.Fatal("concurrent proposals left multiple pending", pending)
		}
		for _, v := range proposals {
			actor := f.one
			if v.ToUserID == f.two.UserID {
				actor = f.two
			}
			if v.ID != pending[0].ID {
				requireCode(t, f.service.AcceptOwnership(ctx, actor, f.team.ID, v.ID), "TEAM_OWNERSHIP_CONFLICT")
			}
		}
		actor := f.one
		if pending[0].ToUserID == f.two.UserID {
			actor = f.two
		}
		if err := f.service.AcceptOwnership(ctx, actor, f.team.ID, pending[0].ID); err != nil {
			t.Fatal(err)
		}
		assertOneOwner(t, f, actor.UserID)
	})
	t.Run("proposal-and-confirmation", func(t *testing.T) {
		f := newTeamPG(t, 4)
		ctx := context.Background()
		f.join(t, f.one)
		f.join(t, f.two)
		original, err := f.service.BeginOwnership(ctx, f.owner, f.team.ID, f.one.UserID)
		if err != nil {
			t.Fatal(err)
		}
		var next app.OwnershipTransfer
		var proposeErr, acceptErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			next, proposeErr = f.service.BeginOwnership(ctx, f.owner, f.team.ID, f.two.UserID)
		}()
		go func() { defer wg.Done(); acceptErr = f.service.AcceptOwnership(ctx, f.one, f.team.ID, original.ID) }()
		wg.Wait()
		switch {
		case proposeErr == nil:
			requireCode(t, acceptErr, "TEAM_OWNERSHIP_CONFLICT")
			if err = f.service.AcceptOwnership(ctx, f.two, f.team.ID, next.ID); err != nil {
				t.Fatal(err)
			}
			assertOneOwner(t, f, f.two.UserID)
		case acceptErr == nil:
			requireCode(t, proposeErr, "ROLE_FORBIDDEN")
			assertOneOwner(t, f, f.one.UserID)
		default:
			t.Fatal("neither serialized action succeeded", proposeErr, acceptErr)
		}
	})
}
