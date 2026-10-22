package postgres

import (
	"context"
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/payment"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type paymentPGFixture struct {
	s              *Store
	user, operator string
	pkg            p.Package
}

func newPaymentPGFixture(t *testing.T) paymentPGFixture {
	t.Helper()
	s, _, _ := runtimeFixture(t)
	ctx := context.Background()
	f := paymentPGFixture{s: s, user: identity.New("payment_user"), operator: identity.New("payment_operator")}
	for _, v := range []struct{ id, role string }{{f.user, "user"}, {f.operator, "operator"}} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$1||'@test.invalid','支付测试',$2,'fixture'::bytea)`, v.id, v.role); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	f.pkg, err = s.SavePaymentPackage(ctx, p.Package{Name: "测试档位", ID: identity.New("package"), OriginalAmountFen: 100, AmountFen: 100, RechargeCredits: 1000, GiftCredits: 100, Enabled: true}, f.operator)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := s.PaymentSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := settings.DailyLimitFen
	if _, err = s.pool.Exec(ctx, `UPDATE drama_catalog_v1.payment_settings SET daily_limit_fen=500000 WHERE id`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `UPDATE drama_catalog_v1.payment_settings SET daily_limit_fen=$1 WHERE id`, before)
	})
	return f
}
func (f paymentPGFixture) order(t *testing.T) p.Order {
	t.Helper()
	no, err := p.GenerateOrderNo(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	o, created, err := f.s.CreatePaymentOrder(context.Background(), p.Order{OrderNo: no, BuyerUserID: f.user, ClientRequestID: identity.New("request"), Target: p.Target{Kind: "personal", ID: f.user, Name: "个人空间"}, CreditAccountID: f.user, PackageSnapshot: f.pkg.Snapshot(), Channel: "demo", AgreementVersion: p.AgreementVersion})
	if err != nil || !created {
		t.Fatal(o, created, err)
	}
	return o
}
func paidConfirmation(o p.Order) ports.PaymentConfirmation {
	at := time.Now().UTC().Truncate(time.Microsecond)
	return ports.PaymentConfirmation{OrderNo: o.OrderNo, Channel: o.Channel, Source: "query", Trade: ports.ChannelTrade{OrderNo: o.OrderNo, TradeNo: "demo-" + o.OrderNo, State: "paid", AmountFen: o.AmountFen, PaidAt: &at}}
}
func paymentHasCode(err error, code string) bool {
	var e *fault.Error
	return errors.As(err, &e) && e.Code == code
}

func TestPaymentPGCreateSnapshotsConcurrentCreditAndPendingLimit(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	o := f.order(t)
	var created atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			again, isNew, err := f.s.CreatePaymentOrder(ctx, o)
			if err != nil || again.OrderNo != o.OrderNo {
				t.Error(again, err)
			}
			if isNew {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 0 {
		t.Fatal("recreated same request")
	}
	newOrder := o
	newOrder.OrderNo, _ = p.GenerateOrderNo(time.Now())
	newOrder.ClientRequestID = identity.New("new_request")
	if _, _, err := f.s.CreatePaymentOrder(ctx, newOrder); !paymentHasCode(err, "PAYMENT_CONFLICT") {
		t.Fatal("second pending accepted", err)
	}
	next := f.pkg
	next.AmountFen = 200
	next.OriginalAmountFen = 200
	updated, err := f.s.SavePaymentPackage(ctx, next, f.operator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.SavePaymentPackage(ctx, next, f.operator); !paymentHasCode(err, "PAYMENT_CONFLICT") {
		t.Fatal("package CAS", err)
	}
	saved, err := f.s.PaymentOrder(ctx, o.OrderNo)
	if err != nil || saved.AmountFen != 100 || updated.AmountFen != 200 {
		t.Fatal(saved, updated, err)
	}
	confirmation := paidConfirmation(o)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			credited, err := f.s.CreditPaymentOrder(ctx, confirmation)
			if err != nil || credited.Status != p.Credited {
				t.Error(credited, err)
			}
		}()
	}
	wg.Wait()
	wallet, err := f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.Balance != 1100 || wallet.RechargeBalance != 1000 || wallet.GiftBalance != 100 {
		t.Fatal(wallet, err)
	}
	var count int
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE source=$1`, "payment-order:"+o.OrderNo).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	confirmed, ok, err := f.s.ConfirmedPaymentCredit(ctx, o.OrderNo)
	if err != nil || !ok || confirmed.OrderNo != o.OrderNo {
		t.Fatal(confirmed, ok, err)
	}
	details, err := f.s.PaymentOrderDetails(ctx, o.OrderNo)
	if err != nil || len(details.Credits) != 2 || len(details.Events) != 2 {
		t.Fatal(details, err)
	}
	if agreed, err := f.s.PaymentAgreement(ctx, f.user, p.AgreementVersion); err != nil || !agreed {
		t.Fatal(agreed, err)
	}
	if _, err = f.s.pool.Exec(ctx, `UPDATE drama_catalog_v1.users SET enabled=false WHERE id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	newOrder.PackageSnapshot = updated.Snapshot()
	if _, _, err = f.s.CreatePaymentOrder(ctx, newOrder); !paymentHasCode(err, "PAYMENT_TARGET_INACTIVE") {
		t.Fatal("disabled user accepted", err)
	}
}

func TestPaymentPGCreditRollbackAndCommitResponseLoss(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	o := f.order(t)
	confirmation := paidConfirmation(o)
	suffix := identity.New("payment_fail")
	sql := fmt.Sprintf(`CREATE FUNCTION drama_catalog_v1.%s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture bonus rejected'; END $$; CREATE TRIGGER %s BEFORE INSERT ON drama_catalog_v1.credit_ledger FOR EACH ROW WHEN (NEW.operation_id='gift:recharge-bonus:%s') EXECUTE FUNCTION drama_catalog_v1.%s()`, suffix, suffix, o.OrderNo, suffix)
	if _, err := f.s.pool.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_, _ = f.s.pool.Exec(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON drama_catalog_v1.credit_ledger; DROP FUNCTION IF EXISTS drama_catalog_v1.%s()`, suffix, suffix))
	}
	t.Cleanup(cleanup)
	if _, err := f.s.CreditPaymentOrder(ctx, confirmation); err == nil {
		t.Fatal("bonus failure committed")
	}
	current, err := f.s.PaymentOrder(ctx, o.OrderNo)
	wallet, werr := f.s.CreditWallet(ctx, f.user)
	if err != nil || werr != nil || current.Status != p.Pending || wallet.Balance != 0 {
		t.Fatal(current, wallet, err, werr)
	}
	var count int
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE source=$1`, "payment-order:"+o.OrderNo).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	cleanup()
	// 专属测试代理仅丢掉COMMIT响应，数据库实际已提交；存储须用新连接按稳定编号回读。
	proxy, arm := dropOneCommitResponse(t)
	database, err := url.Parse(os.Getenv("DRAMA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	database.Host = proxy
	lost, err := Open(ctx, database.String())
	if err != nil {
		t.Fatal(err)
	}
	defer lost.Close()
	arm.Store(true)
	confirmed, err := lost.CreditPaymentOrder(ctx, confirmation)
	if err != nil || confirmed.Status != p.Credited || arm.Load() {
		t.Fatal("commit response-loss recovery", confirmed, err, arm.Load())
	}
	if _, err = lost.CreditPaymentOrder(ctx, confirmation); err != nil {
		t.Fatal(err)
	}
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE source=$1`, "payment-order:"+o.OrderNo).Scan(&count); err != nil || count != 2 {
		t.Fatal("recovery duplicated money", count, err)
	}
}
func dropOneCommitResponse(t *testing.T) (string, *atomic.Bool) {
	t.Helper()
	database, err := url.Parse(os.Getenv("DRAMA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	var arm atomic.Bool
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			upstream, err := net.DialTimeout("tcp", database.Host, 3*time.Second)
			if err != nil {
				client.Close()
				continue
			}
			go func() {
				defer client.Close()
				defer upstream.Close()
				go func() { _, _ = io.Copy(upstream, client); upstream.Close() }()
				for {
					header := make([]byte, 5)
					if _, err := io.ReadFull(upstream, header); err != nil {
						return
					}
					length := int(binary.BigEndian.Uint32(header[1:]))
					if length < 4 || length > 64<<20 {
						return
					}
					body := make([]byte, length-4)
					if _, err := io.ReadFull(upstream, body); err != nil {
						return
					}
					if header[0] == 'C' && string(body) == "COMMIT\x00" && arm.CompareAndSwap(true, false) {
						return
					}
					if _, err := client.Write(append(header, body...)); err != nil {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().String(), &arm
}

func TestPaymentPGClosedPaidCreatesOneRefundWithoutCredits(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	o := f.order(t)
	if _, err := f.s.ClosePaymentOrder(ctx, o.OrderNo, "cancelled", "close"); err != nil {
		t.Fatal(err)
	}
	confirmation := paidConfirmation(o)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			closed, err := f.s.CreditPaymentOrder(ctx, confirmation)
			if err != nil || closed.Status != p.Closed {
				t.Error(closed, err)
			}
		}()
	}
	wg.Wait()
	details, err := f.s.PaymentOrderDetails(ctx, o.OrderNo)
	if err != nil || len(details.Refunds) != 1 || details.Refunds[0].AmountFen != 100 || details.Refunds[0].Kind != "late_payment" || len(details.Credits) != 0 || len(details.Events) != 3 {
		t.Fatal(details, err)
	}
	r := details.Refunds[0]
	done, err := f.s.FinishPaymentRefund(ctx, r.RefundNo, ports.ChannelRefundResult{RefundNo: r.RefundNo, OrderNo: o.OrderNo, TradeNo: "demo-" + o.OrderNo, State: "succeeded", AmountFen: r.AmountFen, TotalFen: o.AmountFen, BillAt: confirmationTime()}, "query")
	if err != nil || done.Status != "succeeded" {
		t.Fatal(done, err)
	}
	current, err := f.s.PaymentOrder(ctx, o.OrderNo)
	if err != nil || current.Status != p.Closed {
		t.Fatal(current, err)
	}
	var count int
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.payment_anomalies WHERE order_no=$1 AND kind='paid_after_close'`, o.OrderNo).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func confirmationTime() *time.Time { at := time.Now().UTC().Truncate(time.Microsecond); return &at }
func TestPaymentPGRefundReclaimsOnlyPermanentFundsAndRestoresOnce(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	o := f.order(t)
	if _, err := f.s.CreditPaymentOrder(ctx, paidConfirmation(o)); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	grantDaily := func(amount int64) {
		t.Helper()
		if _, err := f.s.Add(ctx, c.Change{AccountID: f.user, Amount: amount, Category: c.Gift, OperationID: identity.New("daily"), Source: "fixture", Subject: "fixture", Reason: "fixture", DailyExpiresAt: &expiry}); err != nil {
			t.Fatal(err)
		}
	}
	grantDaily(40)
	if _, err := f.s.Deduct(ctx, c.Change{AccountID: f.user, Amount: 130, OperationID: identity.New("spend"), Source: "fixture", Subject: "fixture", Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	grantDaily(50)
	quote, err := f.s.PaymentRefundQuote(ctx, o.OrderNo, 500)
	if err != nil || quote.MaxCredits != 1000 || quote.AmountFen != 50 || quote.GiftCredits != 10 {
		t.Fatal(quote, err)
	}
	in := p.RefundRequest{RefundID: identity.New("refund_request"), OrderNo: o.OrderNo, OperatorID: f.operator, Reason: "人工退款", Credits: 500}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.s.CreatePaymentRefund(ctx, in)
			if err != nil || r.RechargeCredits != 500 || r.GiftCredits != 10 {
				t.Error(r, err)
			}
		}()
	}
	wg.Wait()
	r, err := f.s.PaymentRefundByRequest(ctx, in.RefundID)
	if err != nil {
		t.Fatal(err)
	}
	wallet, err := f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.Balance != 550 || wallet.RechargeBalance != 500 || wallet.GiftBalance != 50 || wallet.DailyBalance != 50 {
		t.Fatal(wallet, err)
	}
	// 退款未确认时不恢复；成功还必须有真实的账单归属时刻。
	if _, err = f.s.FinishPaymentRefund(ctx, r.RefundNo, ports.ChannelRefundResult{RefundNo: r.RefundNo, State: "pending"}, "query"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.FinishPaymentRefund(ctx, r.RefundNo, ports.ChannelRefundResult{RefundNo: r.RefundNo, TradeNo: "demo-" + o.OrderNo, State: "succeeded", AmountFen: 50, TotalFen: 100}, "query"); err == nil {
		t.Fatal("missing bill time accepted")
	}
	for range 2 {
		failed, err := f.s.FinishPaymentRefund(ctx, r.RefundNo, ports.ChannelRefundResult{RefundNo: r.RefundNo, OrderNo: o.OrderNo, State: "failed"}, "query")
		if err != nil || failed.Status != "failed" {
			t.Fatal(failed, err)
		}
	}
	wallet, err = f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.Balance != 1060 || wallet.RechargeBalance != 1000 || wallet.GiftBalance != 60 || wallet.DailyBalance != 50 {
		t.Fatal("failed refund restoration", wallet, err)
	}
	var count int
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE operation_id=ANY($1)`, []string{"recharge:refund-restore:" + r.RefundNo, "gift:refund-restore:" + r.RefundNo}).Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate restore", count, err)
	}
	different := in
	different.RefundID = identity.New("different_refund")
	if _, err = f.s.CreatePaymentRefund(ctx, different); !paymentHasCode(err, "PAYMENT_REFUND_EXISTS") {
		t.Fatal("second operator refund", err)
	}
	again, err := f.s.CreatePaymentRefund(ctx, in)
	if err != nil || again.ID != r.ID || again.Status != "failed" {
		t.Fatal("refund request idempotency", again, err)
	}
}
func TestPaymentPGZeroFenRefundAndCrossDayTradeFacts(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	o := f.order(t)
	start, end := p.BeijingDay(time.Now())
	paidAt := start.Add(-time.Minute)
	confirmation := paidConfirmation(o)
	confirmation.Trade.PaidAt = &paidAt
	if _, err := f.s.CreditPaymentOrder(ctx, confirmation); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreatePaymentRefund(ctx, p.RefundRequest{RefundID: identity.New("zero_refund"), OrderNo: o.OrderNo, OperatorID: f.operator, Reason: "fixture", Credits: 1}); !paymentHasCode(err, "PAYMENT_INVALID_AMOUNT") {
		t.Fatal(err)
	}
	var count int
	if err := f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.payment_refunds WHERE order_no=$1`, o.OrderNo).Scan(&count); err != nil || count != 0 {
		t.Fatal("zero fen left refund", count, err)
	}
	wallet, err := f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.Balance != 1100 {
		t.Fatal(wallet, err)
	}
	yesterday, err := f.s.PaymentOrdersForBill(ctx, "demo", start.Add(-24*time.Hour), start)
	if err != nil || !containsPaymentOrder(yesterday, o.OrderNo) {
		t.Fatal("trade assigned to local credit day", yesterday, err)
	}
	today, err := f.s.PaymentOrdersForBill(ctx, "demo", start, end)
	if err != nil || containsPaymentOrder(today, o.OrderNo) {
		t.Fatal("local credit day leaked into trade day", today, err)
	}
	refund, err := f.s.CreatePaymentRefund(ctx, p.RefundRequest{RefundID: identity.New("real_refund"), OrderNo: o.OrderNo, OperatorID: f.operator, Reason: "fixture", Credits: 500})
	if err != nil {
		t.Fatal(err)
	}
	billAt := start.Add(-time.Second)
	if _, err = f.s.FinishPaymentRefund(ctx, refund.RefundNo, ports.ChannelRefundResult{RefundNo: refund.RefundNo, OrderNo: o.OrderNo, State: "pending", BillAt: &billAt}, "query"); err != nil {
		t.Fatal(err)
	}
	// 已存受理日可用于缺少时刻的后续成功事实，本地FinishedAt今天不改变账单日。
	done, err := f.s.FinishPaymentRefund(ctx, refund.RefundNo, ports.ChannelRefundResult{RefundNo: refund.RefundNo, OrderNo: o.OrderNo, TradeNo: "demo-" + o.OrderNo, State: "succeeded", AmountFen: 50, TotalFen: 100}, "query")
	if err != nil || done.BillAt == nil || !done.BillAt.Equal(billAt) || done.FinishedAt.Before(start) {
		t.Fatal(done, err)
	}
	today, err = f.s.PaymentOrdersForBill(ctx, "demo", start, end)
	if err != nil || containsPaymentOrder(today, o.OrderNo) {
		t.Fatal("local refund completion day leaked", today, err)
	}
	order, err := f.s.PaymentOrder(ctx, o.OrderNo)
	if err != nil || order.Status != p.Refunded {
		t.Fatal(order, err)
	}
	conflicting := confirmation
	conflicting.Trade.TradeNo = "another-trade"
	if _, err = f.s.CreditPaymentOrder(ctx, conflicting); err == nil {
		t.Fatal("frozen trade identity overwritten")
	}
}
func containsPaymentOrder(orders []p.Order, no string) bool {
	for _, o := range orders {
		if o.OrderNo == no {
			return true
		}
	}
	return false
}

func TestPaymentPGDailyLimitClaimFencesAndHistoricalBills(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	settings, err := f.s.PaymentSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.DailyLimitFen = 100
	settings, err = f.s.SavePaymentSettings(ctx, settings, f.operator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.SavePaymentSettings(ctx, p.Settings{Revision: settings.Revision - 1, DailyLimitFen: 0}, f.operator); !paymentHasCode(err, "PAYMENT_CONFLICT") {
		t.Fatal(err)
	}
	o := f.order(t)
	if _, err = f.s.CreditPaymentOrder(ctx, paidConfirmation(o)); err != nil {
		t.Fatal(err)
	}
	next := o
	next.OrderNo, _ = p.GenerateOrderNo(time.Now())
	next.ClientRequestID = identity.New("limit_request")
	if _, _, err = f.s.CreatePaymentOrder(ctx, next); !paymentHasCode(err, "PAYMENT_DAILY_LIMIT") {
		t.Fatal(err)
	}
	start, _ := p.BeijingDay(time.Now())
	if _, err = f.s.pool.Exec(ctx, `UPDATE drama_catalog_v1.payment_orders SET credited_at=$2 WHERE order_no=$1`, o.OrderNo, start.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	pending, _, err := f.s.CreatePaymentOrder(ctx, next)
	if err != nil {
		t.Fatal("previous Beijing day counted", err)
	}
	now := time.Now().Add(time.Minute)
	if err = f.s.SchedulePaymentOrder(ctx, pending.OrderNo, 0, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	first, err := f.s.ClaimPaymentOrders(ctx, now, 100, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var original p.Order
	for _, v := range first {
		if v.OrderNo == pending.OrderNo {
			original = v
		}
	}
	if original.QueryAttempts != 1 {
		t.Fatal(first)
	}
	_, err = f.s.ClaimPaymentOrders(ctx, now.Add(2*time.Minute), 100, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.SchedulePaymentOrder(ctx, pending.OrderNo, original.QueryAttempts, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, err := f.s.PaymentOrder(ctx, pending.OrderNo)
	if err != nil || after.QueryAttempts != 2 || !after.NextQueryAt.Equal(now.Add(3*time.Minute).Truncate(time.Microsecond)) {
		t.Fatal("stale scanner overwrote lease", after, err)
	}
	// 全局账单表的本轮channel记录单独清理，测试只使用专属数据库。
	if _, err = f.s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.payment_bills WHERE channel='wechatpay'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.s.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.payment_bills WHERE channel='wechatpay'`)
	})
	date := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	claim, ok, err := f.s.ClaimPaymentBill(ctx, "wechatpay", &date, now)
	if err != nil || !ok || claim.Attempts != 1 {
		t.Fatal(claim, ok, err)
	}
	if err = f.s.FinishPaymentBill(ctx, "wechatpay", claim, false, nil); err != nil {
		t.Fatal(err)
	}
	tomorrow := date.Add(24 * time.Hour)
	nextClaim, ok, err := f.s.ClaimPaymentBill(ctx, "wechatpay", &tomorrow, now.Add(time.Hour))
	if err != nil || !ok || !nextClaim.Date.Equal(date) || nextClaim.Attempts != 2 {
		t.Fatal("failed historical bill abandoned", nextClaim, ok, err)
	}
	if err = f.s.FinishPaymentBill(ctx, "wechatpay", claim, true, nil); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = f.s.pool.QueryRow(ctx, `SELECT status FROM drama_catalog_v1.payment_bills WHERE channel='wechatpay' AND bill_date=$1`, date.Format("2006-01-02")).Scan(&status); err != nil || status != "pending" {
		t.Fatal("late bill worker overwrote new claim", status, err)
	}
	if err = f.s.FinishPaymentBill(ctx, "wechatpay", nextClaim, true, map[string]int64{"matched": 1}); err != nil {
		t.Fatal(err)
	}
	last := date.Add(3 * 24 * time.Hour)
	claim, ok, err = f.s.ClaimPaymentBill(ctx, "wechatpay", &last, now.Add(2*time.Hour))
	if err != nil || !ok || !claim.Date.Equal(tomorrow) {
		t.Fatal(claim, ok, err)
	}
	for i := claim.Attempts; i < 24; i++ {
		if err = f.s.FinishPaymentBill(ctx, "wechatpay", claim, false, nil); err != nil {
			t.Fatal(err)
		}
		claim, ok, err = f.s.ClaimPaymentBill(ctx, "wechatpay", nil, now.Add(time.Duration(i+2)*time.Hour))
		if err != nil || !ok || !claim.Date.Equal(tomorrow) {
			t.Fatal(claim, ok, err)
		}
	}
	if err = f.s.FinishPaymentBill(ctx, "wechatpay", claim, false, nil); err != nil {
		t.Fatal(err)
	}
	claim, ok, err = f.s.ClaimPaymentBill(ctx, "wechatpay", nil, now.Add(30*time.Hour))
	if err != nil || !ok || claim.Date.Equal(tomorrow) {
		t.Fatal("24-attempt bill reclaimed", claim, ok, err)
	}
}

func TestPaymentPGRefundMidTransactionFailureAndConcurrentClaims(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	o := f.order(t)
	if _, err := f.s.CreditPaymentOrder(ctx, paidConfirmation(o)); err != nil {
		t.Fatal(err)
	}
	in := p.RefundRequest{RefundID: identity.New("refund_atomic"), OrderNo: o.OrderNo, OperatorID: f.operator, Reason: "fixture", Credits: 500}
	suffix := identity.New("refund_fail")
	operation := "gift:reclaim:" + paymentRefundNumber(in.RefundID)
	sql := fmt.Sprintf(`CREATE FUNCTION drama_catalog_v1.%s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture gift reclaim rejected'; END $$; CREATE TRIGGER %s BEFORE INSERT ON drama_catalog_v1.credit_ledger FOR EACH ROW WHEN (NEW.operation_id='%s') EXECUTE FUNCTION drama_catalog_v1.%s()`, suffix, suffix, operation, suffix)
	if _, err := f.s.pool.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_, _ = f.s.pool.Exec(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON drama_catalog_v1.credit_ledger; DROP FUNCTION IF EXISTS drama_catalog_v1.%s()`, suffix, suffix))
	}
	t.Cleanup(cleanup)
	if _, err := f.s.CreatePaymentRefund(ctx, in); err == nil {
		t.Fatal("gift reclaim failure accepted")
	}
	wallet, err := f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.Balance != 1100 || wallet.RechargeBalance != 1000 || wallet.GiftBalance != 100 {
		t.Fatal("refund failure changed funds", wallet, err)
	}
	if _, err = f.s.PaymentRefundByRequest(ctx, in.RefundID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("refund row survived failure", err)
	}
	var count int
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE source=$1`, "payment-order:"+o.OrderNo).Scan(&count); err != nil || count != 2 {
		t.Fatal("partial reclaim survived", count, err)
	}
	cleanup()
	r, err := f.s.CreatePaymentRefund(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Second)
	var wg sync.WaitGroup
	claimed := make(chan []p.Refund, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := f.s.ClaimPaymentRefunds(ctx, now, 100, time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			claimed <- rows
		}()
	}
	wg.Wait()
	close(claimed)
	seen := map[string]bool{}
	found := false
	for rows := range claimed {
		for _, row := range rows {
			if seen[row.ID] {
				t.Fatal("duplicate refund lease", row.ID)
			}
			seen[row.ID] = true
			if row.ID == r.ID {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("new refund not claimed")
	}
	// 两个首次相同请求并发时只允许一个调用方创建渠道订单。
	user := identity.New("first_create")
	if _, err = f.s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$1||'@test.invalid','fixture','user','fixture'::bytea)`, user); err != nil {
		t.Fatal(err)
	}
	no, _ := p.GenerateOrderNo(time.Now())
	newOrder := p.Order{OrderNo: no, BuyerUserID: user, ClientRequestID: identity.New("first_request"), CreditAccountID: user, Target: p.Target{Kind: "personal", ID: user, Name: "个人空间"}, PackageSnapshot: f.pkg.Snapshot(), Channel: "demo", AgreementVersion: p.AgreementVersion}
	var made atomic.Int32
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, created, err := f.s.CreatePaymentOrder(ctx, newOrder)
			if err != nil || got.OrderNo != no {
				t.Error(got, err)
			}
			if created {
				made.Add(1)
			}
		}()
	}
	wg.Wait()
	if made.Load() != 1 {
		t.Fatal("first concurrent request created more than once", made.Load())
	}
	now = time.Now().Add(time.Minute)
	orders := make(chan []p.Order, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := f.s.ClaimPaymentOrders(ctx, now, 100, time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			orders <- rows
		}()
	}
	wg.Wait()
	close(orders)
	seen = map[string]bool{}
	found = false
	for rows := range orders {
		for _, row := range rows {
			if seen[row.OrderNo] {
				t.Fatal("duplicate order lease", row.OrderNo)
			}
			seen[row.OrderNo] = true
			if row.OrderNo == no {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("new order not claimed")
	}
}

func TestPaymentPGRefundRestoreCapacityKeepsPendingAndAuditsOnce(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	pkg := f.pkg
	pkg.AmountFen = 1
	pkg.RechargeCredits = 1
	pkg.GiftCredits = 0
	var err error
	f.pkg, err = f.s.SavePaymentPackage(ctx, pkg, f.operator)
	if err != nil {
		t.Fatal(err)
	}
	o := f.order(t)
	if _, err = f.s.CreditPaymentOrder(ctx, paidConfirmation(o)); err != nil {
		t.Fatal(err)
	}
	refund, err := f.s.CreatePaymentRefund(ctx, p.RefundRequest{RefundID: identity.New("capacity_refund"), OrderNo: o.OrderNo, OperatorID: f.operator, Reason: "fixture", Credits: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Add(ctx, c.Change{AccountID: f.user, Amount: c.MaxAmount, Category: c.Recharge, OperationID: identity.New("fill_capacity"), Source: "fixture", Subject: "fixture", Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	result := ports.ChannelRefundResult{RefundNo: refund.RefundNo, OrderNo: o.OrderNo, State: "failed"}
	for range 2 {
		if _, err = f.s.FinishPaymentRefund(ctx, refund.RefundNo, result, "query"); !paymentHasCode(err, "PAYMENT_REFUND_RESTORE_PENDING") {
			t.Fatal(err)
		}
		// 应用在原事务回滚后独立保存已核实的受阻证据；不会宣称恢复已完成。
		if err = f.s.RecordPaymentAnomaly(ctx, p.Anomaly{ID: "refund-restore-pending:" + refund.RefundNo, OrderNo: o.OrderNo, Channel: "demo", Kind: "refund_failed", Detail: map[string]any{"refundNo": refund.RefundNo, "stage": "restore_pending"}}); err != nil {
			t.Fatal(err)
		}
		if err = f.s.RecordPaymentEvent(ctx, p.Event{ID: "refund-restore-pending:" + refund.RefundNo, OrderNo: o.OrderNo, RefundID: refund.ID, Source: "refund", Outcome: "restore_pending", Facts: map[string]any{"refundNo": refund.RefundNo, "stage": "restore_pending"}}); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := f.s.PaymentRefund(ctx, refund.RefundNo)
	if err != nil || saved.Status != "pending" {
		t.Fatal(saved, err)
	}
	wallet, err := f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.Balance != c.MaxAmount {
		t.Fatal(wallet, err)
	}
	details, err := f.s.PaymentOrderDetails(ctx, o.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	evidence := 0
	for _, e := range details.Events {
		if e.Outcome == "restore_pending" {
			evidence++
		}
	}
	if evidence != 1 {
		t.Fatal("duplicate blocked evidence", details.Events)
	}
	var count int
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.payment_anomalies WHERE id=$1`, "refund-restore-pending:"+refund.RefundNo).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = f.s.Deduct(ctx, c.Change{AccountID: f.user, Amount: 1, OperationID: identity.New("make_room"), Source: "fixture", Subject: "fixture", Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		done, err := f.s.FinishPaymentRefund(ctx, refund.RefundNo, result, "query")
		if err != nil || done.Status != "failed" {
			t.Fatal(done, err)
		}
	}
	wallet, err = f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.Balance != c.MaxAmount {
		t.Fatal("restoration after capacity available", wallet, err)
	}
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE operation_id=$1`, "recharge:refund-restore:"+refund.RefundNo).Scan(&count); err != nil || count != 1 {
		t.Fatal("restored more than once", count, err)
	}
}
func TestPaymentPGConcurrentDifferentTradeIdentityIsRejected(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	o := f.order(t)
	confirmation := paidConfirmation(o)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var accepted, rejected atomic.Int32
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			in := confirmation
			in.Trade.TradeNo = fmt.Sprintf("trade-%d", i)
			if _, err := f.s.CreditPaymentOrder(ctx, in); err == nil {
				accepted.Add(1)
			} else if paymentHasCode(err, "PAYMENT_CHANNEL_FAILED") {
				rejected.Add(1)
			} else {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if accepted.Load() != 1 || rejected.Load() != 1 {
		t.Fatal(accepted.Load(), rejected.Load())
	}
	current, err := f.s.PaymentOrder(ctx, o.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	confirmation.Trade.TradeNo = current.ChannelTradeNo
	changed := confirmation.Trade.PaidAt.Add(time.Second)
	confirmation.Trade.PaidAt = &changed
	if _, err = f.s.CreditPaymentOrder(ctx, confirmation); err == nil {
		t.Fatal("changed frozen paid time")
	}
}

func TestPaymentPGUnlimitedDailyLimitSkipsUnsafeLifetimeSum(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	settings, err := f.s.PaymentSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.DailyLimitFen = 0
	settings, err = f.s.SavePaymentSettings(ctx, settings, f.operator)
	if err != nil {
		t.Fatal(err)
	}
	pkg := f.pkg
	pkg.AmountFen = p.MaxAmount
	pkg.OriginalAmountFen = p.MaxAmount
	pkg.RechargeCredits = 1
	pkg.GiftCredits = 0
	f.pkg, err = f.s.SavePaymentPackage(ctx, pkg, f.operator)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		o := f.order(t)
		if _, err = f.s.CreditPaymentOrder(ctx, paidConfirmation(o)); err != nil {
			t.Fatal(err)
		}
	}
	// 两笔各合法安全整数的累计可超安全上界；0不限仍允许新订单。
	third := f.order(t)
	if _, err = f.s.ClosePaymentOrder(ctx, third.OrderNo, "fixture", "close"); err != nil {
		t.Fatal(err)
	}
	settings.DailyLimitFen = p.MaxAmount
	settings, err = f.s.SavePaymentSettings(ctx, settings, f.operator)
	if err != nil {
		t.Fatal(err)
	}
	next := third
	next.OrderNo, _ = p.GenerateOrderNo(time.Now())
	next.ClientRequestID = identity.New("finite_after_large")
	if _, _, err = f.s.CreatePaymentOrder(ctx, next); !paymentHasCode(err, "PAYMENT_DAILY_LIMIT") {
		t.Fatal("large finite sum did not reject safely", err)
	}
	// 自造大额订单事实使PG numeric SUM超int64；有限上限只需饱和比较，不改历史订单。
	if _, err = f.s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.payment_orders(order_no,buyer_user_id,client_request_id,target_kind,target_id,target_name,credit_account_id,package_id,original_amount_fen,amount_fen,recharge_credits,gift_credits,channel,status,agreement_version,expires_at,next_query_at,created_at,credited_at,channel_paid_at)
 SELECT $5::text||i,$1,$1||':sum:'||i,'personal',$1,'fixture',$1,$2,$3,$3,1,0,'demo','credited',$4,clock_timestamp()+interval '15 minutes',clock_timestamp(),clock_timestamp(),clock_timestamp(),clock_timestamp() FROM generate_series(1,1025)i`, f.user, f.pkg.ID, p.MaxAmount, p.AgreementVersion, identity.New("payment_sum_fact")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.s.CreatePaymentOrder(ctx, next); !paymentHasCode(err, "PAYMENT_DAILY_LIMIT") {
		t.Fatal("numeric SUM overflow escaped finite limit", err)
	}
	settings.DailyLimitFen = 0
	if _, err = f.s.SavePaymentSettings(ctx, settings, f.operator); err != nil {
		t.Fatal(err)
	}
	if _, created, err := f.s.CreatePaymentOrder(ctx, next); err != nil || !created {
		t.Fatal("unlimited path still computed unsafe sum", created, err)
	}
}

func TestPaymentPGConfigurationWithoutReasonsKeepsAuditAndCAS(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	changed := f.pkg
	changed.Name = "无需原因的配置"
	changed.OriginalAmountFen = 125
	saved, err := f.s.SavePaymentPackage(ctx, changed, f.operator)
	if err != nil || saved.Revision != changed.Revision+1 {
		t.Fatal(saved, err)
	}
	if _, err = f.s.SavePaymentPackage(ctx, changed, f.operator); !paymentHasCode(err, "PAYMENT_CONFLICT") {
		t.Fatal("stale package accepted", err)
	}
	settings, err := f.s.PaymentSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.s.SavePaymentSettings(ctx, settings, f.operator)
	if err != nil || updated.Revision != settings.Revision+1 {
		t.Fatal(updated, err)
	}
	if _, err = f.s.SavePaymentSettings(ctx, settings, f.operator); !paymentHasCode(err, "PAYMENT_CONFLICT") {
		t.Fatal("stale settings accepted", err)
	}
	var packageAudits, settingsAudits int
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.payment_events WHERE source='package' AND outcome='saved' AND operator_id=$1 AND reason='' AND created_at IS NOT NULL AND facts->>'packageId'=$2 AND facts->>'revision'=$3`, f.operator, saved.ID, fmt.Sprint(saved.Revision)).Scan(&packageAudits); err != nil || packageAudits != 1 {
		t.Fatal("package audit missing or conflict was audited as saved", packageAudits, err)
	}
	if err = f.s.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.payment_events WHERE source='settings' AND outcome='saved' AND operator_id=$1 AND reason='' AND created_at IS NOT NULL AND facts->>'revision'=$2 AND facts->>'dailyLimitFen'=$3`, f.operator, fmt.Sprint(updated.Revision), fmt.Sprint(updated.DailyLimitFen)).Scan(&settingsAudits); err != nil || settingsAudits != 1 {
		t.Fatal("settings audit missing or conflict was audited as saved", settingsAudits, err)
	}
	if _, err = f.s.SavePaymentPackage(ctx, saved, ""); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("empty actor accepted", err)
	}
	if _, err = f.s.SavePaymentSettings(ctx, updated, ""); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("empty actor accepted", err)
	}
}
func TestPaymentPGTeamRefundOnlyReclaimsUnallocatedCredits(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	team := identity.New("team")
	no, err := p.GenerateOrderNo(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	o, created, err := f.s.CreatePaymentOrder(ctx, p.Order{OrderNo: no, BuyerUserID: f.user, ClientRequestID: identity.New("request"), Target: p.Target{Kind: "team", ID: team, Name: "测试团队"}, CreditAccountID: team, PackageSnapshot: f.pkg.Snapshot(), Channel: "demo", AgreementVersion: p.AgreementVersion})
	if err != nil || !created {
		t.Fatal(o, created, err)
	}
	if _, err = f.s.CreditPaymentOrder(ctx, paidConfirmation(o)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Allocate(ctx, c.AllocationChange{AccountID: team, HolderID: "member", OperationID: identity.New("allocate"), ActorID: f.user, Amount: 700}); err != nil {
		t.Fatal(err)
	}
	// 未分配=1100-700=400：充值部分以此为上限，赠送扣回不再动成员名下积分。
	quote, err := f.s.PaymentRefundQuote(ctx, o.OrderNo, 0)
	if err != nil || quote.MaxCredits != 400 || quote.RechargeCredits != 400 || quote.GiftCredits != 0 || quote.AmountFen != 40 {
		t.Fatal(quote, err)
	}
	if _, err = f.s.PaymentRefundQuote(ctx, o.OrderNo, 401); !paymentHasCode(err, "PAYMENT_REFUND_EXCEEDED") {
		t.Fatal("team refund above unallocated", err)
	}
	quote, err = f.s.PaymentRefundQuote(ctx, o.OrderNo, 300)
	if err != nil || quote.RechargeCredits != 300 || quote.GiftCredits != 30 {
		t.Fatal(quote, err)
	}
	r, err := f.s.CreatePaymentRefund(ctx, p.RefundRequest{RefundID: identity.New("team_refund"), OrderNo: o.OrderNo, OperatorID: f.operator, Reason: "团队退款", Credits: 400})
	if err != nil || r.RechargeCredits != 400 || r.GiftCredits != 0 || r.AmountFen != 40 {
		t.Fatal(r, err)
	}
	balances, err := f.s.CreditBalances(ctx, team)
	if err != nil || balances.Wallet.Balance != 700 || balances.Allocated != 700 {
		t.Fatal(balances, err)
	}
}
