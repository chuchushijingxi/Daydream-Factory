package spaces

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	n "drama-agent/internal/domain/notifications"
	d "drama-agent/internal/domain/spaces"
)

func (s *Service) CreditScope(ctx context.Context, a ports.Actor) (ports.SpaceScope, error) {
	if err := a.Require(); err != nil {
		return ports.SpaceScope{}, err
	}
	if s.Resolver == nil {
		return ports.SpaceScope{}, fault.New("SPACE_UNAVAILABLE", "空间权限服务尚未组装")
	}
	return s.Resolver.Resolve(ctx, a, ports.RequestedSpace(ctx))
}
func (s *Service) RequirePersonalCredits(ctx context.Context, a ports.Actor) error {
	scope, err := s.CreditScope(ctx, a)
	if err != nil {
		return err
	}
	if scope.Kind != d.Personal {
		return fault.New("DAILY_GIFT_PERSONAL_ONLY", "团队空间不能领取每日赠送")
	}
	return nil
}
func (s *Service) CreditWallet(ctx context.Context, a ports.Actor) (CreditWalletView, error) {
	scope, err := s.CreditScope(ctx, a)
	if err != nil {
		return CreditWalletView{}, err
	}
	out := CreditWalletView{SpaceID: scope.SpaceID, Role: scope.Role}
	if scope.Kind == d.Personal {
		if s.CreditQueries == nil {
			return out, fault.New("CREDIT_UNAVAILABLE", "积分查询尚未组装")
		}
		wallet, e := s.CreditQueries.CreditWallet(ctx, scope.SpaceID)
		out.Balance = wallet.Balance
		return out, e
	}
	if s.AllocationQueries == nil {
		return out, fault.New("CREDIT_UNAVAILABLE", "积分分配查询尚未组装")
	}
	// 成员与管理员的可用积分都是本人名下分配额；管理员另带团队三余额，用于在团队页管理分配。
	if scope.Role.HoldsCredits() {
		holder, e := s.AllocationQueries.HolderCredits(ctx, scope.SpaceID, a.UserID)
		if e != nil {
			return out, e
		}
		if holder.Open {
			out.Balance = holder.Balance
		}
		if scope.Role == d.Member {
			return out, nil
		}
	}
	balances, err := s.AllocationQueries.CreditBalances(ctx, scope.SpaceID)
	if err != nil {
		return out, err
	}
	unallocated, err := balances.Unallocated()
	if err != nil {
		return out, err
	}
	if scope.Role == d.Owner {
		out.Balance = balances.Wallet.Balance
	}
	out.TotalBalance = &balances.Wallet.Balance
	out.Allocated = &balances.Allocated
	out.Unallocated = &unallocated
	return out, nil
}

