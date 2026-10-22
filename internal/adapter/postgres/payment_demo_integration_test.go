package postgres

import (
	"context"
	"drama-agent/internal/adapter/demopay"
	app "drama-agent/internal/application/payment"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/payment"
	"os"
	"sync"
	"testing"
	"time"
)

func TestDemoPaymentPGRestartConcurrentPaymentAndRefund(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	o := f.order(t)
	channel, err := demopay.New(f.s)
	if err != nil {
		t.Fatal(err)
	}
	input := ports.ChannelOrder{OrderNo: o.OrderNo, AmountFen: o.AmountFen, Description: "演示充值", ExpiresAt: o.ExpiresAt}
	code, err := channel.Create(ctx, input)
	if err != nil || code != "demo://"+o.OrderNo {
		t.Fatal(code, err)
	}
	if again, err := channel.Create(ctx, input); err != nil || again != code {
		t.Fatal(again, err)
	}
	different := input
	different.AmountFen++
	if _, err = channel.Create(ctx, different); err == nil {
		t.Fatal("same order changed amount")
	}
	service := &app.Service{Store: f.s, Channels: map[string]ports.PaymentChannel{"demo": channel}, Environment: "development"}
	buyer := ports.Actor{UserID: f.user, Role: ports.RoleUser}
	if failed, err := service.Simulate(ctx, buyer, o.OrderNo, "failed"); err != nil || failed.Status != p.Pending {
		t.Fatal(failed, err)
	}
	if trade, err := channel.Query(ctx, o.OrderNo); err != nil || trade.State != "failed" {
		t.Fatal(trade, err)
	}
	var wg sync.WaitGroup
	paid := make(chan ports.ChannelTrade, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trade, err := channel.Simulate(ctx, o.OrderNo, "paid")
			if err != nil {
				t.Error(err)
				return
			}
			paid <- trade
		}()
	}
	wg.Wait()
	close(paid)
	var first ports.ChannelTrade
	for trade := range paid {
		if trade.State != "paid" || trade.PaidAt == nil || trade.TradeNo != "demo:"+o.OrderNo {
			t.Fatal(trade)
		}
		if first.PaidAt == nil {
			first = trade
		} else if !trade.PaidAt.Equal(*first.PaidAt) {
			t.Fatal("concurrent payment rewrote paidAt", trade, first)
		}
	}
	confirmed, err := service.ConfirmPaid(ctx, "demo", o.OrderNo, first, "simulate")
	if err != nil || confirmed.Status != p.Credited {
		t.Fatal(confirmed, err)
	}
	wallet, err := f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.RechargeBalance != 1000 || wallet.GiftBalance != 100 {
		t.Fatal("demo bypassed normal credit transaction", wallet, err)
	}
	if _, err = channel.Simulate(ctx, o.OrderNo, "failed"); err == nil {
		t.Fatal("paid fact regressed")
	}
	if err = channel.Close(ctx, o.OrderNo); err == nil {
		t.Fatal("paid channel trade closed")
	}
	// 重开真实PG连接和适配器，模拟服务重启后不依赖进程内状态。
	reopened, err := Open(ctx, os.Getenv("DRAMA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := demopay.New(reopened)
	if err != nil {
		t.Fatal(err)
	}
	trade, err := restarted.Query(ctx, o.OrderNo)
	if err != nil || trade.TradeNo != first.TradeNo || !trade.PaidAt.Equal(*first.PaidAt) {
		t.Fatal(trade, err)
	}
	refundNo := identity.New("demo_refund")
	if result, err := restarted.QueryRefund(ctx, refundNo, o.OrderNo); err != nil || result.State != "not_found" {
		t.Fatal("nonexistent refund was successful", result, err)
	}
	refund := ports.ChannelRefund{RefundNo: refundNo, OrderNo: o.OrderNo, TradeNo: first.TradeNo, AmountFen: 50, TotalFen: o.AmountFen, Reason: "fixture"}
	refunds := make(chan ports.ChannelRefundResult, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := restarted.Refund(ctx, refund)
			if err != nil {
				t.Error(err)
				return
			}
			refunds <- result
		}()
	}
	wg.Wait()
	close(refunds)
	var billAt *time.Time
	for result := range refunds {
		if result.State != "succeeded" || result.AmountFen != 50 || result.OrderNo != o.OrderNo || result.BillAt == nil {
			t.Fatal(result)
		}
		if billAt == nil {
			billAt = result.BillAt
		} else if !billAt.Equal(*result.BillAt) {
			t.Fatal("concurrent refund changed bill date")
		}
	}
	// 渠道退款不能改变已冻结的付款时间，也不能借另一个退款号再次退款。
	trade, err = restarted.Query(ctx, o.OrderNo)
	if err != nil || !trade.PaidAt.Equal(*first.PaidAt) {
		t.Fatal("refund changed payment date", trade, err)
	}
	changedRefund := refund
	changedRefund.AmountFen = 25
	if _, err = restarted.Refund(ctx, changedRefund); err == nil {
		t.Fatal("same refund changed amount")
	}
	changedRefund = refund
	changedRefund.RefundNo = identity.New("second_refund")
	if _, err = restarted.Refund(ctx, changedRefund); err == nil {
		t.Fatal("second refund for same trade")
	}
	if missing, err := restarted.QueryRefund(ctx, changedRefund.RefundNo, o.OrderNo); err != nil || missing.State != "not_found" {
		t.Fatal(missing, err)
	}
	other, _ := p.GenerateOrderNo(time.Now())
	otherInput := input
	otherInput.OrderNo = other
	if _, err = restarted.Create(ctx, otherInput); err != nil {
		t.Fatal(err)
	}
	otherTrade, err := restarted.Simulate(ctx, other, "paid")
	if err != nil {
		t.Fatal(err)
	}
	changedRefund = refund
	changedRefund.OrderNo = other
	changedRefund.TradeNo = otherTrade.TradeNo
	if _, err = restarted.Refund(ctx, changedRefund); err == nil {
		t.Fatal("same refund moved to another trade")
	}
	// 再次重开仅只读回执，仍是同一退款原号、金额和渠道账单时间。
	third, err := demopay.New(f.s)
	if err != nil {
		t.Fatal(err)
	}
	queried, err := third.QueryRefund(ctx, refundNo, o.OrderNo)
	if err != nil || queried.AmountFen != 50 || !queried.BillAt.Equal(*billAt) {
		t.Fatal(queried, err)
	}
}

