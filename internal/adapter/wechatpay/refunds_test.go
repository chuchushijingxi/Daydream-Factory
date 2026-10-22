package wechatpay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
)

func TestRefundStatesAndProtocolFacts(t *testing.T) {
	for _, test := range []struct{ state, want string }{{"SUCCESS", "succeeded"}, {"PROCESSING", "pending"}, {"ABNORMAL", "pending"}, {"CLOSED", "failed"}} {
		t.Run(test.state, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				return signedResponse(t, 200, rawJSON(t, refundFact(test.state))), nil
			})
			out, err := newClient(t, cfg).QueryRefund(context.Background(), refundNo, orderNo)
			if err != nil || out.State != test.want {
				t.Fatalf("state %+v %v", out, err)
			}
		})
	}
	for _, test := range []struct {
		name, code string
		change     func(map[string]any)
	}{
		{"merchant when present", "PAYMENT_IDENTITY_MISMATCH", func(f map[string]any) { f["mchid"] = "other" }},
		{"app when present", "PAYMENT_IDENTITY_MISMATCH", func(f map[string]any) { f["appid"] = "other" }},
		{"order", "PAYMENT_IDENTITY_MISMATCH", func(f map[string]any) { f["out_trade_no"] = "other" }},
		{"refund no", "PAYMENT_IDENTITY_MISMATCH", func(f map[string]any) { f["out_refund_no"] = "other" }},
		{"currency", "PAYMENT_INVALID_AMOUNT", func(f map[string]any) { f["amount"].(map[string]any)["currency"] = "USD" }},
		{"larger refund", "PAYMENT_INVALID_AMOUNT", func(f map[string]any) { f["amount"].(map[string]any)["refund"] = 101 }},
		{"non-original channel", "PAYMENT_CHANNEL_FAILED", func(f map[string]any) { f["channel"] = "BALANCE" }},
		{"unknown", "PAYMENT_CHANNEL_FAILED", func(f map[string]any) { f["status"] = "UNEXPECTED" }},
		{"missing acceptance time", "PAYMENT_CHANNEL_FAILED", func(f map[string]any) { delete(f, "create_time") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				f := refundFact("SUCCESS")
				test.change(f)
				return signedResponse(t, 200, rawJSON(t, f)), nil
			})
			_, err := newClient(t, cfg).QueryRefund(context.Background(), refundNo, orderNo)
			requireCode(t, err, test.code)
		})
	}
}
func TestRefundRejectionRequiresImmediateVerifiedQuery(t *testing.T) {
	for _, test := range []struct {
		name, queryState, want, errorCode string
		unsigned                          bool
	}{
		{name: "definitely absent", queryState: "not_found", want: "failed"},
		{name: "actually pending", queryState: "PROCESSING", want: "pending"},
		{name: "actually succeeded", queryState: "SUCCESS", want: "succeeded"},
		{name: "query failure", queryState: "http_error", errorCode: "PAYMENT_CHANNEL_FAILED"},
		{name: "query unsigned", queryState: "not_found", unsigned: true, errorCode: "PAYMENT_SIGNATURE_INVALID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig(t)
			calls := 0
			cfg.HTTPClient.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method == http.MethodPost {
					return signedResponse(t, 400, []byte(`{"code":"USER_ACCOUNT_ABNORMAL","message":"private refusal payload"}`)), nil
				}
				var response *http.Response
				switch test.queryState {
				case "not_found":
					response = signedResponse(t, 404, []byte(`{"code":"RESOURCE_NOT_EXISTS"}`))
				case "http_error":
					response = signedResponse(t, 500, []byte(`{"code":"SYSTEM_ERROR"}`))
				default:
					response = signedResponse(t, 200, rawJSON(t, refundFact(test.queryState)))
				}
				if test.unsigned {
					response.Header = http.Header{}
				}
				return response, nil
			})
			out, err := newClient(t, cfg).Refund(context.Background(), ports.ChannelRefund{RefundNo: refundNo, OrderNo: orderNo, TradeNo: "WXTRADE", AmountFen: 50, TotalFen: 100})
			if calls != 2 {
				t.Fatalf("refusal not followed by query %d", calls)
			}
			if test.errorCode != "" {
				requireCode(t, err, test.errorCode)
				if out.State == "failed" {
					t.Fatal("unknown rejected as final failure")
				}
			} else if err != nil || out.State != test.want {
				t.Fatalf("refund %+v %v", out, err)
			}
			if err != nil && strings.Contains(err.Error(), "private refusal payload") {
				t.Fatal("raw business refusal leaked")
			}
		})
	}
}
func TestRefundBalanceShortageAndInitialClosedAreNotFinal(t *testing.T) {
	for _, mode := range []string{"NOT_ENOUGH", "CLOSED"} {
		t.Run(mode, func(t *testing.T) {
			cfg := testConfig(t)
			calls := 0
			cfg.HTTPClient.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method == http.MethodGet {
					return signedResponse(t, 200, rawJSON(t, refundFact("PROCESSING"))), nil
				}
				if mode == "NOT_ENOUGH" {
					return signedResponse(t, 400, []byte(`{"code":"NOT_ENOUGH"}`)), nil
				}
				return signedResponse(t, 200, rawJSON(t, refundFact("CLOSED"))), nil
			})
			out, err := newClient(t, cfg).Refund(context.Background(), ports.ChannelRefund{RefundNo: refundNo, OrderNo: orderNo, AmountFen: 50, TotalFen: 100})
			if mode == "NOT_ENOUGH" {
				requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
				if calls != 1 {
					t.Fatal("caller should query uncertain rejection")
				}
			} else if err != nil || out.State != "pending" || calls != 2 {
				t.Fatalf("closed without latest query %+v %v", out, err)
			}
		})
	}
}
func TestRefundNotificationOptionalCurrencyAndMissingBillTime(t *testing.T) {
	cfg := testConfig(t)
	c := newClient(t, cfg)
	fact := refundFact("SUCCESS")
	fact["mchid"] = "testmerchant"
	fact["refund_status"] = "SUCCESS"
	delete(fact, "status")
	delete(fact, "create_time")
	delete(fact["amount"].(map[string]any), "currency")
	notice, ack, err := c.ParseNotify(context.Background(), encryptedNotify(t, cfg, "REFUND.SUCCESS", fact))
	if err != nil || ack.Status != 204 || notice.Refund.State != "succeeded" || notice.Refund.BillAt != nil {
		t.Fatalf("official notification fields %+v %v", notice, err)
	}
	fact["amount"].(map[string]any)["currency"] = "USD"
	_, _, err = c.ParseNotify(context.Background(), encryptedNotify(t, cfg, "REFUND.SUCCESS", fact))
	requireCode(t, err, "PAYMENT_INVALID_AMOUNT")
	delete(fact["amount"].(map[string]any), "currency")
	fact["mchid"] = "other"
	_, _, err = c.ParseNotify(context.Background(), encryptedNotify(t, cfg, "REFUND.SUCCESS", fact))
	requireCode(t, err, "PAYMENT_IDENTITY_MISMATCH")
}
func TestNotificationClosedRequiresLatestQuery(t *testing.T) {
	cfg := testConfig(t)
	calls := 0
	cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return signedResponse(t, 200, rawJSON(t, refundFact("PROCESSING"))), nil
	})
	fact := refundFact("CLOSED")
	fact["mchid"] = "testmerchant"
	fact["refund_status"] = "CLOSED"
	delete(fact, "status")
	notice, _, err := newClient(t, cfg).ParseNotify(context.Background(), encryptedNotify(t, cfg, "REFUND.CLOSED", fact))
	if err != nil || calls != 1 || notice.Refund.State != "pending" {
		t.Fatalf("closed callback finalized without latest evidence %+v %v", notice, err)
	}
}
func TestInputAndHTTPFailuresAreBoundedAndSafe(t *testing.T) {
	cfg := testConfig(t)
	_, err := newClient(t, cfg).Create(context.Background(), ports.ChannelOrder{OrderNo: orderNo, AmountFen: 100, Description: "积分", ExpiresAt: fixtureNow.Add(-time.Second)})
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return signedResponse(t, 404, []byte(`{"code":"ORDER_NOT_EXIST"}`)), nil
	})
	c := newClient(t, cfg)
	trade, err := c.Query(context.Background(), orderNo)
	if err != nil || trade.State != "notpay" {
		t.Fatalf("missing trade %+v %v", trade, err)
	}
	if err = c.Close(context.Background(), orderNo); err != nil {
		t.Fatal(err)
	}
	cfg.MaxResponseBytes = 8
	cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return signedResponse(t, 200, rawJSON(t, tradeFact())), nil
	})
	_, err = newClient(t, cfg).Query(context.Background(), orderNo)
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	cfg = testConfig(t)
	calls := 0
	cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		r := signedResponse(t, 302, []byte(`{"code":"REDIRECT"}`))
		r.Header.Set("Location", "https://private.invalid/token")
		return r, nil
	})
	_, err = newClient(t, cfg).Query(context.Background(), orderNo)
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	if calls != 1 || strings.Contains(err.Error(), "private.invalid") {
		t.Fatal("redirect followed or leaked")
	}
}