// mine 只对管理员有意义：团队页读团队流水，本人积分明细读名下流水；成员始终只有名下流水。
func (s *Service) CreditHistory(ctx context.Context, a ports.Actor, filter c.HistoryFilter, mine bool) (CreditHistoryView, error) {
	scope, err := s.CreditScope(ctx, a)
	if err != nil {
		return CreditHistoryView{}, err
	}
	if err = filter.Validate(); err != nil {
		return CreditHistoryView{}, err
	}
	if scope.Kind == d.TeamKind && (scope.Role == d.Member || mine && scope.Role.HoldsCredits()) {
		if s.AllocationQueries == nil {
			return CreditHistoryView{}, fault.New("CREDIT_UNAVAILABLE", "积分分配查询尚未组装")
		}
		history, e := s.AllocationQueries.HolderCreditHistory(ctx, scope.SpaceID, a.UserID, filter)
		if e != nil {
			return CreditHistoryView{}, e
		}
		out := creditHistoryView(history, false)
		if filter.Direction == "all" && filter.Category == "all" {
			events, e := s.AllocationQueries.AllocationHistory(ctx, scope.SpaceID, a.UserID, filter)
			if e != nil {
				return CreditHistoryView{}, e
			}
			out.Allocations = &events
		}
		return out, nil
	}
	if s.CreditQueries == nil {
		return CreditHistoryView{}, fault.New("CREDIT_UNAVAILABLE", "积分查询尚未组装")
	}
	history, err := s.CreditQueries.CreditHistory(ctx, scope.SpaceID, filter)
	return creditHistoryView(history, true), err
}
func (s *Service) RecentCreditEntries(ctx context.Context, a ports.Actor) (CreditRecentView, error) {
	scope, err := s.CreditScope(ctx, a)
	if err != nil {
		return CreditRecentView{}, err
	}
	// 最近记录服务顶栏的本人余额：持有分配额的角色看名下流水与分配事件。
	if scope.Kind == d.TeamKind && scope.Role.HoldsCredits() {
		if s.AllocationQueries == nil {
			return CreditRecentView{}, fault.New("CREDIT_UNAVAILABLE", "积分分配查询尚未组装")
		}
		items, e := s.AllocationQueries.RecentHolderCreditEntries(ctx, scope.SpaceID, a.UserID)
		if e != nil {
			return CreditRecentView{}, e
		}
		events, e := s.AllocationQueries.RecentAllocationEvents(ctx, scope.SpaceID, a.UserID)
		if e != nil {
			return CreditRecentView{}, e
		}
		return CreditRecentView{Items: creditEntryViews(items, false), Allocations: events}, nil
	}
	if s.CreditQueries == nil {
		return CreditRecentView{}, fault.New("CREDIT_UNAVAILABLE", "积分查询尚未组装")
	}
	items, err := s.CreditQueries.RecentCreditEntries(ctx, scope.SpaceID)
	return CreditRecentView{Items: creditEntryViews(items, true)}, err
}
func (s *Service) WatchCredits(ctx context.Context, a ports.Actor) (<-chan struct{}, func(), error) {
	scope, err := s.CreditScope(ctx, a)
	if err != nil {
		return nil, nil, err
	}
	if s.CreditUpdates == nil {
		return nil, nil, fault.New("CREDIT_UNAVAILABLE", "积分更新尚未组装")
	}
	return s.CreditUpdates.WatchCreditAccount(ctx, scope.SpaceID)
}
func allocationOperation(space, command string) string {
	sum := sha256.Sum256([]byte(space + "\x00" + command))
	return "space-allocation:" + hex.EncodeToString(sum[:])
}
func (s *Service) Allocate(ctx context.Context, a ports.Actor, space, holder string, in AllocationInput) (c.AllocationEvent, error) {
	return s.allocate(ctx, a, space, holder, in, false)
}
func (s *Service) Reclaim(ctx context.Context, a ports.Actor, space, holder string, in AllocationInput) (c.AllocationEvent, error) {
	return s.allocate(ctx, a, space, holder, in, true)
}
func (s *Service) allocate(ctx context.Context, a ports.Actor, space, holder string, in AllocationInput, reclaim bool) (c.AllocationEvent, error) {
	if !validID(in.CommandID) || len(in.CommandID) < 8 || !validID(holder) {
		return c.AllocationEvent{}, invalid()
	}
	kind, action := "allocate", d.AllocateCredits
	if reclaim {
		kind, action = "reclaim", d.ReclaimCredits
	}
	change := c.AllocationChange{AccountID: space, HolderID: holder, ActorID: a.UserID, OperationID: allocationOperation(space, in.CommandID), Amount: in.Amount, All: in.All}
	if err := change.Validate(kind); err != nil {
		return c.AllocationEvent{}, err
	}
	var event c.AllocationEvent
	err := s.withTeam(ctx, a, space, action, func(tx Tx, _ d.Team, actor Member) error {
		member, e := tx.Member(ctx, holder)
		if e != nil {
			return e
		}
		if !actor.Role.CanAllocateTo(member.Role) {
			return forbidden()
		}
		if reclaim {
			event, e = tx.ReclaimCredits(ctx, change)
		} else {
			event, e = tx.AllocateCredits(ctx, change)
		}
		if e != nil {
			if f, ok := e.(*fault.Error); ok && f.Code == "CREDIT_UNALLOCATED_INSUFFICIENT" {
				return fault.New("TEAM_UNALLOCATED_INSUFFICIENT", "团队未分配积分不足")
			}
			return e
		}
		if event.Replayed {
			return nil
		}
		return audit(ctx, tx, a, space, "credits_"+kind, holder, map[string]any{"operationId": change.OperationID, "amount": event.Amount, "all": in.All})
	})
	return event, err
}
func releaseHolder(ctx context.Context, tx Tx, a ports.Actor, space, holder string) error {
	_, err := tx.ReleaseCredits(ctx, c.AllocationChange{AccountID: space, HolderID: holder, ActorID: a.UserID, OperationID: "space-release:" + identity.New("release")})
	return err
}
func (s *Service) RequestCredits(ctx context.Context, a ports.Actor, space string, needed int64) error {
	if err := c.ValidAmount(needed); err != nil {
		return err
	}
	return s.withTeam(ctx, a, space, d.ViewMembers, func(tx Tx, team d.Team, member Member) error {
		if !member.Role.HoldsCredits() {
			return forbidden()
		}
		if s.Limiter == nil {
			return fault.New("RUNTIME_UNAVAILABLE", "积分申请限流服务尚未组装")
		}
		allowed, err := s.Limiter.Allow(ctx, "team:credit-request:"+space+":"+a.UserID, 1, 5*time.Minute)
		if err != nil {
			return err
		}
		if !allowed {
			return fault.New("RATE_LIMITED", "请在5分钟后再次申请积分")
		}
		holder, err := tx.HolderCredits(ctx, space, a.UserID)
		if err != nil {
			return err
		}
		user, err := tx.FindUser(ctx, a.UserID)
		if err != nil {
			return err
		}
		remaining := holder.Balance
		if !holder.Open {
			remaining = 0
		}
		body := n.EscapeMarkdownText(team.Name) + "：" + n.EscapeMarkdownText(user.Name) + "申请积分，当前剩余" + strconv.FormatInt(remaining, 10) + "，所需" + strconv.FormatInt(needed, 10) + "。"
		operation := "team-credit-request:" + identity.New("request")
		// 只有所有者能给管理员分配积分，管理员的申请只通知所有者。
		if member.Role == d.Admin {
			return notify(ctx, tx, team.OwnerID, operation, "收到团队积分申请", body, n.Link{Type: "team", SpaceID: space})
		}
		return notifyManagers(ctx, tx, operation, "收到团队积分申请", body, n.Link{Type: "team", SpaceID: space})
	})
}
