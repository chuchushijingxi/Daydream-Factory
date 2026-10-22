package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/accounts"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

var testNow = time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC)
var buyer = ports.Actor{UserID: "buyer", Role: ports.RoleUser}
var operator = ports.Actor{UserID: "operator", Role: ports.RoleOperator}

type fakeUsers struct{ disabled bool }

func (u *fakeUsers) CurrentUser(_ context.Context, id string) (accounts.User, error) {
	status := "enabled"
	if u.disabled {
		status = "disabled"
	}
	return accounts.User{ID: id, Status: status}, nil
}

type fakeRate struct {
	allowed bool
	calls   int
	key     string
	limit   int
	window  time.Duration
}

func (r *fakeRate) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	r.calls++
	r.key = key
	r.limit = limit
	r.window = window
	return r.allowed, nil
}
func (r *fakeRate) ResetLimit(context.Context, string) error { return nil }

type fakeChannel struct {
	id            string
	create        func(context.Context, ports.ChannelOrder) (string, error)
	query         func(context.Context, string) (ports.ChannelTrade, error)
	close         func(context.Context, string) error
	refund        func(context.Context, ports.ChannelRefund) (ports.ChannelRefundResult, error)
	queryRefund   func(context.Context, string, string) (ports.ChannelRefundResult, error)
	bill          func(context.Context, time.Time) ([]ports.BillRow, error)
	mu            sync.Mutex
	creates       int
	queries       int
	closes        int
	refundCalls   int
	refundQueries int
	sequence      []string
}

func (c *fakeChannel) ID() string { return c.id }
func (c *fakeChannel) Create(ctx context.Context, in ports.ChannelOrder) (string, error) {
	c.mu.Lock()
	c.creates++
	c.sequence = append(c.sequence, "create:"+in.OrderNo)
	c.mu.Unlock()
	if c.create != nil {
		return c.create(ctx, in)
	}
	return "demo://" + in.OrderNo, nil
}
func (c *fakeChannel) Query(ctx context.Context, no string) (ports.ChannelTrade, error) {
	c.mu.Lock()
	c.queries++
	c.sequence = append(c.sequence, "query:"+no)
	c.mu.Unlock()
	if c.query != nil {
		return c.query(ctx, no)
	}
	return ports.ChannelTrade{OrderNo: no, State: "notpay"}, nil
}
func (c *fakeChannel) Close(ctx context.Context, no string) error {
	c.mu.Lock()
	c.closes++
	c.sequence = append(c.sequence, "close:"+no)
	c.mu.Unlock()
	if c.close != nil {
		return c.close(ctx, no)
	}
	return nil
}
func (c *fakeChannel) Refund(ctx context.Context, in ports.ChannelRefund) (ports.ChannelRefundResult, error) {
	c.mu.Lock()
	c.refundCalls++
	c.mu.Unlock()
	if c.refund != nil {
		return c.refund(ctx, in)
	}
	return ports.ChannelRefundResult{RefundNo: in.RefundNo, OrderNo: in.OrderNo, AmountFen: in.AmountFen, TotalFen: in.TotalFen, TradeNo: in.TradeNo, State: "succeeded", BillAt: &testNow}, nil
}
func (c *fakeChannel) QueryRefund(ctx context.Context, no, order string) (ports.ChannelRefundResult, error) {
	c.mu.Lock()
	c.refundQueries++
	c.mu.Unlock()
	if c.queryRefund != nil {
		return c.queryRefund(ctx, no, order)
	}
	return ports.ChannelRefundResult{RefundNo: no, OrderNo: order, State: "pending"}, nil
}
func (c *fakeChannel) Simulate(_ context.Context, no, result string) (ports.ChannelTrade, error) {
	if result == "paid" {
		return paid(no), nil
	}
	return ports.ChannelTrade{OrderNo: no, State: "failed"}, nil
}
func (c *fakeChannel) ParseNotify(context.Context, ports.NotifyInput) (ports.PaymentNotify, ports.NotifyAck, error) {
	panic("not used")
}
func (c *fakeChannel) Bill(ctx context.Context, date time.Time) ([]ports.BillRow, error) {
	if c.bill != nil {
		return c.bill(ctx, date)
	}
	return nil, nil
}

