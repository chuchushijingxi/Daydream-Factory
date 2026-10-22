package spaces

import (
	"context"
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/spaces"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func fundFixture(store *memoryStore, amount int64) {
	store.state.wallet = c.Wallet{Balance: amount, GiftBalance: amount}
}
func TestAllocationCommandsAreAtomicAndAuditOnlyOnce(t *testing.T) {
	ctx, s, store := fixture()
	fundFixture(store, 500)
	id := store.state.team.ID
	in := AllocationInput{CommandID: "allocate-001", Amount: 120}
	first, err := s.Allocate(ctx, actor("owner"), id, "member", in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Allocate(ctx, actor("owner"), id, "member", in)
	if err != nil || again.ID != first.ID || !again.Replayed {
		t.Fatal(again, err)
	}
	if store.state.allocations["member"].Balance != 120 || len(store.state.audit) != 1 || len(store.state.allocationEvents) != 1 {
		t.Fatal("replayed command changed holder or audit")
	}
	changed := in
	changed.Amount = 121
	if _, err = s.Allocate(ctx, actor("owner"), id, "member", changed); !hasCode(err, "CREDIT_IDEMPOTENCY_CONFLICT") {
		t.Fatal(err)
	}
	if _, err = s.Reclaim(ctx, actor("owner"), id, "member", in); !hasCode(err, "CREDIT_IDEMPOTENCY_CONFLICT") {
		t.Fatal("changed action used another operation key", err)
	}
	if _, err = s.Allocate(ctx, actor("admin"), id, "member", in); !hasCode(err, "CREDIT_IDEMPOTENCY_CONFLICT") {
		t.Fatal("changed actor used another operation key", err)
	}
	if err = store.SpaceTx(ctx, id, func(tx Tx) error { return tx.AddMember(ctx, "new-a", d.Member, s.now()) }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Allocate(ctx, actor("owner"), id, "new-a", in); !hasCode(err, "CREDIT_IDEMPOTENCY_CONFLICT") {
		t.Fatal("changed holder used another operation key", err)
	}
	store.failAudit = true
	if _, err = s.Allocate(ctx, actor("admin"), id, "member", AllocationInput{CommandID: "allocate-002", Amount: 20}); err == nil {
		t.Fatal("allocation ignored audit failure")
	}
	store.failAudit = false
	if store.state.allocations["member"].Balance != 120 || len(store.state.allocationEvents) != 1 {
		t.Fatal("allocation survived failed audit")
	}
	reclaim := AllocationInput{CommandID: "reclaim-all-001", All: true}
	receipt, err := s.Reclaim(ctx, actor("admin"), id, "member", reclaim)
	if err != nil || receipt.Amount != 120 {
		t.Fatal(receipt, err)
	}
	repeat, err := s.Reclaim(ctx, actor("admin"), id, "member", reclaim)
	if err != nil || repeat.ID != receipt.ID || repeat.Amount != 120 {
		t.Fatal("all replay recalculated amount", repeat, err)
	}
	if store.state.wallet.Balance != 500 || store.state.allocations["member"].Balance != 0 || len(store.state.audit) != 2 {
		t.Fatal("reclaim changed total balance or repeated audit")
	}
}
func TestOnlyOwnerAllocatesToAdminsAndNobodyAllocatesToOwner(t *testing.T) {
	ctx, s, store := fixture()
	fundFixture(store, 500)
	id := store.state.team.ID
	// 现有管理员没有名下行，分配时直接开立。
	delete(store.state.allocations, "admin")
	if _, err := s.Allocate(ctx, actor("owner"), id, "admin", AllocationInput{CommandID: "owner-to-admin", Amount: 80}); err != nil {
		t.Fatal(err)
	}
	if held := store.state.allocations["admin"]; !held.Open || held.Balance != 80 {
		t.Fatal("owner allocation to admin", held)
	}
	if _, err := s.Reclaim(ctx, actor("owner"), id, "admin", AllocationInput{CommandID: "owner-reclaim-admin", Amount: 30}); err != nil || store.state.allocations["admin"].Balance != 50 {
		t.Fatal("owner reclaim from admin", store.state.allocations["admin"], err)
	}
	for _, target := range []string{"admin", "owner"} {
		if _, err := s.Allocate(ctx, actor("admin"), id, target, AllocationInput{CommandID: "admin-to-" + target, Amount: 10}); !hasCode(err, "ROLE_FORBIDDEN") {
			t.Fatal("admin allocated to", target, err)
		}
	}
	if _, err := s.Allocate(ctx, actor("owner"), id, "owner", AllocationInput{CommandID: "owner-to-self", Amount: 10}); !hasCode(err, "ROLE_FORBIDDEN") {
		t.Fatal("owner allocated to self", err)
	}
	if _, err := s.Allocate(ctx, actor("admin"), id, "member", AllocationInput{CommandID: "admin-to-member", Amount: 10}); err != nil {
		t.Fatal("admin can still allocate to members", err)
	}
}
func TestRoleChangesKeepHolderWhileLeaveRemoveAndOwnershipRelease(t *testing.T) {
	ctx, s, store := fixture()
	fundFixture(store, 500)
	id := store.state.team.ID
	store.state.allocations["member"] = c.HolderAllocation{AccountID: id, HolderID: "member", Balance: 70, Open: true}
	// 成员与管理员都花名下分配额：升为管理员、再降回成员都保留本人积分。
	for _, role := range []d.Role{d.Admin, d.Member} {
		if err := s.SetRole(ctx, actor("owner"), id, "member", role); err != nil {
			t.Fatal(err)
		}
		if held := store.state.allocations["member"]; !held.Open || held.Balance != 70 || len(store.state.allocationEvents) != 0 {
			t.Fatal("role change moved holder balance", role, held)
		}
	}
	for _, action := range []string{"leave", "remove", "ownership", "admin-ownership"} {
		t.Run(action, func(t *testing.T) {
			ctx, s, store := fixture()
			fundFixture(store, 500)
			id := store.state.team.ID
			holder := "member"
			if action == "admin-ownership" {
				holder = "admin"
			}
			store.state.allocations[holder] = c.HolderAllocation{AccountID: id, HolderID: holder, Balance: 70, Open: true}
			var err error
			switch action {
			case "leave":
				err = s.Leave(ctx, actor("member"), id)
			case "remove":
				err = s.RemoveMember(ctx, actor("admin"), id, "member")
			case "ownership", "admin-ownership":
				var transfer OwnershipTransfer
				transfer, err = s.BeginOwnership(ctx, actor("owner"), id, holder)
				if err == nil {
					err = s.AcceptOwnership(ctx, actor(holder), id, transfer.ID)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			held := store.state.allocations[holder]
			if held.Open || held.Balance != 0 || store.state.wallet.Balance != 500 {
				t.Fatal("leave/removal/new owner retained holder balance", held)
			}
			if action == "ownership" || action == "admin-ownership" {
				// 原所有者降为管理员后从零开始持有名下分配额。
				if old := store.state.allocations["owner"]; !old.Open || old.Balance != 0 {
					t.Fatal("previous owner did not become an empty holder", old)
				}
			}
		})
	}
}
func TestFailedRemovalNotificationRestoresMemberAndAllocation(t *testing.T) {
	ctx, s, store := fixture()
	fundFixture(store, 100)
	id := store.state.team.ID
	store.state.allocations["member"] = c.HolderAllocation{AccountID: id, HolderID: "member", Balance: 70, Open: true}
	store.failNotify = true
	if err := s.RemoveMember(ctx, actor("admin"), id, "member"); err == nil {
		t.Fatal("failed notice ignored")
	}
	if store.state.members["member"].Role != d.Member || store.state.allocations["member"].Balance != 70 || !store.state.allocations["member"].Open || len(store.state.allocationEvents) != 0 {
		t.Fatal("failed member removal released holder")
	}
}
func TestTeamCreditWalletAndHistoryExposeOnlyMembersOwnFacts(t *testing.T) {
	ctx, s, store := fixture()
	ctx = ports.WithRequestedSpace(ctx, store.state.team.ID)
	id := store.state.team.ID
	fundFixture(store, 470)
	store.state.allocations["member"] = c.HolderAllocation{AccountID: id, HolderID: "member", Balance: 100, Open: true}
	store.state.allocations["other"] = c.HolderAllocation{AccountID: id, HolderID: "other", Balance: 60, Open: true}
	store.state.creditOperations["mine"] = c.Entry{ID: "mine", AccountID: id, HolderID: "member", Kind: "deduct", Amount: 20, Balance: 480}
	store.state.creditOperations["other"] = c.Entry{ID: "other", AccountID: id, HolderID: "other", Kind: "deduct", Amount: 10, Balance: 470}
	store.state.allocationEvents["mine"] = c.AllocationEvent{ID: "mine", AccountID: id, HolderID: "member", Kind: "allocate", Amount: 120}
	store.state.allocationEvents["other"] = c.AllocationEvent{ID: "other", AccountID: id, HolderID: "other", Kind: "allocate", Amount: 70}
	wallet, err := s.CreditWallet(ctx, actor("member"))
	if err != nil || wallet.Balance != 100 || wallet.TotalBalance != nil || wallet.Allocated != nil || wallet.Unallocated != nil {
		t.Fatal(wallet, err)
	}
	raw, _ := json.Marshal(wallet)
	if strings.Contains(string(raw), "totalBalance") || strings.Contains(string(raw), "allocated") {
		t.Fatal("member wallet exposed aggregate", string(raw))
	}
	owner, err := s.CreditWallet(ctx, actor("owner"))
	if err != nil || owner.Balance != 470 || *owner.Allocated != 160 || *owner.Unallocated != 310 {
		t.Fatal(owner, err)
	}
	// 管理员可用积分是本人名下分配额，同时保留团队三余额用于管理分配。
	store.state.allocations["admin"] = c.HolderAllocation{AccountID: id, HolderID: "admin", Balance: 30, Open: true}
	store.state.creditOperations["admin-use"] = c.Entry{ID: "admin-use", AccountID: id, HolderID: "admin", Kind: "deduct", Amount: 5, Balance: 465}
	admin, err := s.CreditWallet(ctx, actor("admin"))
	if err != nil || admin.Balance != 30 || *admin.TotalBalance != 470 || *admin.Allocated != 190 || *admin.Unallocated != 280 {
		t.Fatal(admin, err)
	}
	filter := c.HistoryFilter{Page: 1, PageSize: 10, Direction: "all", Category: "all"}
	history, err := s.CreditHistory(ctx, actor("member"), filter, false)
	if err != nil || len(history.Items) != 1 || history.Items[0].ID != "mine" || history.Allocations == nil || len(history.Allocations.Items) != 1 || history.Allocations.Items[0].ID != "mine" {
		t.Fatal(history, err)
	}
	mine, err := s.CreditHistory(ctx, actor("admin"), filter, true)
	if err != nil || len(mine.Items) != 1 || mine.Items[0].ID != "admin-use" || mine.Items[0].Balance != nil {
		t.Fatal("admin holder history", mine, err)
	}
	if err = s.RequirePersonalCredits(ctx, actor("member")); !hasCode(err, "DAILY_GIFT_PERSONAL_ONLY") {
		t.Fatal(err)
	}
}

type boundedRecentQueries struct {
	ports.CreditAllocationQueries
	ledgerCalls, eventCalls int
}

func (q *boundedRecentQueries) RecentHolderCreditEntries(context.Context, string, string) ([]c.Entry, error) {
	q.ledgerCalls++
	return []c.Entry{{ID: "recent"}}, nil
}
func (q *boundedRecentQueries) RecentAllocationEvents(context.Context, string, string) ([]c.AllocationEvent, error) {
	q.eventCalls++
	return []c.AllocationEvent{{ID: "event"}}, nil
}
func TestMemberRecentUsesBoundedQueriesWithoutSummaryOrCount(t *testing.T) {
	ctx, s, store := fixture()
	q := &boundedRecentQueries{}
	s.AllocationQueries = q
	ctx = ports.WithRequestedSpace(ctx, store.state.team.ID)
	recent, err := s.RecentCreditEntries(ctx, actor("member"))
	if err != nil || q.ledgerCalls != 1 || q.eventCalls != 1 || len(recent.Items) != 1 || len(recent.Allocations) != 1 {
		t.Fatal(recent, err)
	}
}

type requestLimiter struct {
	calls  int
	key    string
	limit  int
	window time.Duration
}

func (l *requestLimiter) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	l.calls++
	l.key, l.limit, l.window = key, limit, window
	return l.calls == 1, nil
}
func (l *requestLimiter) ResetLimit(context.Context, string) error { return nil }
func TestCreditRequestNotifiesAllManagersWithRemainingNeededAndTeamRateLimit(t *testing.T) {
	ctx, s, store := fixture()
	id := store.state.team.ID
	store.state.allocations["member"] = c.HolderAllocation{AccountID: id, HolderID: "member", Balance: 20, Open: true}
	limiter := &requestLimiter{}
	s.Limiter = limiter
	if err := s.RequestCredits(ctx, actor("member"), id, 90); err != nil {
		t.Fatal(err)
	}
	if len(store.state.notifications) != 2 || limiter.limit != 1 || limiter.window != 5*time.Minute || !strings.Contains(limiter.key, id) || !strings.Contains(limiter.key, "member") {
		t.Fatal(limiter, store.state.notifications)
	}
	for _, message := range store.state.notifications {
		if !strings.Contains(message.Body, "member") || !strings.Contains(message.Body, "20") || !strings.Contains(message.Body, "90") {
			t.Fatal(message)
		}
	}
	if err := s.RequestCredits(ctx, actor("member"), id, 90); !hasCode(err, "RATE_LIMITED") {
		t.Fatal(err)
	}
	if len(store.state.notifications) != 2 {
		t.Fatal("rate-limited request sent notice")
	}
	if err := s.RequestCredits(ctx, actor("owner"), id, 90); !hasCode(err, "ROLE_FORBIDDEN") {
		t.Fatal("owner spends unallocated credits and has nobody to ask", err)
	}
	// 只有所有者能给管理员分配，管理员的申请只通知所有者。
	s.Limiter = &requestLimiter{}
	if err := s.RequestCredits(ctx, actor("admin"), id, 40); err != nil {
		t.Fatal(err)
	}
	if latest := store.state.notifications[2:]; len(latest) != 1 || latest[0].RecipientUserID != "owner" {
		t.Fatal("admin request reached someone other than the owner", latest)
	}
}
func TestExpiredReadOnlyUsesTotalIncludingAllAllocations(t *testing.T) {
	ctx, s, store := fixture()
	id := store.state.team.ID
	store.state.team.ExpiresAt = s.now().Add(-48 * time.Hour)
	fundFixture(store, 100)
	store.state.allocations["member"] = c.HolderAllocation{AccountID: id, HolderID: "member", Balance: 100, Open: true}
	if _, err := s.Schedule(ctx, s.now(), 20); err != nil {
		t.Fatal(err)
	}
	if len(store.state.notifications) != 0 {
		t.Fatal("fully allocated positive balance became read only")
	}
	store.state.wallet = c.Wallet{}
	store.state.allocations["member"] = c.HolderAllocation{AccountID: id, HolderID: "member", Open: true}
	if count, err := s.Schedule(ctx, s.now(), 20); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if len(store.state.notifications) != 2 {
		t.Fatal("read-only notice omitted managers")
	}
	if count, err := s.Schedule(ctx, s.now(), 20); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	scope, err := s.CreditScope(ports.WithRequestedSpace(ctx, id), actor("member"))
	if err != nil || !scope.ReadOnly {
		t.Fatal(scope, err)
	}
	if _, err = ports.ChargeAccount(scope); err == nil {
		t.Fatal("read-only scope could start generation")
	}
	if err = s.Leave(ctx, actor("member"), id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreditScope(ports.WithRequestedSpace(ctx, id), actor("member")); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("removed member retained credit access", err)
	}
}

type accountCreditQueries struct {
	ports.CreditQueries
	entries []c.Entry
}

func (q accountCreditQueries) CreditHistory(_ context.Context, _ string, f c.HistoryFilter) (c.History, error) {
	return c.History{Items: q.entries, Total: int64(len(q.entries)), Page: f.Page, PageSize: f.PageSize}, nil
}
func (q accountCreditQueries) RecentCreditEntries(context.Context, string) ([]c.Entry, error) {
	return q.entries, nil
}
func TestMemberLedgerViewsOmitAccountBalanceAndManagerViewsKeepTruth(t *testing.T) {
	ctx, s, store := fixture()
	ctx = ports.WithRequestedSpace(ctx, store.state.team.ID)
	id := store.state.team.ID
	entry := c.Entry{ID: "private-balance", AccountID: id, HolderID: "member", Kind: "deduct", Amount: 20, Balance: 480}
	store.state.creditOperations[entry.ID] = entry
	s.CreditQueries = accountCreditQueries{entries: []c.Entry{entry, {ID: "later", Balance: 470}}}
	filter := c.HistoryFilter{Page: 1, PageSize: 10, Category: "all", Direction: "all"}
	history, err := s.CreditHistory(ctx, actor("member"), filter, false)
	if err != nil {
		t.Fatal(err)
	}
	recent, err := s.RecentCreditEntries(ctx, actor("member"))
	if err != nil {
		t.Fatal(err)
	}
	for _, items := range [][]CreditEntryView{history.Items, recent.Items} {
		if len(items) != 1 || items[0].Balance != nil {
			t.Fatal("member received account historical balance", items)
		}
		raw, _ := json.Marshal(items)
		var decoded []map[string]json.RawMessage
		if err = json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if _, exists := decoded[0]["balance"]; exists {
			t.Fatal("member ledger JSON exposes account balance", string(raw))
		}
	}
	if store.state.creditOperations[entry.ID].Balance != 480 {
		t.Fatal("privacy projection rewrote ledger balance")
	}
	manager, err := s.CreditHistory(ctx, actor("admin"), filter, false)
	if err != nil || len(manager.Items) != 2 || manager.Items[0].Balance == nil || *manager.Items[0].Balance != 480 || manager.Items[1].Balance == nil || *manager.Items[1].Balance != 470 {
		t.Fatal("manager lost historical account balances", manager, err)
	}
	managerRecent, err := s.RecentCreditEntries(ctx, actor("owner"))
	if err != nil || managerRecent.Items[0].Balance == nil || *managerRecent.Items[0].Balance != 480 {
		t.Fatal(managerRecent, err)
	}
}
