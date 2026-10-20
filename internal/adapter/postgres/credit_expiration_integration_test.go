package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
)

func TestDailyGiftExpirationAndCrossDayRefundPG(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	future := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	past := future.Add(-48 * time.Hour)
	grant := func(account, category string, amount int64, expiry *time.Time) d.Entry {
		t.Helper()
		entry, err := s.Add(ctx, d.Change{AccountID: account, Category: category, Amount: amount, DailyExpiresAt: expiry, OperationID: identity.New("expiry_grant"), Source: "fixture", Subject: "fixture", Reason: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	debit := func(account string, amount int64) d.Entry {
		t.Helper()
		entry, err := s.Deduct(ctx, d.Change{AccountID: account, Amount: amount, OperationID: identity.New("expiry_debit"), Source: "operations-adjustment", Subject: "fixture", Reason: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	account := identity.New("expiry_refund")
	grant(account, d.Recharge, 100, nil)
	grant(account, d.Gift, 40, nil)
	grant(account, d.Gift, 60, &future)
	first := debit(account, 90)
	if first.DailyAmount != 60 || first.GiftAmount != 90 || first.RechargeAmount != 0 {
		t.Fatal("daily was not spent first", first)
	}
	sameDay, err := s.Refund(ctx, d.Change{OriginalDeductionID: first.ID, Amount: 90, OperationID: identity.New("same_day"), Subject: "fixture", Reason: "fixture"})
	w, readErr := s.CreditWallet(ctx, account)
	if err != nil || readErr != nil || sameDay.DailyAmount != 60 || w.Balance != 200 || w.DailyBalance != 60 || !w.DailyExpiresAt.Equal(future) {
		t.Fatal(sameDay, w, err, readErr)
	}
	original := debit(account, 120)
	// 隔离夹具推进原扣款的有效期；不改宿主机时钟、不等待真实午夜。
	if _, err = conn.Exec(ctx, `UPDATE drama_catalog_v1.credit_ledger SET daily_expires_at=$2 WHERE id=$1`, original.ID, past); err != nil {
		t.Fatal(err)
	}
	grant(account, d.Gift, 15, &future) // 新一天已领取，旧退款不能混入这个周期。
	refund := d.Change{OriginalDeductionID: original.ID, Amount: 120, OperationID: identity.New("cross_day"), Subject: "fixture", Reason: "fixture"}
	// 让到期流水失败，验证原返还及钱包也整体回滚。
	suffix := identity.New("expiry_fail")
	function := "drama_catalog_v1." + suffix
	sql := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture expiry write rejected'; END $$; CREATE TRIGGER %s BEFORE INSERT ON drama_catalog_v1.credit_ledger FOR EACH ROW WHEN (NEW.account_id='%s' AND NEW.kind='expire') EXECUTE FUNCTION %s()`, function, suffix, account, function)
	if _, err = conn.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_, _ = conn.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON drama_catalog_v1.credit_ledger; DROP FUNCTION IF EXISTS %s()`, suffix, function))
	}
	t.Cleanup(cleanup)
	if _, err = s.Refund(ctx, refund); err == nil {
		t.Fatal("expiry failure committed a refund")
	}
	w, _ = s.CreditWallet(ctx, account)
	if w.Balance != 95 || w.DailyBalance != 15 {
		t.Fatal("failed refund changed wallet", w)
	}
	if _, err = s.CreditOperation(ctx, refund.OperationID); err == nil {
		t.Fatal("refund survived rollback")
	}
	cleanup()
	receipt, err := s.Refund(ctx, refund)
	w, readErr = s.CreditWallet(ctx, account)
	if err != nil || readErr != nil || receipt.Amount != 120 || receipt.Balance != 155 || w.Balance != 155 || w.RechargeBalance != 100 || w.GiftBalance != 55 || w.DailyBalance != 15 || !w.DailyExpiresAt.Equal(future) {
		t.Fatal(receipt, w, err, readErr)
	}
	reclaimed, err := s.CreditOperation(ctx, "gift:expire-refund:"+refund.OperationID)
	if err != nil || reclaimed.Kind != "expire" || reclaimed.Amount != 60 || reclaimed.Balance != 155 || reclaimed.OriginalDeductionID != "" || !reclaimed.DailyExpiresAt.Equal(past) {
		t.Fatal(reclaimed, err)
	}
	again, err := s.Refund(ctx, refund)
	if err != nil || again.ID != receipt.ID {
		t.Fatal("recovery duplicated refund", again, err)
	}
	refund.Amount = 1
	refund.OperationID = identity.New("exceeded")
	if _, err = s.Refund(ctx, refund); !codeIs(err, "CREDIT_REFUND_EXCEEDED") {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	w, _ = s.CreditWallet(ctx, account)
	if w.Balance != 155 || w.DailyBalance != 15 {
		t.Fatal("repeat migration reclassified funds", w)
	}
	raw, _ := json.Marshal(reclaimed)
	if strings.Contains(string(raw), "dailyAmount") || strings.Contains(string(raw), "dailyExpiresAt") {
		t.Fatal("internal funding metadata leaked into receipt")
	}

	t.Run("recovery-read-boundary-concurrency-and-no-offline-grants", func(t *testing.T) {
		account := identity.New("expiry_remaining")
		grant(account, d.Recharge, 100, nil)
		grant(account, d.Gift, 40, nil)
		grant(account, d.Gift, 60, &future)
		original := debit(account, 20)
		if _, err = conn.Exec(ctx, `UPDATE drama_catalog_v1.credit_accounts SET daily_expires_at=$2 WHERE account_id=$1`, account, past); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.Exec(ctx, `UPDATE drama_catalog_v1.credit_ledger SET daily_expires_at=$2 WHERE id=$1`, original.ID, past); err != nil {
			t.Fatal(err)
		}
		w, err := s.CreditWallet(ctx, account)
		if err != nil || w.Balance != 140 || w.GiftBalance != 40 || w.DailyBalance != 0 {
			t.Fatal("expired funds still spendable before sweep", w, err)
		}
		if _, err = s.Deduct(ctx, d.Change{AccountID: account, Amount: 150, OperationID: identity.New("expired_rejected"), Source: "fixture", Subject: "fixture", Reason: "fixture"}); !codeIs(err, "CREDIT_INSUFFICIENT") {
			t.Fatal(err)
		}
		changes, closeWatch, err := s.WatchCreditAccount(ctx, account)
		if err != nil {
			t.Fatal(err)
		}
		defer closeWatch()
		var dailyClaimsBefore int
		if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_gift_claims WHERE kind='daily'`).Scan(&dailyClaimsBefore); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if e := s.ExpireDailyCredits(ctx); e != nil {
					t.Error(e)
				}
			}()
		}
		wg.Add(1)
		go func() { defer wg.Done(); debit(account, 10) }()
		wg.Wait()
		select {
		case <-changes:
		case <-time.After(3 * time.Second):
			t.Fatal("committed expiry/deduction did not notify")
		}
		if err = s.ExpireDailyCredits(ctx); err != nil {
			t.Fatal(err)
		}
		w, _ = s.CreditWallet(ctx, account)
		var count, claimsAfter int
		if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND kind='expire'`, account).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_gift_claims WHERE kind='daily'`).Scan(&claimsAfter); err != nil {
			t.Fatal(err)
		}
		if count != 1 || claimsAfter != dailyClaimsBefore || w.Balance != 130 || w.GiftBalance != 30 || w.RechargeBalance != 100 {
			t.Fatal(count, claimsAfter, dailyClaimsBefore, w)
		}
		if _, err = s.Refund(ctx, d.Change{OriginalDeductionID: original.ID, Amount: 20, OperationID: identity.New("late_refund"), Subject: "fixture", Reason: "fixture"}); err != nil {
			t.Fatal(err)
		}
		filter := d.HistoryFilter{Page: 1, PageSize: 20, Direction: "all", Category: "all"}
		history, err := s.CreditHistory(ctx, account, filter)
		if err != nil || history.Summary.Consumed != 30 || history.Summary.Refunded != 20 || history.Summary.NetConsumed != 10 || history.Summary.SettledConsumed != 10 {
			t.Fatal(history, err)
		}
		filter.Direction = "expired"
		expired, err := s.CreditHistory(ctx, account, filter)
		if err != nil || expired.Total != 2 || expired.Summary.Consumed != 0 || expired.Summary.Refunded != 0 || len(expired.Analysis) != 0 {
			t.Fatal(expired, err)
		}
		w, _ = s.CreditWallet(ctx, account)
		if w.Balance != 130 {
			t.Fatal("late refund renewed daily funds", w)
		}
	})
}
