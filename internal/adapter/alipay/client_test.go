package alipay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

var keyOnce sync.Once
var appKey, platformKey *rsa.PrivateKey
var keyErr error
var testNow = time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC)

const orderNo = "PO20261020120000ABCDEFGHIJ"
const tradeNo = "test-trade"
const refundNo = "test-refund"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func keys(t *testing.T) {
	t.Helper()
	keyOnce.Do(func() {
		appKey, keyErr = rsa.GenerateKey(rand.Reader, 2048)
		if keyErr == nil {
			platformKey, keyErr = rsa.GenerateKey(rand.Reader, 2048)
		}
	})
	if keyErr != nil {
		t.Fatal(keyErr)
	}
}
func config(t *testing.T, transport http.RoundTripper) Config {
	t.Helper()
	keys(t)
	dir := t.TempDir()
	privatePath := dir + "/private.pem"
	publicPath := dir + "/public.pem"
	private, err := x509.MarshalPKCS8PrivateKey(appKey)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&platformKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}), 0600); err != nil {
		t.Fatal(err)
	}
	return Config{Gateway: "sandbox", AppID: "test-app", SellerID: "test-seller", AppPrivateKeyFile: privatePath, AlipayPublicKeyFile: publicPath, NotifyURL: "https://example.invalid/api/payments/notify/alipay", HTTPClient: &http.Client{Transport: transport}, Now: func() time.Time { return testNow }}
}
func newChannel(t *testing.T, transport http.RoundTripper) *Channel {
	t.Helper()
	c, err := New(config(t, transport))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func testSignature(t *testing.T, key *rsa.PrivateKey, raw string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(raw))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}