func TestRefundReasonUTF8LimitAndStableRetry(t *testing.T) {
	for _, reason := range []string{strings.Repeat("中", 40), strings.Repeat("🙂", 30), "   " + strings.Repeat("中🙂", 30)} {
		cfg := testConfig(t)
		var first []byte
		calls := 0
		cfg.HTTPClient.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
			body := checkRequestSignature(t, req)
			calls++
			if calls == 1 {
				first = body
			} else if string(first) != string(body) {
				t.Fatal("same refund retry changed channel fields")
			}
			var payload struct {
				Reason   string `json:"reason"`
				RefundNo string `json:"out_refund_no"`
			}
			if json.Unmarshal(body, &payload) != nil {
				t.Fatal("bad request")
			}
			if len(payload.Reason) > 80 || !utf8.ValidString(payload.Reason) || !strings.HasPrefix(strings.TrimSpace(reason), payload.Reason) || payload.RefundNo != refundNo {
				t.Fatal("refund reason exceeded protocol or split rune")
			}
			return signedResponse(t, 200, rawJSON(t, refundFact("PROCESSING"))), nil
		})
		client := newClient(t, cfg)
		in := ports.ChannelRefund{RefundNo: refundNo, OrderNo: orderNo, TradeNo: "WXTRADE", AmountFen: 50, TotalFen: 100, Reason: reason}
		for range 2 {
			if _, err := client.Refund(context.Background(), in); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRefundOptionalIdentityRejectsExplicitEmptyOrNull(t *testing.T) {
	for _, field := range []string{"appid", "mchid"} {
		for _, value := range []any{"", nil} {
			t.Run(field+"/"+string(rawJSON(t, value)), func(t *testing.T) {
				cfg := testConfig(t)
				cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
					fact := refundFact("SUCCESS")
					fact[field] = value
					return signedResponse(t, 200, rawJSON(t, fact)), nil
				})
				_, err := newClient(t, cfg).QueryRefund(context.Background(), refundNo, orderNo)
				requireCode(t, err, "PAYMENT_IDENTITY_MISMATCH")
			})
		}
	}
}

func TestRefundNotifyOptionalCurrencyRejectsExplicitEmptyOrNull(t *testing.T) {
	for _, value := range []any{"", nil} {
		t.Run(string(rawJSON(t, value)), func(t *testing.T) {
			cfg := testConfig(t)
			fact := refundFact("SUCCESS")
			fact["mchid"] = "testmerchant"
			fact["refund_status"] = "SUCCESS"
			delete(fact, "status")
			fact["amount"].(map[string]any)["currency"] = value
			_, ack, err := newClient(t, cfg).ParseNotify(context.Background(), encryptedNotify(t, cfg, "REFUND.SUCCESS", fact))
			requireCode(t, err, "PAYMENT_INVALID_AMOUNT")
			if ack.Status == http.StatusNoContent {
				t.Fatal("invalid currency acknowledged")
			}
		})
	}
}
