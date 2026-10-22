package alipay

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"drama-agent/internal/application/ports"
)

func notifyValues() url.Values {
	return url.Values{"app_id": {"test-app"}, "seller_id": {"test-seller"}, "out_trade_no": {orderNo}, "trade_no": {tradeNo}, "trade_status": {"TRADE_SUCCESS"}, "total_amount": {"2"}, "buyer_pay_amount": {"0.01"}, "gmt_payment": {"2026-10-20 11:59:59"}, "sign_type": {"RSA2"}, "unknown_field": {"中文 + %2B & = %"}}
}
func notifyBody(t *testing.T, values url.Values) []byte {
	t.Helper()
	keys(t)
	names := []string{}
	for k := range values {
		if k != "sign" && k != "sign_type" && values.Get(k) != "" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	parts := []string{}
	for _, k := range names {
		parts = append(parts, k+"="+values.Get(k))
	}
	values.Set("sign", testSignature(t, platformKey, strings.Join(parts, "&")))
	return []byte(values.Encode())
}
func TestNotifySingleURLDecodeUnknownFieldsAndTotalAmount(t *testing.T) {
	c := newChannel(t, roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("notification is offline"); return nil, nil }))
	body := notifyBody(t, notifyValues())
	notice, ack, err := c.ParseNotify(context.Background(), ports.NotifyInput{Body: body})
	if err != nil || notice.Kind != "pay" || notice.Trade.AmountFen != 200 || notice.Trade.State != "paid" || notice.Trade.PaidAt == nil || ack.Status != 200 || ack.Body != "success" {
		t.Fatalf("%+v %+v %v", notice, ack, err)
	}
	// Removing or re-decoding a future field would change the canonical signature.
	body = []byte(strings.Replace(string(body), "%252B", "%2B", 1))
	_, _, err = c.ParseNotify(context.Background(), ports.NotifyInput{Body: body})
	if codeOf(err) != "PAYMENT_SIGNATURE_INVALID" {
		t.Fatal(err)
	}
}
func TestNotifyRejectsProtocolSignatureIdentityAndAmounts(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     func(url.Values)
		body       func([]byte) []byte
	}{
		{name: "bad signature", want: "PAYMENT_SIGNATURE_INVALID", body: func(b []byte) []byte { return []byte(strings.Replace(string(b), "test-trade", "tampered", 1)) }},
		{name: "unknown field tamper", want: "PAYMENT_SIGNATURE_INVALID", body: func(b []byte) []byte { return append(b, []byte("&new_field=unsigned")...) }},
		{name: "duplicate app", want: "PAYMENT_PROTOCOL_INVALID", body: func(b []byte) []byte { return append(b, []byte("&app_id=test-app")...) }},
		{name: "encoded duplicate", want: "PAYMENT_PROTOCOL_INVALID", body: func(b []byte) []byte { return append(b, []byte("&app%5Fid=test-app")...) }},
		{name: "duplicate signature", want: "PAYMENT_PROTOCOL_INVALID", body: func(b []byte) []byte { return append(b, []byte("&sign=x")...) }},
		{name: "wrong signature type", want: "PAYMENT_SIGNATURE_INVALID", mutate: func(v url.Values) { v.Set("sign_type", "RSA") }},
		{name: "missing signature type", want: "PAYMENT_SIGNATURE_INVALID", mutate: func(v url.Values) { v.Del("sign_type") }},
		{name: "wrong app", want: "PAYMENT_IDENTITY_MISMATCH", mutate: func(v url.Values) { v.Set("app_id", "wrong-app") }},
		{name: "wrong seller", want: "PAYMENT_IDENTITY_MISMATCH", mutate: func(v url.Values) { v.Set("seller_id", "wrong-seller") }},
		{name: "missing seller", want: "PAYMENT_IDENTITY_MISMATCH", mutate: func(v url.Values) { v.Del("seller_id") }},
		{name: "empty order", want: "PAYMENT_IDENTITY_MISMATCH", mutate: func(v url.Values) { v.Del("out_trade_no") }},
		{name: "unknown state", want: "PAYMENT_PROTOCOL_INVALID", mutate: func(v url.Values) { v.Set("trade_status", "NEW_STATUS") }},
		{name: "missing transaction", want: "PAYMENT_PROTOCOL_INVALID", mutate: func(v url.Values) { v.Del("trade_no") }},
		{name: "missing payment date", want: "PAYMENT_PROTOCOL_INVALID", mutate: func(v url.Values) { v.Del("gmt_payment") }},
		{name: "precision", want: "PAYMENT_INVALID_AMOUNT", mutate: func(v url.Values) { v.Set("total_amount", "2.001") }},
		{name: "negative", want: "PAYMENT_INVALID_AMOUNT", mutate: func(v url.Values) { v.Set("total_amount", "-2") }},
		{name: "exponent", want: "PAYMENT_INVALID_AMOUNT", mutate: func(v url.Values) { v.Set("total_amount", "2e0") }},
		{name: "space", want: "PAYMENT_INVALID_AMOUNT", mutate: func(v url.Values) { v.Set("total_amount", "2 ") }},
		{name: "overflow", want: "PAYMENT_INVALID_AMOUNT", mutate: func(v url.Values) { v.Set("total_amount", "90071992547409.92") }},
		{name: "invalid form", want: "PAYMENT_PROTOCOL_INVALID", body: func([]byte) []byte { return []byte("app_id=%ZZ") }},
		{name: "oversize", want: "PAYMENT_PROTOCOL_INVALID", body: func([]byte) []byte { return []byte(strings.Repeat("x", 65537)) }},
	}
	c := newChannel(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := notifyValues()
			if tc.mutate != nil {
				tc.mutate(values)
			}
			body := notifyBody(t, values)
			if tc.body != nil {
				body = tc.body(body)
			}
			notice, ack, err := c.ParseNotify(context.Background(), ports.NotifyInput{Body: body})
			if codeOf(err) != tc.want || notice.Kind != "" || ack.Body != "fail" || ack.Status < 400 {
				t.Fatalf("%+v %+v %v", notice, ack, err)
			}
			if strings.Contains(err.Error(), "wrong-app") || strings.Contains(err.Error(), "wrong-seller") {
				t.Fatal("identity leaked")
			}
		})
	}
}
func TestNotifyClosedIsNotARefundAndUnpaidDoesNotCredit(t *testing.T) {
	c := newChannel(t, nil)
	for _, tc := range []struct{ status, state string }{{"TRADE_CLOSED", "closed"}, {"WAIT_BUYER_PAY", "notpay"}} {
		values := notifyValues()
		values.Set("trade_status", tc.status)
		values.Set("refund_fee", "1.20")
		notice, _, err := c.ParseNotify(context.Background(), ports.NotifyInput{Body: notifyBody(t, values)})
		if err != nil || notice.Kind != "pay" || notice.Trade.State != tc.state {
			t.Fatalf("%+v %v", notice, err)
		}
	}
}