func signedResponse(t *testing.T, method, raw string) *http.Response {
	t.Helper()
	encoded, _ := json.Marshal(testSignature(t, platformKey, raw))
	return rawResponse(`{"` + strings.ReplaceAll(method, ".", "_") + `_response":` + raw + `,"sign":` + string(encoded) + `}`)
}
func rawResponse(raw string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}
}
func request(t *testing.T, r *http.Request) (string, map[string]json.RawMessage) {
	t.Helper()
	if r.Method != "POST" || r.URL.String() != sandboxGateway {
		t.Fatalf("gateway request %s %s", r.Method, r.URL)
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("app_id") != "test-app" || values.Get("sign_type") != "RSA2" || values.Get("timestamp") != "2026-10-20 12:00:00" {
		t.Fatal("public parameters")
	}
	names := []string{}
	for k := range values {
		if k != "sign" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	parts := []string{}
	for _, k := range names {
		parts = append(parts, k+"="+values.Get(k))
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
	sig, err := base64.StdEncoding.DecodeString(values.Get("sign"))
	if err != nil || rsa.VerifyPKCS1v15(&appKey.PublicKey, crypto.SHA256, digest[:], sig) != nil {
		t.Fatal("request must sign unescaped values including sign_type")
	}
	var biz map[string]json.RawMessage
	if json.Unmarshal([]byte(values.Get("biz_content")), &biz) != nil {
		t.Fatal("biz_content")
	}
	return values.Get("method"), biz
}
func stringValue(t *testing.T, biz map[string]json.RawMessage, key string) string {
	t.Helper()
	var s string
	if json.Unmarshal(biz[key], &s) != nil {
		t.Fatalf("missing %s", key)
	}
	return s
}
func codeOf(err error) string {
	var f *fault.Error
	if errors.As(err, &f) {
		return f.Code
	}
	return ""
}
func tradeJSON(status, money string) string {
	return `{"code":"10000","out_trade_no":"` + orderNo + `","trade_no":"` + tradeNo + `","trade_status":"` + status + `","total_amount":"` + money + `","buyer_pay_amount":"0.01","send_pay_date":"2026-10-20 11:59:59"}`
}
func refundJSON(status string) string {
	return `{"code":"10000","out_trade_no":"` + orderNo + `","trade_no":"` + tradeNo + `","out_request_no":"` + refundNo + `","refund_amount":"1.2","total_amount":"2","refund_status":"` + status + `","gmt_refund_pay":"2026-10-20 12:00:00"}`
}

func TestFiveActionsAndOriginalRSA2(t *testing.T) {
	actions := []string{}
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		method, biz := request(t, r)
		actions = append(actions, method)
		switch method {
		case "alipay.trade.precreate":
			for key, want := range map[string]string{"out_trade_no": orderNo, "total_amount": "2.00", "seller_id": "test-seller", "qr_code_timeout_express": "14m", "timeout_express": "14m", "subject": "充值 + % & ="} {
				if stringValue(t, biz, key) != want {
					t.Fatalf("%s", key)
				}
			}
			return signedResponse(t, method, `{ "out_trade_no":"`+orderNo+`", "code":"10000", "qr_code":"https:\/\/qr.alipay.com\/test", "extra":{"sign":"nested","text":"\u4e2d"} }`), nil
		case "alipay.trade.query":
			return signedResponse(t, method, tradeJSON("TRADE_SUCCESS", "2")), nil
		case "alipay.trade.close":
			return signedResponse(t, method, `{"code":"10000","out_trade_no":"`+orderNo+`"}`), nil
		case "alipay.trade.refund":
			if stringValue(t, biz, "out_request_no") != refundNo || stringValue(t, biz, "refund_amount") != "1.20" {
				t.Fatal("stable refund request")
			}
			return signedResponse(t, method, `{"code":"10000","out_trade_no":"`+orderNo+`","trade_no":"`+tradeNo+`","refund_fee":"1.20","fund_change":"Y","gmt_refund_pay":"2026-10-20 12:00:00"}`), nil
		case "alipay.trade.fastpay.refund.query":
			return signedResponse(t, method, refundJSON("REFUND_SUCCESS")), nil
		}
		t.Fatalf("unexpected method %s", method)
		return nil, nil
	}))
	qr, err := c.Create(context.Background(), ports.ChannelOrder{OrderNo: orderNo, Description: "充值 + % & =", AmountFen: 200, ExpiresAt: testNow.Add(14*time.Minute + 59*time.Second)})
	if err != nil || qr != "https://qr.alipay.com/test" {
		t.Fatalf("create %q %v", qr, err)
	}
	tr, err := c.Query(context.Background(), orderNo)
	if err != nil || tr.State != "paid" || tr.AmountFen != 200 || tr.PaidAt == nil || tr.PaidAt.Format(time.RFC3339) != "2026-10-20T03:59:59Z" {
		t.Fatalf("query %+v %v", tr, err)
	}
	if err = c.Close(context.Background(), orderNo); err != nil {
		t.Fatal(err)
	}
	refunded, err := c.Refund(context.Background(), ports.ChannelRefund{OrderNo: orderNo, TradeNo: tradeNo, RefundNo: refundNo, AmountFen: 120, TotalFen: 200})
	if err != nil || refunded.State != "succeeded" || refunded.BillAt == nil {
		t.Fatalf("refund %+v %v", refunded, err)
	}
	queried, err := c.QueryRefund(context.Background(), refundNo, orderNo)
	if err != nil || queried.State != "succeeded" || queried.AmountFen != 120 || queried.BillAt == nil {
		t.Fatalf("refund query %+v %v", queried, err)
	}
	if len(actions) != 5 {
		t.Fatal(actions)
	}
}
func TestResponseVerificationRejectsTamperingAndAmbiguity(t *testing.T) {
	cases := []struct {
		name, body string
		want       string
	}{
		{"tampered", `{"alipay_trade_query_response":` + tradeJSON("TRADE_SUCCESS", "2") + `,"sign":"bad"}`, "PAYMENT_SIGNATURE_INVALID"},
		{"duplicate response", `{"alipay_trade_query_response":{},"alipay_trade_query_response":{},"sign":"x"}`, "PAYMENT_PROTOCOL_INVALID"},
		{"duplicate sign", `{"alipay_trade_query_response":{},"sign":"a","sign":"b"}`, "PAYMENT_PROTOCOL_INVALID"},
		{"unsigned error", `{"error_response":{"code":"40004","sub_code":"ACQ.TRADE_NOT_ALLOW_REFUND"}}`, "PAYMENT_PROTOCOL_INVALID"},
		{"missing sign", `{"alipay_trade_query_response":` + tradeJSON("TRADE_SUCCESS", "2") + `}`, "PAYMENT_SIGNATURE_INVALID"},
		{"trailing json", `{} {}`, "PAYMENT_PROTOCOL_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChannel(t, roundTripFunc(func(*http.Request) (*http.Response, error) { return rawResponse(tc.body), nil }))
			_, err := c.Query(context.Background(), orderNo)
			if codeOf(err) != tc.want {
				t.Fatalf("%v", err)
			}
		})
	}
	t.Run("reserialized signature fails", func(t *testing.T) {
		c := newChannel(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			raw := tradeJSON("TRADE_SUCCESS", "2")
			signed := signedResponse(t, "alipay.trade.query", raw)
			body, _ := io.ReadAll(signed.Body)
			body = bytes.Replace(body, []byte(`"code":"10000"`), []byte(`"code" : "10000"`), 1)
			return rawResponse(string(body)), nil
		}))
		_, err := c.Query(context.Background(), orderNo)
		if codeOf(err) != "PAYMENT_SIGNATURE_INVALID" {
			t.Fatal(err)
		}
	})
	t.Run("duplicate inner keys", func(t *testing.T) {
		c := newChannel(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return signedResponse(t, "alipay.trade.query", `{"code":"10000","code":"40004"}`), nil
		}))
		_, err := c.Query(context.Background(), orderNo)
		if codeOf(err) != "PAYMENT_PROTOCOL_INVALID" {
			t.Fatal(err)
		}
	})
}
func TestQueryStatesAmountsAndIdentity(t *testing.T) {
	for _, tc := range []struct{ status, money, state, want string }{{"TRADE_SUCCESS", "2.0", "paid", ""}, {"TRADE_FINISHED", "2.00", "paid", ""}, {"WAIT_BUYER_PAY", "2", "notpay", ""}, {"TRADE_CLOSED", "2", "closed", ""}, {"NEW_STATUS", "2", "", "PAYMENT_PROTOCOL_INVALID"}, {"TRADE_SUCCESS", "2.001", "", "PAYMENT_INVALID_AMOUNT"}, {"TRADE_SUCCESS", "2e0", "", "PAYMENT_INVALID_AMOUNT"}, {"TRADE_SUCCESS", "-2", "", "PAYMENT_INVALID_AMOUNT"}, {"TRADE_SUCCESS", " 2", "", "PAYMENT_INVALID_AMOUNT"}, {"TRADE_SUCCESS", "0", "", "PAYMENT_INVALID_AMOUNT"}} {
		t.Run(tc.status+tc.money, func(t *testing.T) {
			c := newChannel(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return signedResponse(t, "alipay.trade.query", tradeJSON(tc.status, tc.money)), nil
			}))
			tr, err := c.Query(context.Background(), orderNo)
			if codeOf(err) != tc.want || (err == nil && tr.State != tc.state) {
				t.Fatalf("%+v %v", tr, err)
			}
		})
	}
	for _, tc := range []struct{ name, raw string }{{"order", strings.ReplaceAll(tradeJSON("TRADE_SUCCESS", "2"), orderNo, "wrong-order")}, {"app", strings.Replace(tradeJSON("TRADE_SUCCESS", "2"), `"code":"10000"`, `"code":"10000","app_id":"wrong-app"`, 1)}, {"seller", strings.Replace(tradeJSON("TRADE_SUCCESS", "2"), `"code":"10000"`, `"code":"10000","seller_id":"wrong-seller"`, 1)}} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChannel(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return signedResponse(t, "alipay.trade.query", tc.raw), nil
			}))
			_, err := c.Query(context.Background(), orderNo)
			if codeOf(err) != "PAYMENT_IDENTITY_MISMATCH" {
				t.Fatal(err)
			}
		})
	}
}
func TestTransportErrorsAndLimitsDoNotLeak(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport roundTripFunc
	}{{"timeout", func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-app-and-token") }}, {"http500", func(*http.Request) (*http.Response, error) {
		r := rawResponse("secret-body")
		r.StatusCode = 500
		return r, nil
	}}, {"oversize", func(*http.Request) (*http.Response, error) { return rawResponse(strings.Repeat("x", 256)), nil }}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config(t, tc.transport)
			cfg.ResponseMaxBytes = 128
			c, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Query(context.Background(), orderNo)
			if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "test-app") {
				t.Fatal(err)
			}
		})
	}
}
func TestConfigurationAndExpiry(t *testing.T) {
	cfg := config(t, roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("must not send"); return nil, nil }))
	for _, change := range []func(*Config){func(c *Config) { c.AppID = "" }, func(c *Config) { c.SellerID = "" }, func(c *Config) { c.Gateway = "https://evil.invalid" }, func(c *Config) { c.NotifyURL = "http://example.invalid" }, func(c *Config) { c.BillMaxRows = -1 }, func(c *Config) { c.AppPrivateKeyFile = "/unreadable-private-secret" }} {
		bad := cfg
		change(&bad)
		if _, err := New(bad); codeOf(err) != "PAYMENT_CONFIG_INVALID" || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, ttl := range []time.Duration{-time.Second, 59 * time.Second, 16 * time.Minute} {
		_, err = c.Create(context.Background(), ports.ChannelOrder{OrderNo: orderNo, Description: "充值", AmountFen: 1, ExpiresAt: testNow.Add(ttl)})
		if err == nil {
			t.Fatal("invalid TTL")
		}
	}
	cfg.Gateway = "production"
	c, err = New(cfg)
	if err != nil || c.gateway != productionGateway {
		t.Fatal(err)
	}
}

