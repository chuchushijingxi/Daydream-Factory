package postgres_test

import (
	"context"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/project"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCreditCategoriesPriorityRefundsAndConcurrentLimits(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	account := identity.New("category_account")
	grant := func(category string, amount int64) c.Entry {
		t.Helper()
		e, err := s.Add(ctx, c.Change{AccountID: account, Category: category, Amount: amount, OperationID: identity.New("grant"), Source: "fixture", Subject: "operator", Reason: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	grant(c.Recharge, 100)
	first := grant(c.Gift, 40)
	changed := c.Change{AccountID: account, Category: c.Recharge, Amount: first.Amount, OperationID: first.OperationID, Source: first.Source, Subject: first.Subject, Reason: first.Reason}
	if _, err := s.Add(ctx, changed); !codeIs(err, "CREDIT_IDEMPOTENCY_CONFLICT") {
		t.Fatal("reclassified same grant", err)
	}
	debit, err := s.Deduct(ctx, c.Change{AccountID: account, Amount: 100, OperationID: identity.New("debit"), Source: "fixture", Subject: account, Reason: "fixture"})
	if err != nil || debit.Category != c.Mixed || debit.GiftAmount != 40 || debit.RechargeAmount != 60 {
		t.Fatal("gift was not spent first", debit, err)
	}
	refund := c.Change{OriginalDeductionID: debit.ID, Amount: 50, OperationID: identity.New("refund"), Subject: account, Reason: "fixture"}
	part, err := s.Refund(ctx, refund)
	if err != nil || part.RechargeAmount != 50 || part.GiftAmount != 0 {
		t.Fatal("partial refund did not restore recorded allocation", part, err)
	}
	again, err := s.Refund(ctx, refund)
	if err != nil || again.ID != part.ID {
		t.Fatal("refund idempotency", again, err)
	}
	refund.Amount = 40
	refund.OperationID = identity.New("refund")
	part, err = s.Refund(ctx, refund)
	if err != nil || part.RechargeAmount != 10 || part.GiftAmount != 30 {
		t.Fatal("partial refund did not cross original buckets", part, err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Refund(ctx, c.Change{OriginalDeductionID: debit.ID, Amount: 2, OperationID: identity.New("refund"), Subject: account, Reason: "fixture"}); e == nil {
				successes.Add(1)
			} else if !codeIs(e, "CREDIT_REFUND_EXCEEDED") {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	wallet, err := s.CreditWallet(ctx, account)
	if err != nil || successes.Load() != 5 || wallet != (c.Wallet{Balance: 140, RechargeBalance: 100, GiftBalance: 40}) {
		t.Fatal("concurrent refund cap/allocation", successes.Load(), wallet, err)
	}
	var bad int
	err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND (amount<>recharge_amount+gift_amount+legacy_amount OR amount<=0)`, account).Scan(&bad)
	if err != nil || bad != 0 {
		t.Fatal("ledger allocation mismatch", bad, err)
	}
	if _, err = s.Add(ctx, c.Change{AccountID: account, Amount: c.MaxAmount, Category: c.Gift, OperationID: identity.New("overflow"), Source: "fixture", Subject: "operator", Reason: "fixture"}); !codeIs(err, "CREDIT_INVALID_AMOUNT") {
		t.Fatal("balance overflow", err)
	}
	if _, err = s.Deduct(ctx, c.Change{AccountID: account, Category: c.Gift, Amount: 1, OperationID: identity.New("invalid_category"), Source: "fixture", Subject: account, Reason: "fixture"}); !codeIs(err, "CREDIT_INVALID_CATEGORY") {
		t.Fatal("caller selected a debit category", err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	afterMigration, err := s.CreditWallet(ctx, account)
	if err != nil || afterMigration != wallet {
		t.Fatal("repeated migration reclassified existing balance", afterMigration, wallet, err)
	}
}

func TestCreditHistoryUsesActualCompletionFiltersAndRefundFacts(t *testing.T) {
	s, _ := identityDB(t)
	ctx := context.Background()
	account := identity.New("history_account")
	var earliest c.Entry
	for _, category := range []string{c.Gift, c.Recharge} {
		e, err := s.Add(ctx, c.Change{AccountID: account, Category: category, Amount: 300, OperationID: identity.New("grant"), Source: "fixture", Subject: "operator", Reason: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		if earliest.ID == "" {
			earliest = e
		}
	}
	projectID := identity.New("private_project")
	completeID, pendingID, failedID := identity.New("charge"), identity.New("charge"), identity.New("charge")
	project := p.Project{ID: projectID, OwnerID: account, Revision: 1, Title: "PRIVATE TITLE MUST NOT ENTER ANALYSIS", Runs: []p.Run{
		{ID: completeID, Credit: &c.Charge{ID: completeID}, Status: p.RunCompleted},
		{ID: identity.New("continuation"), Credit: &c.Charge{ID: completeID}, Status: p.RunCompleted},
		{ID: pendingID, Credit: &c.Charge{ID: pendingID}, Status: p.RunRunning},
		{ID: failedID, Credit: &c.Charge{ID: failedID}, Status: p.RunFailed},
	}}
	if err := s.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	debit := func(id string, amount int64, source string) c.Entry {
		t.Helper()
		e, err := s.Deduct(ctx, c.Change{AccountID: account, Amount: amount, OperationID: "generation:" + id + ":deduct", Source: source, Subject: account, Reason: "generation"})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	complete := debit(completeID, 100, projectID)
	debit(pendingID, 80, projectID)
	failed := debit(failedID, 60, projectID)
	debit(identity.New("operator_debit"), 30, "operations-adjustment")
	filter := c.HistoryFilter{Page: 1, PageSize: 50, Direction: "all", Category: "all"}
	history, err := s.CreditHistory(ctx, account, filter)
	if err != nil || history.Total != 6 || history.Summary != (c.HistorySummary{Acquired: 600, Consumed: 270, NetConsumed: 270, SettledConsumed: 130, Pending: 140}) {
		t.Fatal("failed/pending debit treated as settled", history, err)
	}
	var firstRefund c.Entry
	for _, v := range []struct {
		original c.Entry
		amount   int64
	}{{failed, 60}, {complete, 20}} {
		refunded, e := s.Refund(ctx, c.Change{OriginalDeductionID: v.original.ID, Amount: v.amount, OperationID: identity.New("refund"), Subject: account, Reason: "fixture"})
		if e != nil {
			t.Fatal(e)
		}
		if firstRefund.ID == "" {
			firstRefund = refunded
		}
	}
	filter.End = &firstRefund.CreatedAt
	cohort, err := s.CreditHistory(ctx, account, filter)
	if err != nil || cohort.Total != 6 || cohort.Summary.Refunded != 0 || cohort.Summary.NetConsumed != 190 || cohort.Summary.SettledConsumed != 110 {
		t.Fatal("later actual refund incorrectly left earlier debit consumed", cohort, err)
	}
	filter.End = nil
	history, err = s.CreditHistory(ctx, account, filter)
	if err != nil || history.Total != 8 || history.Summary != (c.HistorySummary{Acquired: 600, Consumed: 270, Refunded: 80, NetConsumed: 190, SettledConsumed: 110, Pending: 80}) || len(history.Analysis) != 2 {
		t.Fatal("actual refunds not projected", history, err)
	}
	if history.Analysis[0] != (c.ConsumptionAnalysis{Source: "language-generation", Amount: 80}) || history.Analysis[1] != (c.ConsumptionAnalysis{Source: "operations-adjustment", Amount: 30}) {
		t.Fatal("multi-Run duplicate charge or wrong source", history.Analysis)
	}
	raw, _ := json.Marshal(history.Analysis)
	if strings.Contains(string(raw), projectID) || strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("private project data entered analysis", string(raw))
	}
	filter.Direction, filter.PageSize = "consumed", 2
	first, err := s.CreditHistory(ctx, account, filter)
	filter.Page = 2
	second, err2 := s.CreditHistory(ctx, account, filter)
	if err != nil || err2 != nil || len(first.Items) != 2 || len(second.Items) != 2 || first.Total != 6 || first.Summary != second.Summary || first.Items[0].ID == second.Items[0].ID {
		t.Fatal("pagination/statistics inconsistent", first, second, err, err2)
	}
	filter.Page, filter.PageSize, filter.Category, filter.Direction = 1, 50, c.Recharge, "all"
	recharged, err := s.CreditHistory(ctx, account, filter)
	if err != nil || recharged.Total != 1 || recharged.Summary.Acquired != 300 || recharged.Summary.Consumed != 0 {
		t.Fatal("category filter ignored allocation", recharged, err)
	}
	filter.Category, filter.End = "all", &earliest.CreatedAt
	before, err := s.CreditHistory(ctx, account, filter)
	if err != nil || before.Total != 0 {
		t.Fatal("exclusive end boundary", before, err)
	}
	filter.End, filter.Start = nil, &earliest.CreatedAt
	after, err := s.CreditHistory(ctx, account, filter)
	if err != nil || after.Total != 8 {
		t.Fatal("inclusive start boundary", after, err)
	}
	empty, err := s.CreditHistory(ctx, identity.New("other_account"), filter)
	if err != nil || empty.Total != 0 || len(empty.Analysis) != 0 {
		t.Fatal("cross-account query", empty, err)
	}
}

func TestCreditHistoryRejectsUnsafeLifetimeTotals(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	account := identity.New("history_overflow")
	for i := 0; i < 2; i++ {
		if _, err := s.Add(ctx, c.Change{AccountID: account, Category: c.Gift, Amount: c.MaxAmount, OperationID: identity.New("grant"), Source: "fixture", Subject: "operator", Reason: "fixture"}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := s.Deduct(ctx, c.Change{AccountID: account, Amount: c.MaxAmount, OperationID: identity.New("debit"), Source: "fixture", Subject: account, Reason: "fixture"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.CreditHistory(ctx, account, c.HistoryFilter{Page: 1, PageSize: 20, Category: "all", Direction: "all"}); !codeIs(err, "CREDIT_TOTAL_OVERFLOW") {
		t.Fatal("unsafe integer aggregate returned", err)
	}
	// Every entry and running balance remains valid, but repeated grant/spend
	// cycles can also overflow PG's BIGINT conversion of a lifetime sum.
	large := identity.New("history_pg_overflow")
	if _, err := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_accounts(account_id) VALUES($1)`, large); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_ledger(id,operation_id,account_id,kind,amount,balance,source,subject,reason,category,gift_amount,created_at)
 SELECT $1||':'||i,$1||':'||i,$1,CASE WHEN i%2=1 THEN 'add' ELSE 'deduct' END,$2::bigint,CASE WHEN i%2=1 THEN $2::bigint ELSE 0 END,'fixture','operator','valid repeated grant/spend fixture','gift',$2::bigint,'2020-01-01'::timestamptz+i*interval '1 microsecond' FROM generate_series(1,2050) i`, large, c.MaxAmount); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreditHistory(ctx, large, c.HistoryFilter{Page: 1, PageSize: 20, Category: "all", Direction: "all"}); !codeIs(err, "CREDIT_TOTAL_OVERFLOW") {
		t.Fatal("PG numeric overflow was not mapped", err)
	}
}

func TestCreditLegacyFactsRemainUnclassifiedAndCannotBorrowCompletion(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	account := identity.New("legacy_account")
	if _, err := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_accounts(account_id,balance,legacy_balance) VALUES($1,30,30)`, account); err != nil {
		t.Fatal(err)
	}
	// This fixture represents a pre-origin ledger fact. New Add is never allowed
	// to manufacture this legacy classification.
	legacyID, legacyOperation := identity.New("credit"), identity.New("legacy_grant")
	if _, err := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_ledger(id,operation_id,account_id,kind,amount,balance,source,subject,reason,category,legacy_amount) VALUES($1,$2,$3,'add',30,30,'legacy-fixture','legacy','unclassified','legacy',30)`, legacyID, legacyOperation, account); err != nil {
		t.Fatal(err)
	}
	for _, category := range []string{c.Gift, c.Recharge} {
		amount := int64(5)
		if category == c.Recharge {
			amount = 10
		}
		if _, err := s.Add(ctx, c.Change{AccountID: account, Amount: amount, Category: category, OperationID: identity.New("grant"), Source: "fixture", Subject: "operator", Reason: "fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	debit, err := s.Deduct(ctx, c.Change{AccountID: account, Amount: 20, OperationID: identity.New("debit"), Source: "fixture", Subject: account, Reason: "fixture"})
	if err != nil || debit.GiftAmount != 5 || debit.RechargeAmount != 10 || debit.LegacyAmount != 5 {
		t.Fatal("legacy consumed before recorded funding", debit, err)
	}
	if _, err = s.Refund(ctx, c.Change{OriginalDeductionID: debit.ID, Amount: 20, OperationID: identity.New("refund"), Subject: account, Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	wallet, err := s.CreditWallet(ctx, account)
	if err != nil || wallet != (c.Wallet{Balance: 45, RechargeBalance: 10, GiftBalance: 5, LegacyBalance: 30}) {
		t.Fatal("refund reclassified old money", wallet, err)
	}
	old, err := s.CreditOperation(ctx, legacyOperation)
	if err != nil || old.Category != c.Legacy || old.LegacyAmount != 30 || old.GiftAmount != 0 || old.RechargeAmount != 0 {
		t.Fatal("legacy receipt was rewritten", old, err)
	}
	if _, err = s.Add(ctx, c.Change{AccountID: account, Amount: 1, Category: c.Legacy, OperationID: identity.New("invalid"), Source: "fixture", Subject: "operator", Reason: "fixture"}); !codeIs(err, "CREDIT_INVALID_CATEGORY") {
		t.Fatal("new legacy issuance accepted", err)
	}
	foreignID, chargeID := identity.New("private_project"), identity.New("foreign_charge")
	if err = s.Create(ctx, p.Project{ID: foreignID, OwnerID: identity.New("other_owner"), Revision: 1, Title: "foreign private title", Runs: []p.Run{{ID: chargeID, Credit: &c.Charge{ID: chargeID}, Status: p.RunCompleted}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Deduct(ctx, c.Change{AccountID: account, Amount: 10, OperationID: "generation:" + chargeID + ":deduct", Source: foreignID, Subject: account, Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	history, err := s.CreditHistory(ctx, account, c.HistoryFilter{Page: 1, PageSize: 20, Category: "all", Direction: "all"})
	if err != nil || history.Summary.NetConsumed != 10 || history.Summary.SettledConsumed != 0 || history.Summary.Pending != 10 || len(history.Analysis) != 0 {
		t.Fatal("borrowed another owner's completed result", history, err)
	}
}
