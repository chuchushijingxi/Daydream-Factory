package payment

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

func TestConfirmPaidRejectsUntrustedFacts(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Service, *fakeStore, *string, *ports.ChannelTrade)
		kind   string
	}{
		{"channel", func(s *Service, _ *fakeStore, ch *string, _ *ports.ChannelTrade) {
			s.Channels["alipay"] = &fakeChannel{id: "alipay"}
			*ch = "alipay"
		}, "identity_mismatch"},
		{"order number", func(_ *Service, _ *fakeStore, _ *string, tr *ports.ChannelTrade) { tr.OrderNo = "other" }, "identity_mismatch"},
		{"amount", func(_ *Service, _ *fakeStore, _ *string, tr *ports.ChannelTrade) { tr.AmountFen-- }, "amount_mismatch"},
		{"pending", func(_ *Service, _ *fakeStore, _ *string, tr *ports.ChannelTrade) { tr.State = "notpay" }, ""},
		{"missing paid time", func(_ *Service, _ *fakeStore, _ *string, tr *ports.ChannelTrade) { tr.PaidAt = nil }, ""},
		{"trade number changed", func(_ *Service, st *fakeStore, _ *string, _ *ports.ChannelTrade) {
			o := st.orders["o"]
			o.ChannelTradeNo = "other-transaction"
			o.Status = p.Credited
			st.orders["o"] = o
		}, "identity_mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, st, _ := newFixture()
			st.orders["o"] = order("o", p.Pending)
			tr := paid("o")
			ch := "demo"
			test.change(s, st, &ch, &tr)
			_, err := s.ConfirmPaid(context.Background(), ch, "o", tr, "notify")
			requireCode(t, err, "PAYMENT_CONFLICT")
			if st.creditCalls != 0 {
				t.Fatal("invalid fact reached credit transaction")
			}
			if test.kind != "" && (len(st.anomalies) != 1 || st.anomalies[0].Kind != test.kind) {
				t.Fatalf("missing anomaly: %v", st.anomalies)
			}
		})
	}
	s, st, _ := newFixture()
	_, err := s.ConfirmPaid(context.Background(), "demo", "missing", paid("missing"), "notify")
	requireCode(t, err, "PAYMENT_ORDER_NOT_FOUND")
	if len(st.anomalies) != 1 || st.anomalies[0].OrderNo != "" || st.anomalies[0].Detail["reportedOrderNo"] != "missing" {
		t.Fatal("missing order must not create invalid foreign key")
	}
}
func TestConfirmPaidConcurrentAndCommitUnknown(t *testing.T) {
	s, st, _ := newFixture()
	st.orders["o"] = order("o", p.Pending)
	var wg sync.WaitGroup
	failures := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			_, err := s.ConfirmPaid(context.Background(), "demo", "o", paid("o"), "notify")
			failures <- err
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if st.creditWrites != 1 {
		t.Fatalf("credit writes %d", st.creditWrites)
	}
	for _, confirmed := range []bool{false, true} {
		t.Run(fmt.Sprint(confirmed), func(t *testing.T) {
			s, st, _ := newFixture()
			st.orders["o"] = order("o", p.Pending)
			st.creditUnknown = true
			st.confirmed = confirmed
			out, err := s.ConfirmPaid(context.Background(), "demo", "o", paid("o"), "notify")
			if confirmed {
				if err != nil || out.Status != p.Credited {
					t.Fatalf("confirmed readback %v %v", out, err)
				}
			} else {
				requireCode(t, err, "PAYMENT_COMMIT_UNKNOWN")
			}
		})
	}
}
func TestClosedPaymentCreatesOnlyLateRefund(t *testing.T) {
	s, st, _ := newFixture()
	st.orders["o"] = order("o", p.Closed)
	for range 2 {
		out, err := s.ConfirmPaid(context.Background(), "demo", "o", paid("o"), "notify")
		if err != nil || out.Status != p.Closed {
			t.Fatalf("late pay %v %v", out, err)
		}
	}
	if st.creditWrites != 0 || len(st.refunds) != 1 || st.refunds["LRo"].AmountFen != 100 || st.refunds["LRo"].RechargeCredits != 0 {
		t.Fatal("late payment must not credit")
	}
}
func TestNotificationNonSuccessIsRecordedWithoutCredit(t *testing.T) {
	s, st, _ := newFixture()
	st.orders["o"] = order("o", p.Pending)
	for _, state := range []string{"notpay", "closed", "failed"} {
		tr := paid("o")
		tr.State = state
		if err := s.ConfirmNotification(context.Background(), "demo", ports.PaymentNotify{Kind: "pay", Trade: tr}); err != nil {
			t.Fatal(err)
		}
	}
	if len(st.events) != 3 || st.creditCalls != 0 || st.orders["o"].Status != p.Pending {
		t.Fatal("known nonpayment should only record")
	}
	tr := paid("o")
	tr.State = "unknown"
	err := s.ConfirmNotification(context.Background(), "demo", ports.PaymentNotify{Kind: "pay", Trade: tr})
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	if err = s.RejectNotification(context.Background(), "demo", "signature_invalid"); err != nil {
		t.Fatal(err)
	}
	if len(st.anomalies) != 1 || st.anomalies[0].Kind != "signature_invalid" {
		t.Fatal("no signature anomaly")
	}
}
func TestRefundQuoteZeroFenAndIdempotency(t *testing.T) {
	s, st, _ := newFixture()
	st.orders["o"] = order("o", p.Credited)
	st.quote.AmountFen = 0
	_, err := s.CreateRefund(context.Background(), operator, "o", CreateRefundInput{RefundID: "r", Credits: 1, Reason: "误充"})
	requireCode(t, err, "PAYMENT_INVALID_AMOUNT")
	if st.refundCreates != 0 {
		t.Fatal("zero fen refund wrote state")
	}
	st.quote.AmountFen = 50
	st.quote.RechargeCredits = 500
	first, err := s.CreateRefund(context.Background(), operator, "o", CreateRefundInput{RefundID: "r", Credits: 500, Reason: "误充"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateRefund(context.Background(), operator, "o", CreateRefundInput{RefundID: "r", Credits: 500, Reason: "误充"})
	if err != nil || first.RefundNo != second.RefundNo || st.refundCreates != 1 {
		t.Fatalf("refund not idempotent %v", err)
	}
	_, err = s.CreateRefund(context.Background(), operator, "o", CreateRefundInput{RefundID: "r", Credits: 499, Reason: "误充"})
	requireCode(t, err, "PAYMENT_CONFLICT")
}
func TestRefundUnknownQueriesAndOnlyDefiniteFailureRestores(t *testing.T) {
	s, st, ch := newFixture()
	st.orders["o"] = order("o", p.Credited)
	r := p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", Status: "pending", AmountFen: 50, RechargeCredits: 500, Attempts: 1}
	st.refunds["r"] = r
	st.claimRefunds = []p.Refund{r}
	ch.refund = func(context.Context, ports.ChannelRefund) (ports.ChannelRefundResult, error) {
		return ports.ChannelRefundResult{}, errors.New("network result unknown")
	}
	err := s.ScanRefunds(context.Background())
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	if st.refundFinishes != 0 {
		t.Fatal("unknown restored credits")
	}
	r.Attempts = 2
	st.claimRefunds = []p.Refund{r}
	if err = s.ScanRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ch.refundCalls != 1 || ch.refundQueries != 1 || st.refundFinishes != 0 {
		t.Fatal("unknown must query, not reissue or restore")
	}
	ch.queryRefund = func(_ context.Context, no, order string) (ports.ChannelRefundResult, error) {
		return ports.ChannelRefundResult{RefundNo: no, OrderNo: order, State: "not_found"}, nil
	}
	ch.refund = func(_ context.Context, in ports.ChannelRefund) (ports.ChannelRefundResult, error) {
		if in.RefundNo != "r" || in.AmountFen != 50 {
			t.Fatal("retry changed identity/amount")
		}
		return ports.ChannelRefundResult{RefundNo: in.RefundNo, OrderNo: in.OrderNo, AmountFen: 50, State: "pending"}, nil
	}
	if err = s.ScanRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ch.refundCalls != 2 || st.refundFinishes != 0 {
		t.Fatal("definite not_found must retry same refund")
	}
	ch.queryRefund = func(_ context.Context, no, order string) (ports.ChannelRefundResult, error) {
		return ports.ChannelRefundResult{RefundNo: no, OrderNo: order, State: "failed"}, nil
	}
	if err = s.ScanRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st.refundFinishes != 1 || st.refunds["r"].Status != "failed" {
		t.Fatal("definite failure not delivered to atomic restore")
	}
}
func TestRefundIdentityAndAmountValidation(t *testing.T) {
	tests := []ports.ChannelRefundResult{
		{RefundNo: "r", OrderNo: "other", State: "failed"},
		{RefundNo: "r", OrderNo: "o", State: "succeeded", AmountFen: 51, TotalFen: 100, TradeNo: "trade-o"},
		{RefundNo: "r", OrderNo: "o", State: "unknown"},
	}
	for _, result := range tests {
		s, st, _ := newFixture()
		st.orders["o"] = order("o", p.Credited)
		st.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", Status: "pending", AmountFen: 50}
		if _, err := s.ConfirmRefund(context.Background(), "demo", result, "notify"); err == nil {
			t.Fatal("invalid refund accepted")
		}
		if st.refundFinishes != 0 {
			t.Fatal("invalid refund changed terminal state")
		}
	}
}
func TestScannerStartsWholeBatchWithinLeaseAndRestoresPending(t *testing.T) {
	s, st, ch := newFixture()
	const count = 50
	started := make(chan time.Duration, count)
	release := make(chan struct{})
	for i := range count {
		o := order(fmt.Sprint(i), p.Pending)
		o.QueryAttempts = 1
		st.orders[o.OrderNo] = o
		st.claimOrders = append(st.claimOrders, o)
	}
	ch.query = func(ctx context.Context, no string) (ports.ChannelTrade, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return ports.ChannelTrade{}, errors.New("no timeout")
		}
		started <- time.Until(deadline)
		select {
		case <-release:
			return ports.ChannelTrade{OrderNo: no, State: "notpay"}, nil
		case <-ctx.Done():
			return ports.ChannelTrade{}, ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() { done <- s.ScanOrders(context.Background()) }()
	for range count {
		select {
		case remaining := <-started:
			if remaining > 45*time.Second || remaining < 40*time.Second {
				t.Errorf("operation deadline %v", remaining)
			}
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatal("batch tail did not start before lease")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if st.claimLease != time.Minute || len(st.orderSchedules) != count {
		t.Fatal("lease/recovery schedule missing")
	}
	for _, next := range st.orderSchedules {
		if !next.Equal(testNow.Add(30 * time.Second)) {
			t.Fatal("wrong query backoff")
		}
	}
}
func TestScannerQueriesBeforeExpiryClose(t *testing.T) {
	s, st, ch := newFixture()
	o := order("expired", p.Pending)
	o.ExpiresAt = testNow.Add(-time.Second)
	st.orders[o.OrderNo] = o
	st.claimOrders = []p.Order{o}
	if err := s.ScanOrders(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ch.sequence) != 2 || ch.sequence[0] != "query:expired" || ch.sequence[1] != "close:expired" || st.orders[o.OrderNo].Status != p.Closed {
		t.Fatalf("expiry sequence %v", ch.sequence)
	}
}
func TestOperatorAuthorizationAndAudit(t *testing.T) {
	s, st, _ := newFixture()
	st.orders["o"] = order("o", p.Credited)
	actions := []func() error{
		func() error { _, e := s.Packages(context.Background(), buyer); return e }, func() error { _, e := s.Settings(context.Background(), buyer); return e },
		func() error {
			_, e := s.Orders(context.Background(), buyer, ports.PaymentOrderFilter{Page: 1, PageSize: 10})
			return e
		}, func() error { _, e := s.OrderDetails(context.Background(), buyer, "o"); return e },
		func() error { _, e := s.QueryOrder(context.Background(), buyer, "o", "核查"); return e }, func() error { _, e := s.RefundQuote(context.Background(), buyer, "o", 0); return e },
		func() error { _, e := s.CreateRefund(context.Background(), buyer, "o", CreateRefundInput{}); return e }, func() error {
			_, e := s.Anomalies(context.Background(), buyer, ports.PaymentAnomalyFilter{Page: 1, PageSize: 10})
			return e
		},
		func() error { _, e := s.ResolveAnomaly(context.Background(), buyer, "a", "核查"); return e }, func() error { _, e := s.SavePackage(context.Background(), buyer, st.packages[0]); return e },
		func() error { _, e := s.SaveSettings(context.Background(), buyer, p.Settings{}); return e },
	}
	for _, action := range actions {
		requireCode(t, action(), "ROLE_FORBIDDEN")
	}
	if _, err := s.SavePackage(context.Background(), operator, st.packages[0]); err != nil {
		t.Fatal(err)
	}
	if st.lastOperator != operator.UserID {
		t.Fatal("package audit identity not actor")
	}
	if _, err := s.SaveSettings(context.Background(), operator, p.Settings{DailyLimitFen: 0}); err != nil {
		t.Fatal(err)
	}
	if st.lastOperator != operator.UserID {
		t.Fatal("settings audit missing")
	}
	if _, err := s.ResolveAnomaly(context.Background(), operator, "a", "已核查"); err != nil {
		t.Fatal(err)
	}
	if st.lastOperator != operator.UserID || st.lastReason != "已核查" {
		t.Fatal("resolve audit missing")
	}
}
func TestBillFourDiscrepanciesAndRepairRequireConfirmedPaid(t *testing.T) {
	s, st, ch := newFixture()
	s.Channels = map[string]ports.PaymentChannel{"wechatpay": ch}
	ch.id = "wechatpay"
	for _, no := range []string{"lost", "mismatch", "absent", "refund", "closed"} {
		status := p.Credited
		if no == "lost" {
			status = p.Pending
		}
		if no == "closed" {
			status = p.Closed
		}
		o := order(no, status)
		o.Channel = "wechatpay"
		if status == p.Credited {
			o.PaidAt = &testNow
			o.CreditedAt = &testNow
			o.ChannelTradeNo = "trade-" + no
			st.billOrders = append(st.billOrders, o)
		}
		st.orders[no] = o
	}
	st.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "refund", AmountFen: 100, Status: "pending"}
	ch.query = func(_ context.Context, no string) (ports.ChannelTrade, error) {
		if no == "closed" {
			return ports.ChannelTrade{OrderNo: no, State: "notpay"}, nil
		}
		return paid(no), nil
	}
	rows := []ports.BillRow{{OrderNo: "lost", Kind: "pay", TradeNo: "trade-lost", AmountFen: 100}, {OrderNo: "mismatch", Kind: "pay", TradeNo: "trade-mismatch", AmountFen: 99}, {OrderNo: "unknown", Kind: "pay", TradeNo: "unknown", AmountFen: 100}, {OrderNo: "refund", Kind: "refund", TradeNo: "trade-refund", RefundNo: "r", State: "succeeded", AmountFen: 100}, {OrderNo: "closed", Kind: "pay", TradeNo: "trade-closed", AmountFen: 100}}
	summary, err := s.CompareBill(context.Background(), "wechatpay", testNow, rows)
	if err != nil {
		t.Fatal(err)
	}
	if summary["repaired"] != 1 || st.orders["lost"].Status != p.Credited || st.creditWrites != 1 || st.orders["closed"].Status != p.Closed {
		t.Fatalf("invalid repair %+v", summary)
	}
	kinds := []string{}
	for _, a := range st.anomalies {
		kinds = append(kinds, a.Kind)
	}
	sort.Strings(kinds)
	want := "[bill_amount_mismatch bill_amount_mismatch bill_missing_channel bill_missing_local refund_mismatch]"
	if fmt.Sprint(kinds) != want {
		t.Fatalf("kinds %v", kinds)
	}
	if st.refundFinishes != 0 {
		t.Fatal("bill comparison changed refund state")
	}
}
func TestBillRefundRepresentationRequiresSamePaymentDay(t *testing.T) {
	for _, sameDay := range []bool{false, true} {
		t.Run(fmt.Sprint(sameDay), func(t *testing.T) {
			s, st, ch := newFixture()
			o := order("o", p.Refunded)
			o.ChannelTradeNo = "trade-o"
			paidAt := testNow
			if !sameDay {
				paidAt = paidAt.Add(-24 * time.Hour)
			}
			o.PaidAt = &paidAt
			o.CreditedAt = &testNow
			st.orders["o"] = o
			st.billOrders = []p.Order{o}
			st.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", AmountFen: 100, Status: "succeeded", FinishedAt: &testNow}
			summary, err := s.CompareBill(context.Background(), "demo", testNow, []ports.BillRow{{OrderNo: "o", TradeNo: "trade-o", Kind: "refund", RefundNo: "r", State: "succeeded", AmountFen: 100}})
			if err != nil {
				t.Fatal(err)
			}
			if summary["anomalies"] != 0 || summary["repaired"] != 0 || st.creditCalls != 0 || st.refundFinishes != 0 || ch.queries != 0 {
				t.Fatalf("refund representation changed facts %+v", summary)
			}
		})
	}
}

func TestBillHistoricalClaimSurvivesMidnightAndRestart(t *testing.T) {
	for _, hour := range []int{1, 4} {
		t.Run(fmt.Sprint(hour), func(t *testing.T) {
			s, st, ch := newFixture()
			ch.id = "wechatpay"
			s.Channels = map[string]ports.PaymentChannel{"wechatpay": ch}
			now := time.Date(2026, 10, 23, hour, 0, 0, 0, time.UTC)
			s.Now = func() time.Time { return now }
			historic := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC) // 北京时间20日，模拟停机前的失败账单。
			st.billClaimFn = func(candidate *time.Time, at time.Time) (ports.PaymentBillClaim, bool, error) {
				if hour == 1 && candidate != nil {
					t.Fatal("before 10:00 must not enqueue new bill")
				}
				if hour == 4 && (candidate == nil || candidate.In(time.FixedZone("BJ", 8*60*60)).Day() != 22) {
					t.Fatal("wrong yesterday candidate")
				}
				return ports.PaymentBillClaim{Date: historic, Attempts: 7}, true, nil
			}
			ch.bill = func(_ context.Context, date time.Time) ([]ports.BillRow, error) {
				if !date.Equal(historic) {
					t.Fatal("used new candidate instead of historic claim")
				}
				return nil, nil
			}
			if err := s.ReconcileBills(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !st.billFinished || st.lastBillClaim.Attempts != 7 || !st.lastBillClaim.Date.Equal(historic) {
				t.Fatal("lost date/attempt fence")
			}
		})
	}
}
func TestRefundBillPendingDoesNotMeanSucceeded(t *testing.T) {
	s, st, ch := newFixture()
	o := order("o", p.Credited)
	o.PaidAt = &testNow
	o.ChannelTradeNo = "trade-o"
	st.orders["o"] = o
	st.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", AmountFen: 100, Status: "pending"}
	st.billOrders = []p.Order{o}
	summary, err := s.CompareBill(context.Background(), "demo", testNow, []ports.BillRow{{OrderNo: "o", TradeNo: "trade-o", Kind: "refund", RefundNo: "r", State: "pending", AmountFen: 100}})
	if err != nil || summary["anomalies"] != 0 || st.refundFinishes != 0 || st.refunds["r"].Status != "pending" || ch.refundQueries != 1 {
		t.Fatalf("pending bill incorrectly finalized %+v %v", summary, err)
	}
}
func TestRefundRequestIdentityNeverMatchesAnotherChannelNumber(t *testing.T) {
	s, st, _ := newFixture()
	st.orders["o"] = order("o", p.Credited)
	st.refunds["collision"] = p.Refund{ID: "another-request", RefundNo: "collision", OrderNo: "other", Status: "pending"}
	out, err := s.CreateRefund(context.Background(), operator, "o", CreateRefundInput{RefundID: "collision", Credits: 500, Reason: "误充"})
	if err != nil || out.OrderNo != "o" || st.refundCreates != 1 {
		t.Fatalf("looked up channel number as request id %+v %v", out, err)
	}
}

func TestRefundSuccessRequiresChannelBillDateEvidence(t *testing.T) {
	s, st, ch := newFixture()
	st.orders["o"] = order("o", p.Credited)
	st.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", Status: "pending", AmountFen: 100}
	result := ports.ChannelRefundResult{RefundNo: "r", OrderNo: "o", State: "succeeded", AmountFen: 100, TotalFen: 100, TradeNo: "trade-o"}
	ch.queryRefund = func(_ context.Context, no, order string) (ports.ChannelRefundResult, error) {
		return ports.ChannelRefundResult{RefundNo: no, OrderNo: order, AmountFen: 100, State: "pending"}, nil
	}
	_, err := s.ConfirmRefund(context.Background(), "demo", result, "notify")
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	if st.refundFinishes != 0 {
		t.Fatal("missing date finalized refund")
	}
	accepted := testNow.Add(-24 * time.Hour)
	ch.queryRefund = func(_ context.Context, no, order string) (ports.ChannelRefundResult, error) {
		return ports.ChannelRefundResult{RefundNo: no, OrderNo: order, AmountFen: 100, TotalFen: 100, TradeNo: "trade-o", State: "succeeded", BillAt: &accepted}, nil
	}
	out, err := s.ConfirmRefund(context.Background(), "demo", result, "notify")
	if err != nil || out.Status != "succeeded" || out.BillAt == nil || !out.BillAt.Equal(accepted) {
		t.Fatalf("date not recovered %+v %v", out, err)
	}
}
func TestRefundBillUsesAcceptanceDateInsteadOfLocalFinishDate(t *testing.T) {
	s, st, _ := newFixture()
	o := order("o", p.Refunded)
	paidAt := testNow.Add(-48 * time.Hour)
	o.PaidAt = &paidAt
	o.CreditedAt = &paidAt
	o.ChannelTradeNo = "trade-o"
	st.orders["o"] = o
	st.billOrders = []p.Order{o}
	accepted := testNow.Add(-24 * time.Hour)
	st.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", Status: "succeeded", AmountFen: 100, BillAt: &accepted, FinishedAt: &testNow}
	summary, err := s.CompareBill(context.Background(), "demo", testNow, nil)
	if err != nil || summary["anomalies"] != 0 {
		t.Fatalf("local finish date produced false mismatch %+v %v", summary, err)
	}
	summary, err = s.CompareBill(context.Background(), "demo", accepted, nil)
	if err != nil || summary["anomalies"] != 1 || st.anomalies[len(st.anomalies)-1].Kind != "refund_mismatch" {
		t.Fatalf("missing acceptance-day refund not detected %+v %v", summary, err)
	}
}

func TestSimulationOwnershipFailureAndDevelopmentOnlyLatePayment(t *testing.T) {
	s, st, _ := newFixture()
	st.orders["o"] = order("o", p.Pending)
	out, err := s.Simulate(context.Background(), buyer, "o", "failed")
	if err != nil || out.Status != p.Pending || st.creditCalls != 0 {
		t.Fatal("failed simulation changed pending order")
	}
	_, err = s.Simulate(context.Background(), operator, "o", "paid")
	requireCode(t, err, "PAYMENT_TARGET_FORBIDDEN")
	o := st.orders["o"]
	o.Status = p.Closed
	st.orders["o"] = o
	s.Environment = "staging"
	_, err = s.Simulate(context.Background(), buyer, "o", "paid")
	requireCode(t, err, "PAYMENT_CONFLICT")
	s.Environment = "development"
	out, err = s.Simulate(context.Background(), buyer, "o", "paid")
	if err != nil || out.Status != p.Closed || len(st.refunds) != 1 || st.creditWrites != 0 {
		t.Fatalf("late payment simulation %+v %v", out, err)
	}
}

func TestForeignApplicationBillNeverQueriesOrCreditsMatchingLocalOrder(t *testing.T) {
	s, store, ch := newFixture()
	store.orders["ours"] = order("ours", p.Pending)
	store.orders["collision"] = order("collision", p.Pending)
	ch.query = func(_ context.Context, no string) (ports.ChannelTrade, error) {
		if no != "ours" {
			t.Errorf("foreign application triggered query %s", no)
		}
		return paid(no), nil
	}
	rows := []ports.BillRow{{OrderNo: "ours", TradeNo: "trade-ours", Kind: "pay", AmountFen: 100}, {OrderNo: "collision", TradeNo: "other-app-trade", Kind: "pay", AmountFen: 100, ForeignApplication: true}, {OrderNo: "foreign-unknown", TradeNo: "another", Kind: "pay", AmountFen: 100, ForeignApplication: true}}
	summary, err := s.CompareBill(context.Background(), "demo", testNow, rows)
	if err != nil {
		t.Fatal(err)
	}
	if summary["repaired"] != 1 || ch.queries != 1 || store.creditWrites != 1 || store.orders["collision"].Status != p.Pending {
		t.Fatalf("foreign row changed our payment %+v", summary)
	}
	if len(store.anomalies) != 2 || store.anomalies[0].Kind != "identity_mismatch" || store.anomalies[1].Kind != "bill_missing_local" {
		t.Fatalf("foreign differences %+v", store.anomalies)
	}
}

func TestConfigurationReasonsRemovedWithoutRelaxingFinancialActions(t *testing.T) {
	s, st, _ := newFixture()
	st.lastReason = "先前操作证据"
	pkg, err := s.SavePackage(context.Background(), operator, st.packages[0])
	if err != nil || pkg.Revision != st.packages[0].Revision+1 || st.lastOperator != operator.UserID {
		t.Fatal(pkg, err)
	}
	settings, err := s.SaveSettings(context.Background(), operator, p.Settings{DailyLimitFen: 0})
	if err != nil || settings.Revision != 1 || st.lastOperator != operator.UserID || st.lastReason != "先前操作证据" {
		t.Fatal(settings, err)
	}
	_, err = s.QueryOrder(context.Background(), operator, "o", " ")
	if !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("query without reason accepted", err)
	}
	_, err = s.CreateRefund(context.Background(), operator, "o", CreateRefundInput{RefundID: "r", Credits: 1})
	if !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("refund without reason accepted", err)
	}
	_, err = s.ResolveAnomaly(context.Background(), operator, "a", " ")
	if !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("anomaly without explanation accepted", err)
	}
}
