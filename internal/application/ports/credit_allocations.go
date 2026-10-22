package ports

import (
	"context"
	"drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
)

type CreditAllocations interface {
	Allocate(context.Context, credits.AllocationChange) (credits.AllocationEvent, error)
	Reclaim(context.Context, credits.AllocationChange) (credits.AllocationEvent, error)
	Release(context.Context, credits.AllocationChange) (credits.AllocationEvent, error)
}
type CreditAllocationQueries interface {
	RecentHolderCreditEntries(context.Context, string, string) ([]credits.Entry, error)
	RecentAllocationEvents(context.Context, string, string) ([]credits.AllocationEvent, error)
	CreditBalances(context.Context, string) (credits.Balances, error)
	HolderCredits(context.Context, string, string) (credits.HolderAllocation, error)
	AllocationHistory(context.Context, string, string, credits.HistoryFilter) (Page[credits.AllocationEvent], error)
	HolderCreditHistory(context.Context, string, string, credits.HistoryFilter) (credits.History, error)
}
type CreditDiscounts interface {
	CreditDiscount(context.Context, string, string) (int, error)
}
type CreditTarget struct{ AccountID, HolderID, ActorID string }

// ChargeAccount 只消费经过空间解析的可信Scope；积分核心不认识这些团队角色。
func ChargeAccount(scope SpaceScope) (CreditTarget, error) {
	if scope.UserID == "" || scope.SpaceID == "" {
		return CreditTarget{}, fault.ErrNotFound
	}
	target := CreditTarget{AccountID: scope.SpaceID, ActorID: scope.UserID}
	switch scope.Kind {
	case spaces.Personal:
		if scope.SpaceID != scope.UserID {
			return CreditTarget{}, fault.ErrNotFound
		}
	case spaces.TeamKind:
		if scope.Status == spaces.Disabled || !scope.Role.Valid() {
			return CreditTarget{}, fault.ErrNotFound
		}
		if scope.ReadOnly {
			return CreditTarget{}, fault.New("TEAM_READ_ONLY", "团队已只读")
		}
		if scope.Role.HoldsCredits() {
			target.HolderID = scope.UserID
		}
	default:
		return CreditTarget{}, fault.ErrNotFound
	}
	return target, nil
}
