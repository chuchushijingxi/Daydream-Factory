package spaces

import (
	"context"
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/spaces"
	"sort"
	"time"
)

func (tx *memoryTx) EnsureCreditAccount(context.Context, string) error { return nil }
func (tx *memoryTx) AddCredits(_ context.Context, change c.Change) (c.Entry, error) {
	if old, ok := tx.state.creditOperations[change.OperationID]; ok {
		if !old.Same("add", change) {
			return old, fault.New("CREDIT_IDEMPOTENCY_CONFLICT", "账务编号冲突")
		}
		return old, nil
	}
	funding, err := c.Funding(change.Amount, change.Category)
	if err != nil {
		return c.Entry{}, err
	}
	wallet, err := tx.state.wallet.Credit(funding)
	if err != nil {
		return c.Entry{}, err
	}
	tx.state.wallet = wallet
	entry := c.Entry{ID: change.OperationID, OperationID: change.OperationID, AccountID: change.AccountID, Amount: change.Amount, Balance: wallet.Balance, Kind: "add", Category: change.Category, Source: change.Source, Subject: change.Subject, Reason: change.Reason, GiftAmount: funding.Gift, RechargeAmount: funding.Recharge, CreatedAt: tx.store.at}
	tx.state.creditOperations[change.OperationID] = entry
	return entry, nil
}
func (tx *memoryTx) CreditBalances(context.Context, string) (c.Balances, error) {
	balances := c.Balances{Wallet: tx.state.wallet}
	for _, holder := range tx.state.allocations {
		if holder.Open {
			balances.Allocated += holder.Balance
		}
	}
	return balances, nil
}
func (tx *memoryTx) HolderCredits(_ context.Context, account, holder string) (c.HolderAllocation, error) {
	h, ok := tx.state.allocations[holder]
	if !ok {
		h = c.HolderAllocation{AccountID: account, HolderID: holder}
	}
	return h, nil
}
func (tx *memoryTx) AllocateCredits(ctx context.Context, change c.AllocationChange) (c.AllocationEvent, error) {
	return tx.allocation(ctx, "allocate", change)
}
func (tx *memoryTx) ReclaimCredits(ctx context.Context, change c.AllocationChange) (c.AllocationEvent, error) {
	return tx.allocation(ctx, "reclaim", change)
}
func (tx *memoryTx) ReleaseCredits(ctx context.Context, change c.AllocationChange) (c.AllocationEvent, error) {
	return tx.allocation(ctx, "release", change)
}
func (tx *memoryTx) allocation(ctx context.Context, kind string, change c.AllocationChange) (c.AllocationEvent, error) {
	if err := change.Validate(kind); err != nil {
		return c.AllocationEvent{}, err
	}
	if old, ok := tx.state.allocationEvents[change.OperationID]; ok {
		if !old.Same(kind, change) {
			return old, fault.New("CREDIT_IDEMPOTENCY_CONFLICT", "分配编号冲突")
		}
		old.Replayed = true
		return old, nil
	}
	held, err := tx.HolderCredits(ctx, change.AccountID, change.HolderID)
	if err != nil {
		return c.AllocationEvent{}, err
	}
	amount := change.Amount
	switch kind {
	case "allocate":
		balances, _ := tx.CreditBalances(ctx, change.AccountID)
		available, e := balances.Unallocated()
		if e != nil {
			return c.AllocationEvent{}, e
		}
		held, err = held.Allocate(available, amount)
	case "reclaim":
		held, amount, err = held.Reclaim(amount, change.All)
	case "release":
		held, amount, err = held.Release()
	}
	if err != nil {
		return c.AllocationEvent{}, err
	}
	tx.state.allocations[change.HolderID] = held
	event := c.AllocationEvent{ID: change.OperationID, OperationID: change.OperationID, AccountID: change.AccountID, HolderID: change.HolderID, ActorID: change.ActorID, Kind: kind, Amount: amount, Balance: held.Balance, RequestedAmount: change.Amount, All: change.All, CreatedAt: tx.store.at}
	tx.state.allocationEvents[change.OperationID] = event
	return event, nil
}
func (tx *memoryTx) GrantSchedule(context.Context) (d.GrantSchedule, error) {
	return tx.state.grantSchedule, nil
}
func (tx *memoryTx) SaveGrantSchedule(_ context.Context, schedule d.GrantSchedule) error {
	tx.state.grantSchedule = schedule
	return nil
}
func (s *memoryStore) RunDueTeamGrants(ctx context.Context, at time.Time, limit int, fn func(Tx) error) (int, error) {
	count := 0
	for count < limit {
		if s.state.team.Status(at) != d.Active || s.state.grantSchedule.NextAt.IsZero() || s.state.grantSchedule.NextAt.After(at) {
			break
		}
		if err := s.SpaceTx(ctx, s.state.team.ID, fn); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
func (s *memoryStore) CreditBalances(ctx context.Context, account string) (c.Balances, error) {
	return (&memoryTx{state: &s.state}).CreditBalances(ctx, account)
}
func (s *memoryStore) HolderCredits(ctx context.Context, account, holder string) (c.HolderAllocation, error) {
	return (&memoryTx{state: &s.state}).HolderCredits(ctx, account, holder)
}
func (s *memoryStore) AllocationHistory(_ context.Context, account, holder string, filter c.HistoryFilter) (ports.Page[c.AllocationEvent], error) {
	out := ports.Page[c.AllocationEvent]{Items: []c.AllocationEvent{}, Page: filter.Page, PageSize: filter.PageSize}
	for _, event := range s.state.allocationEvents {
		if event.AccountID == account && (holder == "" || event.HolderID == holder) && (filter.Start == nil || !event.CreatedAt.Before(*filter.Start)) && (filter.End == nil || event.CreatedAt.Before(*filter.End)) {
			out.Items = append(out.Items, event)
		}
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].CreatedAt.After(out.Items[j].CreatedAt) })
	out.Total = int64(len(out.Items))
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(out.Items) {
		out.Items = []c.AllocationEvent{}
	} else {
		end := min(len(out.Items), start+filter.PageSize)
		out.Items = out.Items[start:end]
	}
	return out, nil
}
func (s *memoryStore) HolderCreditHistory(_ context.Context, account, holder string, filter c.HistoryFilter) (c.History, error) {
	out := c.History{Items: []c.Entry{}, Page: filter.Page, PageSize: filter.PageSize}
	for _, entry := range s.state.creditOperations {
		if entry.AccountID == account && entry.HolderID == holder {
			out.Items = append(out.Items, entry)
		}
	}
	out.Total = int64(len(out.Items))
	return out, nil
}
func (s *memoryStore) RecentHolderCreditEntries(ctx context.Context, account, holder string) ([]c.Entry, error) {
	history, err := s.HolderCreditHistory(ctx, account, holder, c.HistoryFilter{Page: 1, PageSize: 5, Direction: "all", Category: "all"})
	return history.Items, err
}
func (s *memoryStore) RecentAllocationEvents(ctx context.Context, account, holder string) ([]c.AllocationEvent, error) {
	events, err := s.AllocationHistory(ctx, account, holder, c.HistoryFilter{Page: 1, PageSize: 5, Direction: "all", Category: "all"})
	return events.Items, err
}
