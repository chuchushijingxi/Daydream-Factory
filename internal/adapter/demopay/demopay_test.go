package demopay

import (
	"context"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
	"testing"
	"time"
)

type fakeStore struct {
	trade    ports.DemoPaymentTrade
	err      error
	created  bool
	refunded ports.ChannelRefund
}

func (s *fakeStore) CreateDemoPayment(_ context.Context, no string, amount int64) (ports.DemoPaymentTrade, error) {
	s.created = true
	return s.trade, s.err
}
func (s *fakeStore) DemoPayment(context.Context, string) (ports.DemoPaymentTrade, error) {
	return s.trade, s.err
}
func (s *fakeStore) CloseDemoPayment(context.Context, string) (ports.DemoPaymentTrade, error) {
	return s.trade, s.err
}
func (s *fakeStore) SimulateDemoPayment(context.Context, string, string) (ports.DemoPaymentTrade, error) {
	return s.trade, s.err
}
func (s *fakeStore) RefundDemoPayment(_ context.Context, r ports.ChannelRefund) (ports.DemoPaymentTrade, error) {
	s.refunded = r
	return s.trade, s.err
}
func TestDemoChannelMapsPersistentFactsAndRejectsInvalidInputs(t *testing.T) {
	ctx := context.Background()
	no, _ := p.GenerateOrderNo(time.Now())
	at := time.Now().UTC()
	store := &fakeStore{trade: ports.DemoPaymentTrade{OrderNo: no, State: "notpay", AmountFen: 100}}
	client, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	if client.ID() != "demo" {
		t.Fatal(client.ID())
	}
	code, err := client.Create(ctx, ports.ChannelOrder{OrderNo: no, AmountFen: 100, ExpiresAt: at.Add(time.Minute)})
	if err != nil || code != "demo://"+no || !store.created {
		t.Fatal(code, err)
	}
	for _, in := range []ports.ChannelOrder{{OrderNo: no, AmountFen: 0, ExpiresAt: at.Add(time.Hour)}, {OrderNo: no, AmountFen: 1, ExpiresAt: at.Add(-time.Hour)}, {OrderNo: "invalid", AmountFen: 1, ExpiresAt: at.Add(time.Hour)}} {
		if _, err = client.Create(ctx, in); err == nil {
			t.Fatal(in)
		}
	}
	store.trade.State = "paid"
	store.trade.PaidAt = &at
	paid, err := client.Query(ctx, no)
	if err != nil || paid.TradeNo != "demo:"+no || paid.PaidAt != &at || paid.AmountFen != 100 {
		t.Fatal(paid, err)
	}
	store.trade.RefundNo = "refund"
	store.trade.RefundedFen = 50
	store.trade.RefundBillAt = &at
	refund, err := client.Refund(ctx, ports.ChannelRefund{RefundNo: "refund", OrderNo: no, TradeNo: "demo:" + no, AmountFen: 50, TotalFen: 100})
	if err != nil || refund.State != "succeeded" || refund.AmountFen != 50 || refund.BillAt != &at || store.refunded.RefundNo != "refund" {
		t.Fatal(refund, err)
	}
	if result, err := client.QueryRefund(ctx, "missing", no); err != nil || result.State != "not_found" {
		t.Fatal(result, err)
	}
	if _, err = client.Refund(ctx, ports.ChannelRefund{RefundNo: "refund", OrderNo: no, TradeNo: "different", AmountFen: 50, TotalFen: 100}); err == nil {
		t.Fatal("trade identity not checked")
	}
	store.err = fault.ErrNotFound
	if err = client.Close(ctx, no); err != nil {
		t.Fatal("missing close not idempotent", err)
	}
	if result, err := client.Query(ctx, no); err != nil || result.State != "not_found" {
		t.Fatal(result, err)
	}
	if result, err := client.QueryRefund(ctx, "refund", no); err != nil || result.State != "not_found" {
		t.Fatal(result, err)
	}
	if _, _, err = client.ParseNotify(ctx, ports.NotifyInput{}); err == nil {
		t.Fatal("demo external callback accepted")
	}
	if _, err = client.Bill(ctx, at); err == nil {
		t.Fatal("unsupported bill was empty success")
	}
	if _, err = New(nil); err == nil {
		t.Fatal("nil store accepted")
	}
}
func TestDemoUnknownOrIncompleteFactsDoNotBecomeSuccessfulPayments(t *testing.T) {
	no, _ := p.GenerateOrderNo(time.Now())
	store := &fakeStore{trade: ports.DemoPaymentTrade{OrderNo: no, State: "paid", AmountFen: 1}}
	client, _ := New(store)
	if _, err := client.Query(context.Background(), no); err == nil {
		t.Fatal("paid without time")
	}
	store.trade.State = "future_state"
	if _, err := client.Query(context.Background(), no); err == nil {
		t.Fatal("unknown state defaulted")
	}
	if _, err := client.Simulate(context.Background(), no, "future_state"); err == nil {
		t.Fatal("unknown simulation accepted")
	}
}
