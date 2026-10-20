package postgres_test

import (
	"context"
	"sync"
	"testing"
	"time"

	c "drama-agent/internal/application/credits"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
)

func TestDailyGiftZeroEligibilityAndLegacyUpgradePG(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	initial, err := s.CreditGiftPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(), `UPDATE drama_catalog_v1.credit_gift_policy SET registration_gift=$1,daily_gift=$2 WHERE id`, initial.RegistrationGift, initial.DailyGift)
		if err != nil {
			t.Error(err)
		}
	})
	setAmount := func(amount int64) c.GiftPolicy {
		t.Helper()
		current, err := s.CreditGiftPolicy(ctx)
		if err != nil {
			t.Fatal(err)
		}
		policy, err := s.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: current.Revision, DailyGift: amount})
		if err != nil {
			t.Fatal(err)
		}
		return policy
	}
	zeroPolicy := setAmount(0)
	fresh := createUser(t, s, identity.New("daily_zero")+"@example.test")
	legacy := createUser(t, s, identity.New("daily_legacy_zero")+"@example.test")
	var now time.Time
	if err = conn.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	today, yesterday := c.GiftDate(now), c.GiftDate(now.Add(-24*time.Hour))
	if _, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_gift_claims(user_id,kind,period,amount,policy_revision,created_at) VALUES($1,'daily',$2,0,$3,$4),($1,'daily',$5,0,$3,$4)`, legacy.ID, today, zeroPolicy.Revision, now.Add(-time.Hour), yesterday); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{fresh.ID, legacy.ID} {
		for range 2 {
			gift, err := s.ClaimDailyCreditGift(ctx, user)
			if err != nil || gift.Amount != 0 || gift.Claimed || gift.AlreadyClaimed {
				t.Fatal("zero consumed eligibility", gift, err)
			}
		}
	}
	for _, user := range []string{fresh.ID, legacy.ID} {
		wallet, err := s.CreditWallet(ctx, user)
		if err != nil || wallet.Balance != 0 {
			t.Fatal("zero wallet", wallet, err)
		}
	}
	var count int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_gift_claims WHERE user_id=$1 AND kind='daily'`, fresh.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("zero created a daily claim", count, err)
	}
	positivePolicy := setAmount(60)
	for _, user := range []string{fresh.ID, legacy.ID} {
		var wg sync.WaitGroup
		results := make(chan c.DailyGift, 12)
		errors := make(chan error, 12)
		for range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				gift, err := s.ClaimDailyCreditGift(ctx, user)
				results <- gift
				errors <- err
			}()
		}
		wg.Wait()
		close(results)
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		first := 0
		for gift := range results {
			if gift.Amount != 60 || !gift.Claimed || gift.Date != today {
				t.Fatal("wrong same-day grant", gift)
			}
			if !gift.AlreadyClaimed {
				first++
			}
		}
		wallet, err := s.CreditWallet(ctx, user)
		if err != nil || wallet.Balance != 60 {
			t.Fatal("positive wallet", wallet, err)
		}
		if first != 1 {
			t.Fatal("concurrent duplicate first receipts", first)
		}
		if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND source='daily-gift' AND amount=60`, user).Scan(&count); err != nil || count != 1 {
			t.Fatal("duplicate funding", count, err)
		}
		var revision uint64
		var created time.Time
		if err = conn.QueryRow(ctx, `SELECT policy_revision,created_at FROM drama_catalog_v1.credit_gift_claims WHERE user_id=$1 AND kind='daily' AND period=$2 AND amount=60 AND operation_id=$3`, user, today, "gift:daily:"+user+":"+today).Scan(&revision, &created); err != nil || revision != positivePolicy.Revision || created.Before(now) {
			t.Fatal("grant eligibility was not frozen atomically", revision, created, err)
		}
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_gift_claims WHERE user_id=$1 AND kind='daily' AND period=$2 AND amount=0 AND operation_id IS NULL`, legacy.ID, yesterday).Scan(&count); err != nil || count != 1 {
		t.Fatal("past zero record was changed", count, err)
	}
	for _, amount := range []int64{90, 0} {
		setAmount(amount)
		for _, user := range []string{fresh.ID, legacy.ID} {
			gift, err := s.ClaimDailyCreditGift(ctx, user)
			if err != nil || gift.Amount != 60 || !gift.Claimed || !gift.AlreadyClaimed {
				t.Fatal("policy change rewrote actual claim", amount, gift, err)
			}
		}
	}
	if _, err = s.Deduct(ctx, d.Change{AccountID: legacy.ID, OperationID: identity.New("spend_daily"), Amount: 60, Source: "fixture", Subject: legacy.ID, Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	setAmount(120)
	spent, err := s.ClaimDailyCreditGift(ctx, legacy.ID)
	if err != nil || spent.Amount != 60 || !spent.AlreadyClaimed {
		t.Fatal("spending reopened eligibility", spent, err)
	}

	spentWallet, err := s.CreditWallet(ctx, legacy.ID)
	if err != nil || spentWallet.Balance != 0 {
		t.Fatal("spent wallet", spentWallet, err)
	}
	t.Run("legacy-upgrade-rolls-back-with-ledger", func(t *testing.T) {
		user := createUser(t, s, identity.New("daily_rollback_zero")+"@example.test")
		if _, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_gift_claims(user_id,kind,period,amount,policy_revision) VALUES($1,'daily',$2,0,$3)`, user.ID, today, zeroPolicy.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.Exec(ctx, `CREATE FUNCTION drama_catalog_v1.reject_test_zero_upgrade() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected claim update failure'; END $$;
			CREATE TRIGGER reject_test_zero_upgrade BEFORE UPDATE ON drama_catalog_v1.credit_gift_claims FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.reject_test_zero_upgrade()`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := conn.Exec(context.Background(), `DROP TRIGGER IF EXISTS reject_test_zero_upgrade ON drama_catalog_v1.credit_gift_claims; DROP FUNCTION IF EXISTS drama_catalog_v1.reject_test_zero_upgrade()`); err != nil {
				t.Error(err)
			}
		})
		if _, err = s.ClaimDailyCreditGift(ctx, user.ID); err == nil {
			t.Fatal("injected update failure was swallowed")
		}
		wallet, err := s.CreditWallet(ctx, user.ID)
		if err != nil || wallet.Balance != 0 {
			t.Fatal("claim failure kept funding", wallet, err)
		}
		if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1`, user.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("claim failure left a ledger entry", count, err)
		}
		if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_gift_claims WHERE user_id=$1 AND kind='daily' AND period=$2 AND amount=0 AND operation_id IS NULL`, user.ID, today).Scan(&count); err != nil || count != 1 {
			t.Fatal("claim failure consumed legacy zero eligibility", count, err)
		}
		if _, err = conn.Exec(ctx, `DROP TRIGGER reject_test_zero_upgrade ON drama_catalog_v1.credit_gift_claims; DROP FUNCTION drama_catalog_v1.reject_test_zero_upgrade()`); err != nil {
			t.Fatal(err)
		}
		gift, err := s.ClaimDailyCreditGift(ctx, user.ID)
		if err != nil || gift.Amount != 120 || gift.AlreadyClaimed || !gift.Claimed {
			t.Fatal("failed upgrade was not retryable", gift, err)
		}
		wallet, err = s.CreditWallet(ctx, user.ID)
		if err != nil || wallet.Balance != 120 {
			t.Fatal("retry wallet", wallet, err)
		}
	})
}
