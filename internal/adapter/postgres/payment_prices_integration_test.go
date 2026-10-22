package postgres

import (
	"context"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/payment"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPaymentPGDiscountSnapshotCreditsLimitAndRefund(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	pkg := f.pkg
	pkg.Name = "原价折扣与赠分"
	pkg.OriginalAmountFen = 10000
	pkg.AmountFen = 8000
	var err error
	f.pkg, err = f.s.SavePaymentPackage(ctx, pkg, f.operator)
	if err != nil {
		t.Fatal(err)
	}
	// 原价超限、实付不超限时仍可下单。
	if _, err = f.s.pool.Exec(ctx, `UPDATE drama_catalog_v1.payment_settings SET daily_limit_fen=8000 WHERE id`); err != nil {
		t.Fatal(err)
	}
	o := f.order(t)
	pkg = f.pkg
	pkg.OriginalAmountFen = 20000
	pkg.AmountFen = 16000
	if _, err = f.s.SavePaymentPackage(ctx, pkg, f.operator); err != nil {
		t.Fatal(err)
	}
	saved, err := f.s.PaymentOrder(ctx, o.OrderNo)
	if err != nil || saved.OriginalAmountFen != 10000 || saved.AmountFen != 8000 {
		t.Fatal(saved, err)
	}
	confirmation := paidConfirmation(o)
	confirmation.Trade.AmountFen = 10000
	if _, err = f.s.CreditPaymentOrder(ctx, confirmation); err == nil {
		t.Fatal("original price accepted as paid amount")
	}
	if _, err = f.s.CreditPaymentOrder(ctx, paidConfirmation(o)); err != nil {
		t.Fatal(err)
	}
	wallet, err := f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.RechargeBalance != 1000 || wallet.GiftBalance != 100 {
		t.Fatal(wallet, err)
	}
	quote, err := f.s.PaymentRefundQuote(ctx, o.OrderNo, 500)
	if err != nil || quote.AmountFen != 4000 || quote.GiftCredits != 50 {
		t.Fatal(quote, err)
	}
	refund, err := f.s.CreatePaymentRefund(ctx, p.RefundRequest{RefundID: identity.New("discount_refund"), OrderNo: o.OrderNo, OperatorID: f.operator, Reason: "退一半", Credits: 500})
	if err != nil || refund.AmountFen != 4000 || refund.GiftCredits != 50 {
		t.Fatal(refund, err)
	}
	wallet, err = f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.RechargeBalance != 500 || wallet.GiftBalance != 50 {
		t.Fatal(wallet, err)
	}
}

func TestPaymentPGPriceMigrationPreservesLegacyFacts(t *testing.T) {
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("isolated PG required")
	}
	ctx := context.Background()
	s, err := Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	namespace := identity.New("payment_price_migration")
	// 自有临时schema模拟旧版，绝不删除/回退正式或验收表。
	legacy := schemaSQL
	start := strings.Index(legacy, "-- 旧档位缺少名称时保留未命名")
	end := strings.Index(legacy, "CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_settings")
	if start < 0 || end <= start {
		t.Fatal("package migration block missing")
	}
	legacy = legacy[:start] + legacy[end:]
	start = strings.Index(legacy, "-- 历史原价只按订单自身实付回填")
	end = strings.Index(legacy, "ALTER TABLE drama_catalog_v1.payment_orders ADD COLUMN IF NOT EXISTS channel_paid_at")
	if start < 0 || end <= start {
		t.Fatal("order migration block missing")
	}
	legacy = legacy[:start] + legacy[end:]
	legacy = strings.ReplaceAll(legacy, "drama_catalog_v1", namespace)
	if _, err = s.pool.Exec(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	defer s.pool.Exec(ctx, "DROP SCHEMA "+namespace+" CASCADE")
	at := time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC)
	for _, query := range []string{
		"INSERT INTO " + namespace + ".users(id,email,username,role,password_hash) VALUES('old-user','old@test.invalid','old','user','fixture'::bytea)",
		"INSERT INTO " + namespace + ".payment_packages(id,revision,amount_fen,recharge_credits,gift_credits,enabled,created_at,updated_at) VALUES('old-package',7,200,1000,100,true,'2026-10-20 04:00:00Z','2026-10-20 04:00:00Z')",
		"INSERT INTO " + namespace + ".payment_orders(order_no,buyer_user_id,client_request_id,target_kind,target_id,target_name,credit_account_id,package_id,amount_fen,recharge_credits,gift_credits,channel,status,agreement_version,expires_at,next_query_at,created_at) VALUES('old-order','old-user','old-request','personal','old-user','personal','old-user','old-package',100,1000,100,'demo','pending','2026-10-20','2026-10-20 04:15:00Z','2026-10-20 04:01:00Z','2026-10-20 04:00:00Z')",
		"INSERT INTO " + namespace + ".credit_accounts(account_id,balance,recharge_balance) VALUES('old-user',777,777)",
	} {
		if _, err = s.pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	upgraded := strings.ReplaceAll(schemaSQL, "drama_catalog_v1", namespace)
	for round := 0; round < 2; round++ {
		if _, err = s.pool.Exec(ctx, upgraded); err != nil {
			t.Fatal(err)
		}
		var revision uint64
		var name string
		var original, amount int64
		var created, updated time.Time
		if err = s.pool.QueryRow(ctx, "SELECT name,revision,original_amount_fen,amount_fen,created_at,updated_at FROM "+namespace+".payment_packages WHERE id='old-package'").Scan(&name, &revision, &original, &amount, &created, &updated); err != nil {
			t.Fatal(err)
		}
		wantOriginal := int64(200)
		if round == 1 {
			wantOriginal = 300
		}
		if name != "" || revision != 7 || original != wantOriginal || amount != 200 || !created.Equal(at) || !updated.Equal(at) {
			t.Fatal(name, revision, original, amount, created, updated)
		}
		var status string
		if err = s.pool.QueryRow(ctx, "SELECT original_amount_fen,amount_fen,status,created_at FROM "+namespace+".payment_orders WHERE order_no='old-order'").Scan(&original, &amount, &status, &created); err != nil || original != 100 || amount != 100 || status != "pending" || !created.Equal(at) {
			t.Fatal(original, amount, status, created, err)
		}
		var balance, events, users, orders int64
		for _, entry := range []struct {
			query string
			dest  *int64
		}{
			{"SELECT balance FROM " + namespace + ".credit_accounts WHERE account_id='old-user'", &balance},
			{"SELECT count(*) FROM " + namespace + ".payment_events", &events},
			{"SELECT count(*) FROM " + namespace + ".users", &users},
			{"SELECT count(*) FROM " + namespace + ".payment_orders", &orders},
		} {
			if err = s.pool.QueryRow(ctx, entry.query).Scan(entry.dest); err != nil {
				t.Fatal(err)
			}
		}
		if balance != 777 || events != 0 || users != 1 || orders != 1 {
			t.Fatal(balance, events, users, orders)
		}
		if round == 0 {
			if _, err = s.pool.Exec(ctx, "UPDATE "+namespace+".payment_packages SET original_amount_fen=300 WHERE id='old-package'"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = s.pool.Exec(ctx, "UPDATE "+namespace+".payment_packages SET original_amount_fen=199 WHERE id='old-package'"); err == nil {
		t.Fatal("invalid original price accepted")
	}
}