func TestDemoPaymentPGClosedLatePaymentHonorsDevelopmentGate(t *testing.T) {
	f := newPaymentPGFixture(t)
	ctx := context.Background()
	o := f.order(t)
	channel, err := demopay.New(f.s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = channel.Create(ctx, ports.ChannelOrder{OrderNo: o.OrderNo, AmountFen: o.AmountFen, ExpiresAt: o.ExpiresAt}); err != nil {
		t.Fatal(err)
	}
	if err = channel.Close(ctx, o.OrderNo); err != nil {
		t.Fatal(err)
	}
	if err = channel.Close(ctx, o.OrderNo); err != nil {
		t.Fatal("repeat close", err)
	}
	if _, err = f.s.ClosePaymentOrder(ctx, o.OrderNo, "cancelled", "close"); err != nil {
		t.Fatal(err)
	}
	service := &app.Service{Store: f.s, Channels: map[string]ports.PaymentChannel{"demo": channel}, Environment: "staging"}
	buyer := ports.Actor{UserID: f.user, Role: ports.RoleUser}
	if _, err = service.Simulate(ctx, buyer, o.OrderNo, "paid"); !paymentHasCode(err, "PAYMENT_CONFLICT") {
		t.Fatal("staging late simulation allowed", err)
	}
	trade, err := channel.Query(ctx, o.OrderNo)
	if err != nil || trade.State != "closed" {
		t.Fatal(trade, err)
	}
	service.Environment = "development"
	order, err := service.Simulate(ctx, buyer, o.OrderNo, "paid")
	if err != nil || order.Status != p.Closed {
		t.Fatal(order, err)
	}
	details, err := f.s.PaymentOrderDetails(ctx, o.OrderNo)
	if err != nil || len(details.Credits) != 0 || len(details.Refunds) != 1 || details.Refunds[0].Kind != "late_payment" {
		t.Fatal(details, err)
	}
	r := details.Refunds[0]
	result, err := channel.Refund(ctx, ports.ChannelRefund{RefundNo: r.RefundNo, OrderNo: o.OrderNo, TradeNo: order.ChannelTradeNo, AmountFen: r.AmountFen, TotalFen: o.AmountFen})
	if err != nil {
		t.Fatal(err)
	}
	bad := result
	bad.TotalFen++
	if _, err = f.s.FinishPaymentRefund(ctx, r.RefundNo, bad, "query"); !paymentHasCode(err, "PAYMENT_CHANNEL_FAILED") {
		t.Fatal("wrong total accepted", err)
	}
	bad = result
	bad.TradeNo = "another-channel-trade"
	if _, err = f.s.FinishPaymentRefund(ctx, r.RefundNo, bad, "query"); !paymentHasCode(err, "PAYMENT_CHANNEL_FAILED") {
		t.Fatal("wrong trade identity accepted", err)
	}
	bad = result
	bad.State = "pending"
	bad.TotalFen++
	if _, err = f.s.FinishPaymentRefund(ctx, r.RefundNo, bad, "query"); !paymentHasCode(err, "PAYMENT_CHANNEL_FAILED") {
		t.Fatal("wrong total persisted as pending evidence", err)
	}
	if _, err = service.ConfirmRefund(ctx, "demo", result, "query"); err != nil {
		t.Fatal(err)
	}
	current, err := f.s.PaymentOrder(ctx, o.OrderNo)
	if err != nil || current.Status != p.Closed {
		t.Fatal(current, err)
	}
	wallet, err := f.s.CreditWallet(ctx, f.user)
	if err != nil || wallet.Balance != 0 {
		t.Fatal("late demo payment credited", wallet, err)
	}
}
