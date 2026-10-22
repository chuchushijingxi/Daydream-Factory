package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
)

func allocationPG(t *testing.T) (*Store, string) {
	t.Helper()
	s, _, _ := runtimeFixture(t)
	account := identity.New("allocation_account")
	ctx := context.Background()
	if _, err := s.Add(ctx, d.Change{AccountID: account, Amount: 5000, Category: d.Gift, OperationID: identity.New("fund"), Source: "fixture", Subject: "fixture", Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, table := range []string{"credit_allocation_events", "credit_allocations", "credit_ledger"} {
			_, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.`+table+` WHERE account_id=$1`, account)
		}
		_, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.credit_accounts WHERE account_id=$1`, account)
		_, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.runtime_events WHERE key=$1`, "credits:"+account)
	})
	return s, account
}
func allocationChange(account, holder, kind string, amount int64) d.AllocationChange {
	return d.AllocationChange{AccountID: account, HolderID: holder, OperationID: identity.New(kind), ActorID: "manager", Amount: amount}
}
func allocationInvariant(t *testing.T, s *Store, account string) d.Balances {
	t.Helper()
	v, e := s.CreditBalances(context.Background(), account)
	if e != nil {
		t.Fatal(e)
	}
	un, e := v.Unallocated()
	if e != nil || un < 0 || v.Wallet.Balance != v.Allocated+un {
		t.Fatal("allocation invariant", v, un, e)
	}
	return v
}
func TestCreditAllocationsPGHolderRefundReleaseAndIdempotency(t *testing.T) {
	s, account := allocationPG(t)
	ctx := context.Background()
	change := allocationChange(account, "member", "allocate", 4000)
	first, err := s.Allocate(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Allocate(ctx, change)
	if err != nil || again.ID != first.ID || !again.Replayed || !again.Same("allocate", change) {
		t.Fatal("allocation replay", again, err)
	}
	altered := change
	altered.Amount = 3000
	_, err = s.Allocate(ctx, altered)
	requireCode(t, err, "CREDIT_IDEMPOTENCY_CONFLICT")
	debit, err := s.Deduct(ctx, d.Change{AccountID: account, HolderID: "member", Amount: 1000, OperationID: identity.New("debit"), Source: "project", Subject: "member", Reason: "generation"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Deduct(ctx, d.Change{AccountID: account, Amount: 1001, OperationID: identity.New("debit"), Source: "project", Subject: "manager", Reason: "generation"})
	requireCode(t, err, "CREDIT_UNALLOCATED_INSUFFICIENT")
	if _, err = s.Refund(ctx, d.Change{OriginalDeductionID: debit.ID, HolderID: "member", Amount: 400, OperationID: identity.New("refund"), Subject: "member", Reason: "generation-incomplete"}); err != nil {
		t.Fatal(err)
	}
	held, err := s.HolderCredits(ctx, account, "member")
	if err != nil || held.Balance != 3400 {
		t.Fatal(held, err)
	}
	release := allocationChange(account, "member", "release", 0)
	event, err := s.Release(ctx, release)
	if err != nil || event.Amount != 3400 {
		t.Fatal(event, err)
	}
	if _, err = s.Refund(ctx, d.Change{OriginalDeductionID: debit.ID, HolderID: "member", Amount: 600, OperationID: identity.New("refund"), Subject: "member", Reason: "generation-incomplete"}); err != nil {
		t.Fatal(err)
	}
	held, err = s.HolderCredits(ctx, account, "member")
	if err != nil || held.Open || held.Balance != 0 {
		t.Fatal("closed refund reopened holder", held, err)
	}
	v := allocationInvariant(t, s, account)
	if v.Wallet.Balance != 5000 || v.Allocated != 0 {
		t.Fatal(v)
	}
	_, err = s.Deduct(ctx, d.Change{AccountID: account, HolderID: "member", Amount: 1, OperationID: identity.New("debit"), Source: "project", Subject: "member", Reason: "generation"})
	requireCode(t, err, "CREDIT_HOLDER_INSUFFICIENT")
	history, err := s.HolderCreditHistory(ctx, account, "member", d.HistoryFilter{Category: "all", Direction: "all", Page: 1, PageSize: 50})
	if err != nil || history.Total != 3 || history.Summary.Acquired != 0 || history.Summary.NetConsumed != 0 {
		t.Fatal("allocation faked wallet acquisitions", history, err)
	}
	events, err := s.AllocationHistory(ctx, account, "member", d.HistoryFilter{Category: "all", Direction: "all", Page: 1, PageSize: 50})
	if err != nil || events.Total != 2 {
		t.Fatal(events, err)
	}
}
func TestCreditAllocationsPGConcurrentInvariantAndUnallocatedLock(t *testing.T) {
	s, account := allocationPG(t)
	ctx := context.Background()
	errs := make(chan error, 30)
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			holder := identity.New("holder")
			if _, e := s.Allocate(ctx, allocationChange(account, holder, "allocate", 100)); e != nil {
				errs <- e
				return
			}
			debit, e := s.Deduct(ctx, d.Change{AccountID: account, HolderID: holder, Amount: 40, OperationID: identity.New("debit"), Source: "fixture", Subject: holder, Reason: "generation"})
			if e != nil {
				errs <- e
				return
			}
			if _, e = s.Refund(ctx, d.Change{OriginalDeductionID: debit.ID, HolderID: holder, Amount: 20, OperationID: identity.New("refund"), Subject: holder, Reason: "generation-incomplete"}); e != nil {
				errs <- e
				return
			}
			if _, e = s.Reclaim(ctx, allocationChange(account, holder, "reclaim", 30)); e != nil {
				errs <- e
				return
			}
			if _, e = s.Release(ctx, allocationChange(account, holder, "release", 0)); e != nil {
				errs <- e
				return
			}
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	v := allocationInvariant(t, s, account)
	if v.Wallet.Balance != 4400 || v.Allocated != 0 {
		t.Fatal(v)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = s.UnallocatedTx(ctx, tx, account); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, e := s.Allocate(ctx, allocationChange(account, "locked", "allocate", 1)); done <- e }()
	select {
	case e := <-done:
		t.Fatal("allocation bypassed refund/unallocated account lock", e)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