type fakeStore struct {
	ports.PaymentStore
	mu                  sync.Mutex
	packages            []p.Package
	orders              map[string]p.Order
	refunds             map[string]p.Refund
	events              []p.Event
	anomalies           []p.Anomaly
	settings            p.Settings
	agreements          map[string]bool
	creditCalls         int
	creditWrites        int
	refundCreates       int
	refundFinishes      int
	refundFinishErr     error
	lastRefundResult    ports.ChannelRefundResult
	refundFinishNos     []string
	refundRestoreWrites int
	auditContextCheck   func(context.Context)

	creditUnknown  bool
	confirmed      bool
	confirmedErr   error
	claimOrders    []p.Order
	claimRefunds   []p.Refund
	orderSchedules map[string]time.Time
	lastFilter     ports.PaymentOrderFilter
	lastOperator   string
	lastReason     string
	quote          p.RefundQuote
	quoteErr       error
	billOrders     []p.Order
	billClaims     []time.Time
	billFinished   bool
	billClaimFn    func(*time.Time, time.Time) (ports.PaymentBillClaim, bool, error)
	lastBillClaim  ports.PaymentBillClaim
	claimLease     time.Duration
}

func (s *fakeStore) PaymentPackages(context.Context) ([]p.Package, error) { return s.packages, nil }
func (s *fakeStore) SavePaymentPackage(_ context.Context, in p.Package, operator string) (p.Package, error) {
	s.lastOperator = operator
	in.Revision++
	return in, nil
}
func (s *fakeStore) PaymentSettings(context.Context) (p.Settings, error) { return s.settings, nil }
func (s *fakeStore) SavePaymentSettings(_ context.Context, in p.Settings, operator string) (p.Settings, error) {
	s.lastOperator = operator
	in.Revision++
	s.settings = in
	return in, nil
}
func (s *fakeStore) PaymentAgreement(_ context.Context, user, version string) (bool, error) {
	return s.agreements[user+version], nil
}
func (s *fakeStore) PaymentOrderByRequest(_ context.Context, buyer, request string) (p.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, order := range s.orders {
		if order.BuyerUserID == buyer && order.ClientRequestID == request {
			return order, nil
		}
	}
	return p.Order{}, fault.ErrNotFound
}
func (s *fakeStore) PendingPaymentOrder(_ context.Context, buyer string) (p.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, order := range s.orders {
		if order.BuyerUserID == buyer && order.Status == p.Pending {
			return order, nil
		}
	}
	return p.Order{}, fault.ErrNotFound
}
func (s *fakeStore) CreatePaymentOrder(_ context.Context, in p.Order) (p.Order, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, order := range s.orders {
		if order.BuyerUserID == in.BuyerUserID && order.ClientRequestID == in.ClientRequestID {
			return order, false, nil
		}
		if order.BuyerUserID == in.BuyerUserID && order.Status == p.Pending {
			return p.Order{}, false, fault.New("PAYMENT_CONFLICT", "pending")
		}
	}
	s.orders[in.OrderNo] = in
	s.agreements[in.BuyerUserID+in.AgreementVersion] = true
	return in, true, nil
}
func (s *fakeStore) PaymentOrder(_ context.Context, no string) (p.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[no]
	if !ok {
		return order, fault.ErrNotFound
	}
	return order, nil
}
func (s *fakeStore) PaymentOrders(_ context.Context, filter ports.PaymentOrderFilter) (ports.Page[p.Order], error) {
	s.lastFilter = filter
	return ports.Page[p.Order]{Items: []p.Order{}, Page: filter.Page, PageSize: filter.PageSize}, nil
}
func (s *fakeStore) PaymentOrderDetails(_ context.Context, no string) (ports.PaymentOrderDetails, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ports.PaymentOrderDetails{Order: s.orders[no], Events: s.events}
	for _, r := range s.refunds {
		if r.OrderNo == no {
			out.Refunds = append(out.Refunds, r)
		}
	}
	return out, nil
}
func (s *fakeStore) SetPaymentOrderCode(_ context.Context, no, url, source string) (p.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.orders[no]
	order.CodeURL = url
	s.orders[no] = order
	return order, nil
}
func (s *fakeStore) RecordPaymentEvent(ctx context.Context, event p.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.auditContextCheck != nil {
		s.auditContextCheck(ctx)
	}
	for _, prior := range s.events {
		if prior.ID == event.ID && event.ID != "" {
			return nil
		}
	}
	s.events = append(s.events, event)
	return nil
}
func (s *fakeStore) CreditPaymentOrder(_ context.Context, in ports.PaymentConfirmation) (p.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creditCalls++
	order := s.orders[in.OrderNo]
	if order.Status == p.Pending {
		s.creditWrites++
		order.Status = p.Credited
		order.ChannelTradeNo = in.Trade.TradeNo
		at := testNow
		order.CreditedAt = &at
		order.PaidAt = in.Trade.PaidAt
		s.orders[in.OrderNo] = order
	} else if order.Status == p.Closed {
		no := "LR" + order.OrderNo
		if _, exists := s.refunds[no]; !exists {
			s.refunds[no] = p.Refund{ID: no, RefundNo: no, OrderNo: order.OrderNo, Kind: "late_payment", AmountFen: order.AmountFen, Status: "pending"}
		}
	}
	if s.creditUnknown {
		return order, fault.New("PAYMENT_COMMIT_UNKNOWN", "unknown")
	}
	return order, nil
}
func (s *fakeStore) ConfirmedPaymentCredit(_ context.Context, no string) (p.Order, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.orders[no], s.confirmed, s.confirmedErr
}
func (s *fakeStore) ClosePaymentOrder(_ context.Context, no, reason, source string) (p.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.orders[no]
	if order.Status == p.Pending {
		order.Status = p.Closed
		order.CloseReason = reason
		s.orders[no] = order
	}
	return order, nil
}
func (s *fakeStore) ClaimPaymentOrders(_ context.Context, _ time.Time, _ int, lease time.Duration) ([]p.Order, error) {
	s.claimLease = lease
	return s.claimOrders, nil
}
func (s *fakeStore) SchedulePaymentOrder(_ context.Context, no string, expectedAttempts int, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orderSchedules[no] = next
	return nil
}
func (s *fakeStore) PaymentRefundQuote(context.Context, string, int64) (p.RefundQuote, error) {
	return s.quote, s.quoteErr
}
func (s *fakeStore) CreatePaymentRefund(_ context.Context, in p.RefundRequest) (p.Refund, error) {
	s.refundCreates++
	refund := p.Refund{ID: in.RefundID, RefundNo: in.RefundID, OrderNo: in.OrderNo, RechargeCredits: in.Credits, GiftCredits: s.quote.GiftCredits, AmountFen: s.quote.AmountFen, OperatorID: in.OperatorID, Reason: in.Reason, Status: "pending"}
	s.refunds[refund.RefundNo] = refund
	return refund, nil
}
func (s *fakeStore) PaymentRefundByRequest(_ context.Context, id string) (p.Refund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, refund := range s.refunds {
		if refund.ID == id {
			return refund, nil
		}
	}
	return p.Refund{}, fault.ErrNotFound
}
func (s *fakeStore) PaymentRefund(_ context.Context, no string) (p.Refund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, refund := range s.refunds {
		if refund.RefundNo == no {
			return refund, nil
		}
	}
	return p.Refund{}, fault.ErrNotFound
}
func (s *fakeStore) FinishPaymentRefund(_ context.Context, no string, result ports.ChannelRefundResult, source string) (p.Refund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refundFinishes++
	s.lastRefundResult = result
	refund := s.refunds[no]
	s.refundFinishNos = append(s.refundFinishNos, no)
	if s.refundFinishErr != nil {
		return refund, s.refundFinishErr
	}
	if result.State == "failed" && refund.Status == "pending" {
		s.refundRestoreWrites++
	}
	refund.Status = result.State
	if refund.BillAt == nil {
		refund.BillAt = result.BillAt
	}
	s.refunds[no] = refund
	return refund, nil
}
func (s *fakeStore) ClaimPaymentRefunds(_ context.Context, _ time.Time, _ int, lease time.Duration) ([]p.Refund, error) {
	s.claimLease = lease
	return s.claimRefunds, nil
}
func (s *fakeStore) RecordPaymentAnomaly(ctx context.Context, in p.Anomaly) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.auditContextCheck != nil {
		s.auditContextCheck(ctx)
	}
	for _, prior := range s.anomalies {
		if prior.ID == in.ID && in.ID != "" {
			return nil
		}
	}
	s.anomalies = append(s.anomalies, in)
	return nil
}
func (s *fakeStore) PaymentAnomalies(context.Context, ports.PaymentAnomalyFilter) (ports.Page[p.Anomaly], error) {
	return ports.Page[p.Anomaly]{Items: s.anomalies}, nil
}
func (s *fakeStore) ResolvePaymentAnomaly(_ context.Context, id, operator, note string) (p.Anomaly, error) {
	s.lastOperator = operator
	s.lastReason = note
	return p.Anomaly{ID: id, ResolvedBy: operator, Note: note, Status: "resolved"}, nil
}
func (s *fakeStore) ClaimPaymentBill(_ context.Context, _ string, candidate *time.Time, now time.Time) (ports.PaymentBillClaim, bool, error) {
	if s.billClaimFn != nil {
		return s.billClaimFn(candidate, now)
	}
	date := now.Add(-48 * time.Hour)
	if candidate != nil {
		date = *candidate
	}
	s.billClaims = append(s.billClaims, date)
	return ports.PaymentBillClaim{Date: date, Attempts: 1}, true, nil
}
func (s *fakeStore) FinishPaymentBill(_ context.Context, _ string, claim ports.PaymentBillClaim, ok bool, _ map[string]int64) error {
	s.lastBillClaim = claim
	s.billFinished = ok
	return nil
}
func (s *fakeStore) PaymentOrdersForBill(context.Context, string, time.Time, time.Time) ([]p.Order, error) {
	return s.billOrders, nil
}
func newFixture() (*Service, *fakeStore, *fakeChannel) {
	store := &fakeStore{packages: []p.Package{{Name: "测试档位", ID: "standard", OriginalAmountFen: 100, AmountFen: 100, RechargeCredits: 1000, GiftCredits: 100, Enabled: true}}, orders: map[string]p.Order{}, refunds: map[string]p.Refund{}, agreements: map[string]bool{}, orderSchedules: map[string]time.Time{}, settings: p.Settings{DailyLimitFen: p.DefaultDailyLimitFen}, quote: p.RefundQuote{MaxCredits: 1000, RechargeCredits: 1000, GiftCredits: 100, AmountFen: 100}}
	channel := &fakeChannel{id: "demo"}
	service := &Service{Store: store, Channels: map[string]ports.PaymentChannel{"demo": channel}, Targets: PersonalTargets{}, Users: &fakeUsers{}, RateLimiter: &fakeRate{allowed: true}, Now: func() time.Time { return testNow }, Environment: "development"}
	return service, store, channel
}
func input() CreateOrderInput {
	return CreateOrderInput{ClientRequestID: "request-1", Target: p.Target{Kind: "personal", ID: buyer.UserID}, PackageID: "standard", Channel: "demo", AgreementVersion: p.AgreementVersion}
}
func order(no string, status p.Status) p.Order {
	tradeNo := ""
	if status == p.Credited || status == p.Refunded {
		tradeNo = "trade-" + no
	}
	return p.Order{OrderNo: no, ChannelTradeNo: tradeNo, BuyerUserID: buyer.UserID, ClientRequestID: no, Target: p.Target{Kind: "personal", ID: buyer.UserID}, CreditAccountID: buyer.UserID, PackageSnapshot: p.PackageSnapshot{PackageID: "standard", OriginalAmountFen: 100, AmountFen: 100, RechargeCredits: 1000, GiftCredits: 100}, Channel: "demo", Status: status, AgreementVersion: p.AgreementVersion, CreatedAt: testNow.Add(-time.Hour), ExpiresAt: testNow.Add(time.Minute)}
}
func paid(no string) ports.ChannelTrade {
	return ports.ChannelTrade{OrderNo: no, State: "paid", AmountFen: 100, TradeNo: "trade-" + no, PaidAt: &testNow}
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	if !codeIs(err, code) {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestCreateIdempotentAndFrozenSnapshot(t *testing.T) {
	s, store, ch := newFixture()
	first, err := s.CreateOrder(context.Background(), buyer, input())
	if err != nil {
		t.Fatal(err)
	}
	if !p.ValidOrderNo(first.OrderNo) || first.ExpiresAt.Sub(first.CreatedAt) != 15*time.Minute || first.CreditAccountID != buyer.UserID {
		t.Fatalf("invalid order %+v", first)
	}
	store.packages[0].Enabled = false
	store.packages[0].AmountFen = 999
	s.RateLimiter.(*fakeRate).allowed = false
	second, err := s.CreateOrder(context.Background(), buyer, input())
	if err != nil {
		t.Fatal(err)
	}
	if second.OrderNo != first.OrderNo || second.AmountFen != 100 || ch.creates != 1 {
		t.Fatalf("not idempotent: %+v", second)
	}
	changed := input()
	changed.Channel = "wechatpay"
	_, err = s.CreateOrder(context.Background(), buyer, changed)
	requireCode(t, err, "PAYMENT_CONFLICT")
	if len(store.orders) != 1 {
		t.Fatal("retry created another order")
	}
}
func TestCreateValidationAndTargetBoundary(t *testing.T) {
	tests := []struct {
		name, code string
		change     func(*Service, *fakeStore, *CreateOrderInput)
	}{
		{"disabled", "ACCOUNT_DISABLED", func(s *Service, _ *fakeStore, _ *CreateOrderInput) { s.Users = &fakeUsers{disabled: true} }},
		{"other personal", "PAYMENT_TARGET_FORBIDDEN", func(_ *Service, _ *fakeStore, in *CreateOrderInput) { in.Target.ID = "other" }},
		{"team dependency", "PAYMENT_TARGET_FORBIDDEN", func(_ *Service, _ *fakeStore, in *CreateOrderInput) { in.Target.Kind = "team" }},
		{"agreement", "PAYMENT_AGREEMENT_REQUIRED", func(_ *Service, _ *fakeStore, in *CreateOrderInput) { in.AgreementVersion = "old" }},
		{"channel", "PAYMENT_CHANNEL_UNAVAILABLE", func(_ *Service, _ *fakeStore, in *CreateOrderInput) { in.Channel = "alipay" }},
		{"package", "PAYMENT_PACKAGE_UNAVAILABLE", func(_ *Service, st *fakeStore, _ *CreateOrderInput) { st.packages[0].Enabled = false }},
		{"test package", "PAYMENT_PACKAGE_UNAVAILABLE", func(_ *Service, st *fakeStore, _ *CreateOrderInput) { st.packages[0].TestOnly = true }},
		{"rate", "RATE_LIMITED", func(s *Service, _ *fakeStore, _ *CreateOrderInput) { s.RateLimiter.(*fakeRate).allowed = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, st, ch := newFixture()
			in := input()
			test.change(s, st, &in)
			_, err := s.CreateOrder(context.Background(), buyer, in)
			requireCode(t, err, test.code)
			if len(st.orders) != 0 || ch.creates != 0 {
				t.Fatal("invalid request persisted")
			}
		})
	}
}
func TestCreateClosesPreviousAfterQueryAndCapturesPayment(t *testing.T) {
	for _, wasPaid := range []bool{false, true} {
		t.Run(fmt.Sprint(wasPaid), func(t *testing.T) {
			s, st, ch := newFixture()
			old := order("previous", p.Pending)
			st.orders[old.OrderNo] = old
			if wasPaid {
				ch.query = func(_ context.Context, no string) (ports.ChannelTrade, error) { return paid(no), nil }
			}
			_, err := s.CreateOrder(context.Background(), buyer, input())
			if err != nil {
				t.Fatal(err)
			}
			if len(ch.sequence) < 2 || ch.sequence[0] != "query:previous" {
				t.Fatalf("missing query first %v", ch.sequence)
			}
			if wasPaid {
				if ch.closes != 0 || st.orders[old.OrderNo].Status != p.Credited {
					t.Fatal("paid order was closed")
				}
			} else {
				if ch.sequence[1] != "close:previous" || st.orders[old.OrderNo].Status != p.Closed {
					t.Fatal("old order not closed first")
				}
			}
		})
	}
}
func TestChannelFailurePreservesPendingAndAudit(t *testing.T) {
	s, st, ch := newFixture()
	ch.create = func(context.Context, ports.ChannelOrder) (string, error) {
		return "", errors.New("private payload must not persist")
	}
	created, err := s.CreateOrder(context.Background(), buyer, input())
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	if st.orders[created.OrderNo].Status != p.Pending || !st.orderSchedules[created.OrderNo].Equal(testNow) || len(st.events) != 1 || st.events[0].Outcome != "channel_failed" {
		t.Fatal("missing recovery state")
	}
	ch.query = func(context.Context, string) (ports.ChannelTrade, error) {
		return ports.ChannelTrade{}, errors.New("query timeout")
	}
	_, err = s.CloseOrder(context.Background(), buyer, created.OrderNo)
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	if ch.closes != 0 || st.orders[created.OrderNo].Status != p.Pending || len(st.events) != 2 {
		t.Fatal("query failure closed order or missing audit")
	}
	ch.query = nil
	ch.close = func(context.Context, string) error { return errors.New("close timeout") }
	_, err = s.CloseOrder(context.Background(), buyer, created.OrderNo)
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	if st.orders[created.OrderNo].Status != p.Pending || st.events[len(st.events)-1].Source != "close" {
		t.Fatal("close failure lost state")
	}
}
func TestReadsAreLocalAndScoped(t *testing.T) {
	s, st, ch := newFixture()
	st.orders["o"] = order("o", p.Pending)
	if _, err := s.GetOrder(context.Background(), buyer, "o"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListOrders(context.Background(), buyer, 1, 10); err != nil {
		t.Fatal(err)
	}
	if st.lastFilter.BuyerUserID != buyer.UserID || ch.queries != 0 || len(st.events) != 0 {
		t.Fatal("read was not local/scoped")
	}
	_, err := s.GetOrder(context.Background(), operator, "o")
	requireCode(t, err, "PAYMENT_TARGET_FORBIDDEN")
}
func TestOptionsEnvironmentAndNoChannel(t *testing.T) {
	for _, env := range []string{"", "production", "staging", "development"} {
		for _, actor := range []ports.Actor{buyer, operator} {
			s, st, _ := newFixture()
			s.Environment = env
			st.packages = append(st.packages, p.Package{Name: "测试档位", ID: "test", Enabled: true, TestOnly: true, OriginalAmountFen: 1, AmountFen: 1, RechargeCredits: 1})
			out, err := s.Options(context.Background(), actor)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if (env == "staging" || env == "development") && actor.Role == ports.RoleOperator {
				want = 2
			}
			if len(out.Packages) != want {
				t.Fatalf("env %q role %s packages %d", env, actor.Role, len(out.Packages))
			}
		}
	}
	s, _, _ := newFixture()
	s.Channels = nil
	out, err := s.Options(context.Background(), buyer)
	if err != nil || len(out.Channels) != 0 || out.Channels == nil {
		t.Fatalf("empty channels %+v %v", out, err)
	}
}

func TestOptionsShowPackageNameButSnapshotOmitsItAndDiscountedLegacyOrder(t *testing.T) {
	s, st, ch := newFixture()
	st.packages[0].Name = "内部运营方案名"
	st.packages[0].OriginalAmountFen = 125
	out, err := s.Options(context.Background(), buyer)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out.Packages)
	if err != nil || out.Packages[0].Name != "内部运营方案名" || !bytes.Contains(raw, []byte(`"name":"内部运营方案名"`)) {
		t.Fatal(string(raw), err)
	}
	if out.Packages[0].OriginalAmountFen != 125 || out.Packages[0].AmountFen != 100 {
		t.Fatal(out.Packages)
	}
	saved, err := s.SavePackage(context.Background(), operator, func() p.Package { v := st.packages[0]; v.Name = "  运营名称  "; return v }())
	if err != nil || saved.Name != "运营名称" {
		t.Fatal(saved, err)
	}
	st.packages[0].Name = "" // 旧档位尚未命名不阻止用户下单。
	ch.create = func(_ context.Context, in ports.ChannelOrder) (string, error) {
		if in.AmountFen != 100 {
			t.Fatalf("channel charged original price: %+v", in)
		}
		return "demo://" + in.OrderNo, nil
	}
	order, err := s.CreateOrder(context.Background(), buyer, input())
	if err != nil || order.OriginalAmountFen != 125 || order.AmountFen != 100 || order.RechargeCredits != 1000 || order.GiftCredits != 100 {
		t.Fatal(order, err)
	}
	st.packages[0].OriginalAmountFen = 200
	again, err := s.CreateOrder(context.Background(), buyer, input())
	if err != nil || again.OriginalAmountFen != 125 || ch.creates != 1 {
		t.Fatal(again, err)
	}
	raw, _ = json.Marshal(again.PackageSnapshot)
	if bytes.Contains(raw, []byte(`"name"`)) {
		t.Fatal("internal name in snapshot", string(raw))
	}
}

func TestDiscountedRefundChannelReceivesOnlyPaidAmount(t *testing.T) {
	s, st, ch := newFixture()
	o := order("discounted", p.Credited)
	o.OriginalAmountFen = 125
	o.AmountFen = 100
	st.orders[o.OrderNo] = o
	refund := p.Refund{ID: "discount-refund", RefundNo: "discount-refund", OrderNo: o.OrderNo, AmountFen: 50, RechargeCredits: 500, GiftCredits: 50, Status: "pending", Attempts: 1}
	st.refunds[refund.RefundNo] = refund
	st.claimRefunds = []p.Refund{refund}
	ch.refund = func(_ context.Context, in ports.ChannelRefund) (ports.ChannelRefundResult, error) {
		if in.TotalFen != 100 || in.AmountFen != 50 {
			t.Fatalf("refund used original price: %+v", in)
		}
		return ports.ChannelRefundResult{RefundNo: in.RefundNo, OrderNo: in.OrderNo, TradeNo: in.TradeNo, TotalFen: 100, AmountFen: 50, State: "succeeded", BillAt: &testNow}, nil
	}
	if err := s.ScanRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ch.refundCalls != 1 || st.refunds[refund.RefundNo].Status != "succeeded" {
		t.Fatal(ch.refundCalls, st.refunds)
	}
}
