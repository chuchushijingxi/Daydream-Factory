package payment

import (
	"context"
	"testing"

	"drama-agent/internal/application/ports"
	p "drama-agent/internal/domain/payment"
)

func TestRefundSuccessFillsOnlyVerifiedOriginalTradeFacts(t *testing.T) {
	tests := []struct {
		name, code string
		change     func(*ports.ChannelRefundResult)
	}{
		{name: "full evidence"},
		{name: "missing total", code: "PAYMENT_CHANNEL_FAILED", change: func(r *ports.ChannelRefundResult) { r.TotalFen = 0 }},
		{name: "missing trade", code: "PAYMENT_CHANNEL_FAILED", change: func(r *ports.ChannelRefundResult) { r.TradeNo = "" }},
		{name: "wrong total", code: "PAYMENT_CONFLICT", change: func(r *ports.ChannelRefundResult) { r.TotalFen = 99 }},
		{name: "wrong trade", code: "PAYMENT_CONFLICT", change: func(r *ports.ChannelRefundResult) { r.TradeNo = "another" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, store, ch := newFixture()
			store.orders["o"] = order("o", p.Credited)
			store.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", AmountFen: 50, Status: "pending"}
			ch.queryRefund = func(_ context.Context, no, order string) (ports.ChannelRefundResult, error) {
				result := ports.ChannelRefundResult{RefundNo: no, OrderNo: order, AmountFen: 50, TotalFen: 100, TradeNo: "trade-o", State: "succeeded", BillAt: &testNow}
				if test.change != nil {
					test.change(&result)
				}
				return result, nil
			}
			out, err := s.ConfirmRefund(context.Background(), "demo", ports.ChannelRefundResult{RefundNo: "r", OrderNo: "o", AmountFen: 50, State: "succeeded"}, "notify")
			if test.code != "" {
				requireCode(t, err, test.code)
				if store.refundFinishes != 0 || store.refunds["r"].Status != "pending" {
					t.Fatal("missing or mismatched evidence finalized refund")
				}
			} else if err != nil || out.Status != "succeeded" || store.lastRefundResult.TotalFen != 100 || store.lastRefundResult.TradeNo != "trade-o" || ch.refundQueries != 1 {
				t.Fatalf("verified facts not forwarded %+v %v", store.lastRefundResult, err)
			}
		})
	}
}
func TestNonzeroRefundOriginalFactsAlwaysMatchFrozenOrder(t *testing.T) {
	for _, state := range []string{"pending", "succeeded", "failed"} {
		s, store, _ := newFixture()
		store.orders["o"] = order("o", p.Credited)
		store.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", AmountFen: 50, Status: "pending"}
		_, err := s.ConfirmRefund(context.Background(), "demo", ports.ChannelRefundResult{RefundNo: "r", OrderNo: "o", AmountFen: 50, TotalFen: 101, TradeNo: "trade-o", State: state}, "query")
		requireCode(t, err, "PAYMENT_CONFLICT")
		if store.refundFinishes != 0 || len(store.anomalies) != 1 {
			t.Fatal("original total mismatch not rejected")
		}
	}
}

func TestNotFoundRefundWithConflictingAttachedFactsNeverRetries(t *testing.T) {
	for _, change := range []func(*ports.ChannelRefundResult){
		func(r *ports.ChannelRefundResult) { r.TradeNo = "wrong-trade" },
		func(r *ports.ChannelRefundResult) { r.TotalFen = 101 },
		func(r *ports.ChannelRefundResult) { r.AmountFen = 51 },
	} {
		s, store, ch := newFixture()
		store.orders["o"] = order("o", p.Credited)
		refund := p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", AmountFen: 50, Status: "pending", Attempts: 2}
		store.refunds["r"] = refund
		store.claimRefunds = []p.Refund{refund}
		ch.queryRefund = func(_ context.Context, no, order string) (ports.ChannelRefundResult, error) {
			r := ports.ChannelRefundResult{RefundNo: no, OrderNo: order, State: "not_found"}
			change(&r)
			return r, nil
		}
		err := s.ScanRefunds(context.Background())
		requireCode(t, err, "PAYMENT_CONFLICT")
		if ch.refundCalls != 0 || store.refundFinishes != 0 || len(store.anomalies) != 1 || store.anomalies[0].Kind != "refund_mismatch" {
			t.Fatal("wrong negative fact triggered retry or escaped anomaly")
		}
	}
}
func TestBillNegativeRefundFactsAreStillChecked(t *testing.T) {
	for _, change := range []func(*ports.ChannelRefundResult){
		func(r *ports.ChannelRefundResult) { r.TradeNo = "wrong-trade" },
		func(r *ports.ChannelRefundResult) { r.TotalFen = 101 },
		func(r *ports.ChannelRefundResult) { r.AmountFen = 51 },
	} {
		s, store, ch := newFixture()
		store.orders["o"] = order("o", p.Credited)
		store.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", AmountFen: 50, Status: "pending"}
		ch.queryRefund = func(_ context.Context, no, order string) (ports.ChannelRefundResult, error) {
			r := ports.ChannelRefundResult{RefundNo: no, OrderNo: order, State: "not_found"}
			change(&r)
			return r, nil
		}
		summary, err := s.CompareBill(context.Background(), "demo", testNow, []ports.BillRow{{OrderNo: "o", TradeNo: "trade-o", Kind: "refund", RefundNo: "r", State: "pending", AmountFen: 50}})
		if err != nil || summary["anomalies"] != 1 || len(store.anomalies) != 1 || store.anomalies[0].Kind != "refund_mismatch" || store.refundFinishes != 0 || ch.refundCalls != 0 {
			t.Fatalf("negative refund bill bypassed checks %+v %v", summary, err)
		}
	}
}
