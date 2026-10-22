package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	paymentapp "drama-agent/internal/application/payment"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

type paymentNotifyChannel struct {
	ports.PaymentChannel
	id         string
	parseError error
	calls      int
	body       []byte
	notify     ports.PaymentNotify
}

func (c *paymentNotifyChannel) ID() string { return c.id }
func (c *paymentNotifyChannel) ParseNotify(_ context.Context, in ports.NotifyInput) (ports.PaymentNotify, ports.NotifyAck, error) {
	c.calls++
	c.body = append([]byte(nil), in.Body...)
	ack := ports.NotifyAck{Status: 204}
	if c.id == "alipay" {
		ack = ports.NotifyAck{Status: 200, ContentType: "text/plain", Body: "success"}
	}
	return c.notify, ack, c.parseError
}

type paymentNotifyStore struct {
	ports.PaymentStore
	order     p.Order
	credited  bool
	unknown   bool
	anomalies []p.Anomaly
}

func (s *paymentNotifyStore) PaymentOrder(context.Context, string) (p.Order, error) {
	return s.order, nil
}
func (s *paymentNotifyStore) CreditPaymentOrder(context.Context, ports.PaymentConfirmation) (p.Order, error) {
	if s.unknown {
		return p.Order{}, fault.New("PAYMENT_COMMIT_UNKNOWN", "结果不明")
	}
	s.credited = true
	s.order.Status = p.Credited
	return s.order, nil
}
func (s *paymentNotifyStore) ConfirmedPaymentCredit(context.Context, string) (p.Order, bool, error) {
	return s.order, s.credited, nil
}
func (s *paymentNotifyStore) RecordPaymentAnomaly(_ context.Context, a p.Anomaly) error {
	s.anomalies = append(s.anomalies, a)
	return nil
}
func (s *paymentNotifyStore) RecordPaymentEvent(context.Context, p.Event) error { return nil }
func TestPaymentNotifyPublicRawBodyCommitAndFailureBoundaries(t *testing.T) {
	for _, id := range []string{"wechatpay", "alipay"} {
		t.Run(id, func(t *testing.T) {
			now := time.Now()
			store := &paymentNotifyStore{order: p.Order{OrderNo: "o", Channel: id, Status: p.Pending, PackageSnapshot: p.PackageSnapshot{AmountFen: 100}}}
			ch := &paymentNotifyChannel{id: id, notify: ports.PaymentNotify{Kind: "pay", Trade: ports.ChannelTrade{OrderNo: "o", TradeNo: "trade", State: "paid", AmountFen: 100, PaidAt: &now}}}
			host, handler := New(&projectapp.Service{}, nil, t.TempDir(), 0)
			if err := host.InstallPayments(&paymentapp.Service{Store: store, Channels: map[string]ports.PaymentChannel{id: ch}}, 64<<10); err != nil {
				t.Fatal(err)
			}
			request := func(body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
				req := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/payments/notify/"+id, strings.NewReader(body))
				if mutate != nil {
					mutate(req)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				return w
			}
			raw := " {\"signed\":\"raw bytes\"}\n"
			w := request(raw, nil)
			want := 204
			if id == "alipay" {
				want = 200
			}
			if w.Code != want || !store.credited || string(ch.body) != raw {
				t.Fatalf("parse/commit/ack %d %s", w.Code, w.Body.String())
			}
			if id == "alipay" && w.Body.String() != "success" {
				t.Fatal("Alipay ack must be exact text")
			}
			store.unknown = true
			store.credited = false
			store.order.Status = p.Pending
			w = request(raw, nil)
			if w.Code != 503 || w.Body.String() == "success" {
				t.Fatal("unknown commit acknowledged", w.Code, w.Body.String())
			}
			store.unknown = false
			host.ReadOnly = true
			before := ch.calls
			w = request(raw, nil)
			if w.Code != 503 || ch.calls != before {
				t.Fatal("read-only callback processed or didn't retry", w.Code)
			}
			host.ReadOnly = false
			for _, test := range []struct{ code, kind string }{{"PAYMENT_SIGNATURE_INVALID", "signature_invalid"}, {"PAYMENT_IDENTITY_MISMATCH", "identity_mismatch"}, {"PAYMENT_INVALID_AMOUNT", "amount_mismatch"}, {"PAYMENT_CHANNEL_FAILED", "notification_invalid"}} {
				ch.parseError = fault.New(test.code, "安全错误")
				before := len(store.anomalies)
				w = request(raw, nil)
				if w.Code < 400 || len(store.anomalies) != before+1 || store.anomalies[before].Kind != test.kind {
					t.Fatal("wrong callback classification", test, store.anomalies)
				}
			}
			ch.parseError = nil
			before = ch.calls
			w = request(strings.Repeat("x", (64<<10)+1), nil)
			if w.Code != 413 || ch.calls != before {
				t.Fatal("oversize notification reached parser")
			}
			before = ch.calls
			w = request(raw, func(r *http.Request) {
				r.Header.Add("Wechatpay-Signature", "one")
				r.Header["wechatpay-signature"] = []string{"two"}
			})
			if w.Code != 400 || ch.calls != before {
				t.Fatal("duplicate signature header reached parser")
			}
			before = ch.calls
			anomalyCount := len(store.anomalies)
			w = request(raw, func(r *http.Request) {
				r.Header["Content-Type"] = []string{"application/json", "text/plain"}
			})
			if w.Code != 400 || ch.calls != before || len(store.anomalies) != anomalyCount+1 || store.anomalies[anomalyCount].Kind != "notification_invalid" {
				t.Fatal("duplicate content type misclassified as signature failure")
			}
			for range 10 {
				before = ch.calls
				w = request(raw, func(r *http.Request) {
					r.Header["Wechatpay-Signature"] = []string{""}
					r.Header["wechatpay-signature"] = []string{"two"}
				})
				if w.Code != 400 || ch.calls != before {
					t.Fatal("empty duplicate signature head bypassed")
				}
			}
		})
	}
}
func TestPaymentAbsentDemoRoutesAreRealNotFound(t *testing.T) {
	host, handler := New(&projectapp.Service{}, nil, t.TempDir(), 0)
	host.Authenticator = &countingAuthenticator{role: ports.RoleUser}
	if err := host.InstallPayments(&paymentapp.Service{Channels: map[string]ports.PaymentChannel{}}, 64<<10); err != nil {
		t.Fatal(err)
	}
	for _, logged := range []bool{false, true} {
		req := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/payments/demo/orders/no/simulate", strings.NewReader(`{"result":"paid"}`))
		if logged {
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "fixture-user.workspace.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 404 {
			t.Fatalf("absent demo route: %d", w.Code)
		}
	}
	callbacks := map[string]bool{}
	for _, route := range host.Routes() {
		if strings.Contains(route.Pattern, "/payments/demo/") {
			t.Fatal("absent demo has registered placeholder")
		}
		if strings.Contains(route.Pattern, "/payments/notify/") {
			if route.Access != "public" {
				t.Fatal("callback not public")
			}
			callbacks[route.Pattern] = true
		}
	}
	if len(callbacks) != 2 {
		t.Fatal("public callback route registry missing")
	}
	if err := host.InstallPayments(&paymentapp.Service{}, 64<<10); err == nil {
		t.Fatal("duplicate install succeeded")
	}
}
func TestPaymentHTTPErrorStatuses(t *testing.T) {
	for _, test := range []struct {
		code   string
		status int
	}{{"PAYMENT_TARGET_FORBIDDEN", 403}, {"PAYMENT_ORDER_NOT_FOUND", 404}, {"PAYMENT_REFUND_RESTORE_PENDING", 503}, {"PAYMENT_COMMIT_UNKNOWN", 503}, {"PAYMENT_DAILY_LIMIT", 409}, {"PAYMENT_SIGNATURE_INVALID", 400}, {"RATE_LIMITED", 429}} {
		status, _ := errorResponse(fault.New(test.code, "fixture"))
		if status != test.status {
			t.Errorf("%s: %d", test.code, status)
		}
	}
}
