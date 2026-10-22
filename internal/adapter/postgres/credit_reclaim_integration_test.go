package postgres

import (
	"context"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreditReclaimPGAtomicIdempotentConcurrentAndHistory(t *testing.T) {
	s, _, _ := runtimeFixture(t)
	ctx := context.Background()
	account := identity.New("reclaim_account")
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.credit_ledger WHERE account_id=$1`, account)
		_, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.credit_accounts WHERE account_id=$1`, account)
		_, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.runtime_events WHERE key=$1`, "credits:"+account)
	})
	grant := func(category string, amount int64, expiry *time.Time) d.Entry {
		t.Helper()
		e, err := s.Add(ctx, d.Change{AccountID: account, Category: category, Amount: amount, DailyExpiresAt: expiry, OperationID: identity.New("grant"), Source: "payment-order:test", Subject: "payment:demo", Reason: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	reclaim := func(c d.Change) (d.Entry, error) {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return d.Entry{}, err
		}
		defer tx.Rollback(ctx)
		e, err := s.creditReclaimTx(ctx, tx, c)
		if err != nil {
			return d.Entry{}, err
		}
		return e, tx.Commit(ctx)
	}
	change := func(original d.Entry, amount int64) d.Change {
		return d.Change{OriginalAdditionID: original.ID, Amount: amount, OperationID: identity.New("reclaim"), Subject: "operator", Reason: "充值退款扣回"}
	}
	hasCode := func(err error, code string) bool { var e *fault.Error; return errors.As(err, &e) && e.Code == code }
	recharge := grant(d.Recharge, 100, nil)
	gift := grant(d.Gift, 40, nil)
	expiry := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	daily := grant(d.Gift, 60, &expiry)
	c := change(recharge, 25)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := reclaim(c)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- e.ID
		}()
	}
	wg.Wait()
	close(ids)
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate reclaim", id, first)
		}
	}
	c.Amount = 24
	if _, err := reclaim(c); !hasCode(err, "CREDIT_IDEMPOTENCY_CONFLICT") {
		t.Fatal("same number changed amount", err)
	}
	permanent, err := reclaim(change(gift, 40))
	if err != nil || permanent.GiftAmount != 40 || permanent.DailyAmount != 0 || permanent.OriginalAdditionID != gift.ID || permanent.Source != gift.Source {
		t.Fatal(permanent, err)
	}
	otherGift := grant(d.Gift, 1, nil)
	if _, err = reclaim(change(otherGift, 2)); !hasCode(err, "CREDIT_RECLAIM_EXCEEDED") {
		t.Fatal(err)
	}
	if _, err = reclaim(change(daily, 1)); err == nil {
		t.Fatal("daily original accepted")
	}
	if _, err = s.Deduct(ctx, d.Change{AccountID: account, Amount: 1, OperationID: identity.New("debit"), Source: "fixture", Subject: "fixture", Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	// 先扣掉每日1份，仍不得把剩余每日份额当成永久gift去扣回。
	if _, err = reclaim(change(otherGift, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err = reclaim(change(gift, 1)); !hasCode(err, "CREDIT_RECLAIM_EXCEEDED") {
		t.Fatal(err)
	}
	grant(d.Recharge, 200, nil)
	var successes atomic.Int32
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reclaim(change(recharge, 10))
			if err == nil {
				successes.Add(1)
			} else if !hasCode(err, "CREDIT_RECLAIM_EXCEEDED") {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 7 {
		t.Fatal("concurrent original cap", successes.Load())
	}
	if _, err = reclaim(change(recharge, 6)); !hasCode(err, "CREDIT_RECLAIM_EXCEEDED") {
		t.Fatal(err)
	}
	if _, err = reclaim(change(recharge, 5)); err != nil {
		t.Fatal(err)
	}
	wallet, err := s.CreditWallet(ctx, account)
	if err != nil || wallet.Balance != 259 || wallet.RechargeBalance != 200 || wallet.GiftBalance != 59 || wallet.DailyBalance != 59 {
		t.Fatal(wallet, err)
	}
	f := d.HistoryFilter{Page: 1, PageSize: 50, Direction: "reclaimed", Category: "all"}
	history, err := s.CreditHistory(ctx, account, f)
	if err != nil || history.Total != 11 || history.Summary != (d.HistorySummary{}) || len(history.Analysis) != 0 {
		t.Fatal("reclaims included in consumption", history, err)
	}
	for _, entry := range history.Items {
		if entry.Kind != "reclaim" || entry.OriginalAdditionID == "" {
			t.Fatal(entry)
		}
	}
	f.Direction = "acquired"
	acquired, err := s.CreditHistory(ctx, account, f)
	if err != nil || acquired.Total != 5 || acquired.Summary.Acquired != 401 {
		t.Fatal(acquired, err)
	}
	f.Direction = "all"
	all, err := s.CreditHistory(ctx, account, f)
	if err != nil || all.Total != 17 || all.Summary.Acquired != 401 || all.Summary.Consumed != 1 {
		t.Fatal(all, err)
	}
	before := wallet
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.CreditWallet(ctx, account)
	if err != nil || after.Balance != before.Balance || after.RechargeBalance != before.RechargeBalance || after.DailyBalance != before.DailyBalance {
		t.Fatal("migration changed balances", before, after, err)
	}

	// 事务整体回滚后，流水、钱包及SSE出站事件均不得残留。
	original := grant(d.Recharge, 30, nil)
	if err = s.FlushRuntimeEvents(ctx); err != nil {
		t.Fatal(err)
	}
	command := change(original, 10)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.creditReclaimTx(ctx, tx, command); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreditOperation(ctx, command.OperationID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("rollback receipt survived", err)
	}
	wallet, err = s.CreditWallet(ctx, account)
	if err != nil || wallet.Balance != 289 {
		t.Fatal("rollback changed balance", wallet, err)
	}
	var pending bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.runtime_events WHERE key=$1)`, "credits:"+account).Scan(&pending); err != nil || pending {
		t.Fatal("rollback published", pending, err)
	}
	changes, cancel, err := s.WatchCreditAccount(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	committed, err := reclaim(command)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FlushRuntimeEvents(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(3 * time.Second):
		t.Fatal("committed reclaim did not notify")
	}
	again, err := reclaim(command)
	if err != nil || again.ID != committed.ID {
		t.Fatal("commit recovery", again, err)
	}
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.runtime_events WHERE key=$1)`, "credits:"+account).Scan(&pending); err != nil || pending {
		t.Fatal("idempotent read queued event", pending, err)
	}
}

func TestCreditReclaimPGPreservesDailyFundsAndRollbackOnInsertFailure(t *testing.T) {
	s, _, _ := runtimeFixture(t)
	ctx := context.Background()
	account := identity.New("reclaim_daily")
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.credit_ledger WHERE account_id=$1`, account)
		_, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.credit_accounts WHERE account_id=$1`, account)
		_, _ = s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.runtime_events WHERE key=$1`, "credits:"+account)
	})
	grant := func(amount int64, expiry *time.Time) d.Entry {
		t.Helper()
		e, err := s.Add(ctx, d.Change{AccountID: account, Category: d.Gift, Amount: amount, DailyExpiresAt: expiry, OperationID: identity.New("grant"), Source: "fixture", Subject: "fixture", Reason: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	reclaim := func(c d.Change) (d.Entry, error) {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return d.Entry{}, err
		}
		defer tx.Rollback(ctx)
		e, err := s.creditReclaimTx(ctx, tx, c)
		if err != nil {
			return d.Entry{}, err
		}
		return e, tx.Commit(ctx)
	}
	original := grant(100, nil)
	if _, err := s.Deduct(ctx, d.Change{AccountID: account, Amount: 90, OperationID: identity.New("consume_gift"), Source: "fixture", Subject: "fixture", Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	daily := grant(50, &expiry)
	c := d.Change{OriginalAdditionID: original.ID, Amount: 11, OperationID: identity.New("insufficient"), Subject: "operator", Reason: "fixture"}
	_, err := reclaim(c)
	var domainError *fault.Error
	if !errors.As(err, &domainError) || domainError.Code != "CREDIT_INSUFFICIENT" {
		t.Fatal("spent permanent funds were replaced by daily funds", err)
	}
	c.Amount = 10
	c.OperationID = identity.New("allowed")
	if _, err = reclaim(c); err != nil {
		t.Fatal(err)
	}
	wallet, err := s.CreditWallet(ctx, account)
	if err != nil || wallet.Balance != 50 || wallet.GiftBalance != 50 || wallet.DailyBalance != 50 || !wallet.DailyExpiresAt.Equal(expiry) {
		t.Fatal(wallet, err)
	}
	// 即使绕开应用，数据库也不允许关联每日Add或普通扣款。
	if _, err = s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_ledger(id,operation_id,account_id,kind,amount,balance,source,subject,reason,category,gift_amount,original_addition_id) VALUES($1,$1,$2,'reclaim',1,49,'fixture','fixture','fixture','gift',1,$3)`, identity.New("invalid_original"), account, daily.ID); err == nil {
		t.Fatal("schema accepted a daily addition")
	}

	// 让新扣回的INSERT失败，验证之前同事务追加的到期回收和余额也撤销。
	second := grant(10, nil)
	past := time.Now().Add(-time.Hour)
	if _, err = s.pool.Exec(ctx, `UPDATE drama_catalog_v1.credit_accounts SET daily_expires_at=$2 WHERE account_id=$1`, account, past); err != nil {
		t.Fatal(err)
	}
	if err = s.FlushRuntimeEvents(ctx); err != nil {
		t.Fatal(err)
	}
	c = d.Change{OriginalAdditionID: second.ID, Amount: 5, OperationID: identity.New("fail_insert"), Subject: "operator", Reason: "fixture"}
	suffix := identity.New("reclaim_fail")
	sql := fmt.Sprintf(`CREATE FUNCTION drama_catalog_v1.%s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture reclaim rejected'; END $$; CREATE TRIGGER %s BEFORE INSERT ON drama_catalog_v1.credit_ledger FOR EACH ROW WHEN (NEW.operation_id='%s') EXECUTE FUNCTION drama_catalog_v1.%s()`, suffix, suffix, c.OperationID, suffix)
	if _, err = s.pool.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON drama_catalog_v1.credit_ledger; DROP FUNCTION IF EXISTS drama_catalog_v1.%s()`, suffix, suffix))
	})
	if _, err = reclaim(c); err == nil {
		t.Fatal("fixture failure was accepted")
	}
	raw, err := scanCreditWallet(s.pool.QueryRow(ctx, `SELECT `+creditWalletColumns+` FROM drama_catalog_v1.credit_accounts WHERE account_id=$1`, account))
	if err != nil || raw.Balance != 60 || raw.DailyBalance != 50 || raw.GiftBalance != 60 {
		t.Fatal("failed insert leaked expiration", raw, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND (operation_id=$2 OR kind='expire')`, account, c.OperationID).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed transaction left receipts", count, err)
	}
	var pending bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.runtime_events WHERE key=$1)`, "credits:"+account).Scan(&pending); err != nil || pending {
		t.Fatal("failed insert notified", pending, err)
	}
}