func TestKeyFormatsAndMalformedKeyFiles(t *testing.T) {
	cfg := config(t, nil)
	private := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(appKey)})
	public := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&platformKey.PublicKey)})
	if err := os.WriteFile(cfg.AppPrivateKeyFile, private, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.AlipayPublicKeyFile, public, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		path     string
		contents []byte
	}{
		{"private duplicate key", cfg.AppPrivateKeyFile, append(append([]byte(nil), private...), private...)},
		{"public trailing material", cfg.AlipayPublicKeyFile, append(append([]byte(nil), public...), []byte("trailing-secret")...)},
		{"malformed private", cfg.AppPrivateKeyFile, []byte("invalid-private-secret")},
		{"certificate instead of public key", cfg.AlipayPublicKeyFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid")})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(tc.path, tc.contents, 0600); err != nil {
				t.Fatal(err)
			}
			_, err = New(cfg)
			if codeOf(err) != "PAYMENT_CONFIG_INVALID" || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
			if err = os.WriteFile(tc.path, original, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestGatewayRedirectIsNotFollowed(t *testing.T) {
	count := 0
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		count++
		if count > 1 {
			t.Fatal("redirect leaked signed form")
		}
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": {"https://evil.invalid/gateway.do"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	_, err := c.Query(context.Background(), orderNo)
	if err == nil || count != 1 {
		t.Fatal(err)
	}
}
func TestChannelConcurrentQueries(t *testing.T) {
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		method, _ := request(t, r)
		return signedResponse(t, method, tradeJSON("TRADE_SUCCESS", "2")), nil
	}))
	var wait sync.WaitGroup
	for range 12 {
		wait.Go(func() {
			tr, err := c.Query(context.Background(), orderNo)
			if err != nil || tr.State != "paid" {
				t.Errorf("%+v %v", tr, err)
			}
		})
	}
	wait.Wait()
}
