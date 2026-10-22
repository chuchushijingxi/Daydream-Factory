package credits

import (
	"drama-agent/internal/domain/fault"
	"errors"
	"testing"
)

func TestHolderAllocationPreservesAccountPartitionAcrossConsumptionAndReturn(t *testing.T) {
	wallet := Wallet{Balance: 100, GiftBalance: 70, RechargeBalance: 30}
	holder := HolderAllocation{AccountID: "account", HolderID: "holder", Open: true}
	assertPartition := func(w Wallet, h HolderAllocation, want int64) {
		t.Helper()
		allocated := int64(0)
		if h.Open {
			allocated = h.Balance
		}
		remaining, err := Unallocated(w.Balance, allocated)
		if err != nil || remaining != want || w.Balance != allocated+remaining {
			t.Fatal(w, h, remaining, want, err)
		}
	}
	var err error
	holder, err = holder.Allocate(100, 40)
	if err != nil {
		t.Fatal(err)
	}
	assertPartition(wallet, holder, 60)
	next, parts, err := wallet.Deduct(15)
	if err != nil || parts.Gift != 15 {
		t.Fatal(parts, err)
	}
	wallet = next
	holder, err = holder.Deduct(15)
	if err != nil {
		t.Fatal(err)
	}
	assertPartition(wallet, holder, 60)
	wallet, err = wallet.Credit(parts)
	if err != nil {
		t.Fatal(err)
	}
	holder, err = holder.Refund(15)
	if err != nil {
		t.Fatal(err)
	}
	assertPartition(wallet, holder, 60)
	holder, reclaimed, err := holder.Reclaim(10, false)
	if err != nil || reclaimed != 10 {
		t.Fatal(reclaimed, err)
	}
	assertPartition(wallet, holder, 70)
	holder, released, err := holder.Release()
	if err != nil || released != 30 || holder.Open {
		t.Fatal(holder, released, err)
	}
	assertPartition(wallet, holder, 100)
	returned, err := holder.Refund(20)
	if err != nil || returned.Balance != 0 || returned.Open {
		t.Fatal("closed holder captured refund", returned, err)
	}
}
func TestHolderLimitsAndReopeningStartAtZero(t *testing.T) {
	holder := HolderAllocation{Open: true, Balance: 7}
	for _, run := range []func() error{
		func() error { _, e := holder.Allocate(3, 4); return e },
		func() error { _, e := holder.Deduct(8); return e },
		func() error { _, _, e := holder.Reclaim(8, false); return e },
		func() error { _, e := Unallocated(4, 5); return e },
		func() error { _, e := (HolderAllocation{Open: false, Balance: 1}).Refund(1); return e },
	} {
		if run() == nil {
			t.Fatal("invalid allocation state or overspend accepted")
		}
	}
	empty, amount, err := holder.Reclaim(0, true)
	if err != nil || amount != 7 || empty.Balance != 0 || !empty.Open {
		t.Fatal(empty, amount, err)
	}
	closed, _, err := holder.Release()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := closed.Allocate(20, 5)
	if err != nil || reopened.Balance != 5 || !reopened.Open {
		t.Fatal(reopened, err)
	}
	if _, err = closed.Deduct(1); err == nil {
		t.Fatal("closed holder spent credits")
	}
}
func TestAllocationRequestIdempotencyComparesRequestedAmountAllActorAndHolder(t *testing.T) {
	change := AllocationChange{AccountID: "account", HolderID: "holder", ActorID: "actor", OperationID: "command", All: true}
	if err := change.Validate("reclaim"); err != nil {
		t.Fatal(err)
	}
	event := AllocationEvent{AccountID: change.AccountID, HolderID: change.HolderID, ActorID: change.ActorID, OperationID: change.OperationID, Kind: "reclaim", Amount: 17, RequestedAmount: 0, All: true}
	if !event.Same("reclaim", change) {
		t.Fatal("all request compared actual amount as request amount")
	}
	for _, mutate := range []func(*AllocationChange){func(c *AllocationChange) { c.Amount = 17; c.All = false }, func(c *AllocationChange) { c.HolderID = "other" }, func(c *AllocationChange) { c.ActorID = "other" }} {
		other := change
		mutate(&other)
		if event.Same("reclaim", other) {
			t.Fatal("changed command was replayed", other)
		}
	}
	if event.Same("allocate", change) {
		t.Fatal("changed action replayed")
	}
	if err := (AllocationChange{AccountID: "a", HolderID: "h", ActorID: "actor", OperationID: "op"}).Validate("release"); err != nil {
		t.Fatal("zero release must close a holder", err)
	}
	if err := change.Validate("allocate"); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestLedgerHolderIsPartOfDebitAndRefundIdempotency(t *testing.T) {
	change := Change{AccountID: "account", HolderID: "holder", Amount: 2, OperationID: "deduct", Source: "source", Subject: "actor", Reason: "reason"}
	entry := Entry{AccountID: change.AccountID, HolderID: change.HolderID, Amount: change.Amount, OperationID: change.OperationID, Source: change.Source, Subject: change.Subject, Reason: change.Reason, Kind: "deduct"}
	if err := change.Validate("deduct"); err != nil || !entry.Same("deduct", change) {
		t.Fatal(err)
	}
	change.HolderID = "other"
	if entry.Same("deduct", change) {
		t.Fatal("deduction changed holder")
	}
	refund := Change{HolderID: "holder", Amount: 2, OperationID: "refund", OriginalDeductionID: "original", Subject: "actor", Reason: "return"}
	returned := Entry{Kind: "refund", HolderID: "holder", Amount: 2, OperationID: "refund", OriginalDeductionID: "original", Subject: "actor", Reason: "return"}
	if err := refund.Validate("refund"); err != nil || !returned.Same("refund", refund) {
		t.Fatal(err)
	}
	refund.HolderID = ""
	if returned.Same("refund", refund) {
		t.Fatal("refund lost original holder")
	}
}
