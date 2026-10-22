package wechatpay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

var fixtureNow = time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC)

const orderNo = "PO20261020120000ABCDEFGHIJ"
const refundNo = "PRofflinefixture"

var keysOnce sync.Once
var merchantKey, platformKey *rsa.PrivateKey

func testKeys(t *testing.T) {
	t.Helper()
	keysOnce.Do(func() {
		var err error
		merchantKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		platformKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
	})
}

type roundTrip func(*http.Request) (*http.Response, error)

func (rt roundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return rt(req) }
func testConfig(t *testing.T) Config {
	testKeys(t)
	apiKey := make([]byte, 32)
	if _, err := rand.Read(apiKey); err != nil {
		t.Fatal(err)
	}
	return Config{MchID: "testmerchant", AppID: "testapp", MchSerialNo: "TESTMERCHANTSERIAL", VerifySerial: "PUB_KEY_ID_NEW", NotifyURL: "https://example.invalid/api/payments/notify/wechatpay", PrivateKey: merchantKey, APIV3Key: apiKey, VerifyKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_NEW": &platformKey.PublicKey}, Now: func() time.Time { return fixtureNow }, RequestTimeout: time.Second, SignatureWindow: 5 * time.Minute, MaxResponseBytes: 1 << 20, NotifyMaxBytes: 64 << 10, MaxBillCompressedBytes: 1 << 20, MaxBillDecompressedBytes: 2 << 20, MaxBillCSVBytes: 1 << 20, MaxBillRows: 1000, MaxBillFieldBytes: 4096, HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network forbidden in offline test")
	})}}
}
func newClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func rawJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func signedResponse(t *testing.T, status int, body []byte) *http.Response {
	t.Helper()
	header := http.Header{}
	timestamp := strconv.FormatInt(fixtureNow.Unix(), 10)
	nonce := "platformnonce"
	header.Set("Wechatpay-Serial", "PUB_KEY_ID_NEW")
	header.Set("Wechatpay-Timestamp", timestamp)
	header.Set("Wechatpay-Nonce", nonce)
	digest := sha256.Sum256([]byte(timestamp + "\n" + nonce + "\n" + string(body) + "\n"))
	sig, err := rsa.SignPKCS1v15(rand.Reader, platformKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	header.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body))}
}
func checkRequestSignature(t *testing.T, req *http.Request) []byte {
	t.Helper()
	if req.Header.Get("Wechatpay-Serial") == "" {
		t.Fatal("missing current WeChat verify serial")
	}
	if req.URL.Scheme != "https" || req.URL.Host != "api.mch.weixin.qq.com" {
		t.Fatal("unexpected endpoint")
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	auth := strings.TrimPrefix(req.Header.Get("Authorization"), "WECHATPAY2-SHA256-RSA2048 ")
	for _, entry := range strings.Split(auth, ",") {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			t.Fatal("malformed authorization")
		}
		value, err := strconv.Unquote(parts[1])
		if err != nil {
			t.Fatal(err)
		}
		values[parts[0]] = value
	}
	if values["mchid"] != "testmerchant" || values["serial_no"] != "TESTMERCHANTSERIAL" || values["timestamp"] != strconv.FormatInt(fixtureNow.Unix(), 10) || values["nonce_str"] == "" {
		t.Fatal("incorrect signed identity")
	}
	message := req.Method + "\n" + req.URL.RequestURI() + "\n" + values["timestamp"] + "\n" + values["nonce_str"] + "\n" + string(raw) + "\n"
	digest := sha256.Sum256([]byte(message))
	sig, err := base64.StdEncoding.DecodeString(values["signature"])
	if err != nil || rsa.VerifyPKCS1v15(&merchantKey.PublicKey, crypto.SHA256, digest[:], sig) != nil {
		t.Fatal("request signature did not cover exact path/query/body")
	}
	return raw
}
func tradeFact() map[string]any {
	return map[string]any{"appid": "testapp", "mchid": "testmerchant", "out_trade_no": orderNo, "transaction_id": "WXTRADE", "trade_state": "SUCCESS", "success_time": fixtureNow.Format(time.RFC3339), "amount": map[string]any{"total": 100, "payer_total": 60, "currency": "CNY"}}
}
func refundFact(state string) map[string]any {
	return map[string]any{"channel": "ORIGINAL", "out_trade_no": orderNo, "transaction_id": "WXTRADE", "out_refund_no": refundNo, "refund_id": "WXREFUND", "status": state, "create_time": fixtureNow.Add(-24 * time.Hour).Format(time.RFC3339), "success_time": fixtureNow.Format(time.RFC3339), "amount": map[string]any{"total": 100, "refund": 50, "payer_total": 60, "payer_refund": 30, "currency": "CNY"}}
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var f *fault.Error
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("want %s got %v", code, err)
	}
}
func encryptedNotify(t *testing.T, cfg Config, event string, fact any) ports.NotifyInput {
	t.Helper()
	block, err := aes.NewCipher(cfg.APIV3Key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := "0123456789ab"
	aad := "associated"
	original := "transaction"
	if strings.HasPrefix(event, "REFUND.") {
		original = "refund"
	}
	encrypted := gcm.Seal(nil, []byte(nonce), rawJSON(t, fact), []byte(aad))
	raw := rawJSON(t, map[string]any{"event_type": event, "resource_type": "encrypt-resource", "resource": map[string]any{"original_type": original, "algorithm": "AEAD_AES_256_GCM", "nonce": nonce, "associated_data": aad, "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
	response := signedResponse(t, http.StatusOK, raw)
	headers := map[string]string{}
	for key := range response.Header {
		headers[key] = response.Header.Get(key)
	}
	return ports.NotifyInput{Header: headers, Body: raw}
}
func TestNativeActionsSignAndVerifyExactBytes(t *testing.T) {
	cfg := testConfig(t)
	calls := 0
	cfg.HTTPClient.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		body := checkRequestSignature(t, req)
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/v3/pay/transactions/native":
			var in map[string]any
			if json.Unmarshal(body, &in) != nil {
				t.Fatal("bad body")
			}
			if in["appid"] != "testapp" || in["notify_url"] != cfg.NotifyURL || in["time_expire"] != fixtureNow.Add(15*time.Minute).Format(time.RFC3339) {
				t.Fatal("wrong frozen input")
			}
			return signedResponse(t, 200, []byte(`{"code_url":"weixin://wxpay/bizpayurl?pr=offline"}`)), nil
		case strings.HasSuffix(req.URL.Path, "/close"):
			return signedResponse(t, 204, nil), nil
		case req.URL.Path == "/v3/refund/domestic/refunds":
			return signedResponse(t, 200, rawJSON(t, refundFact("PROCESSING"))), nil
		case strings.Contains(req.URL.Path, "/v3/refund/domestic/refunds/"):
			return signedResponse(t, 200, rawJSON(t, refundFact("SUCCESS"))), nil
		default:
			if req.URL.Query().Get("mchid") != "testmerchant" {
				t.Fatal("missing merchant query")
			}
			return signedResponse(t, 200, rawJSON(t, tradeFact())), nil
		}
	})
	c := newClient(t, cfg)
	ctx := context.Background()
	if code, err := c.Create(ctx, ports.ChannelOrder{OrderNo: orderNo, AmountFen: 100, Description: "积分充值", ExpiresAt: fixtureNow.Add(15 * time.Minute)}); err != nil || !strings.HasPrefix(code, "weixin://") {
		t.Fatalf("create %v", err)
	}
	if trade, err := c.Query(ctx, orderNo); err != nil || trade.State != "paid" || trade.AmountFen != 100 || trade.PaidAt == nil {
		t.Fatalf("query %+v %v", trade, err)
	}
	if err := c.Close(ctx, orderNo); err != nil {
		t.Fatal(err)
	}
	if refund, err := c.Refund(ctx, ports.ChannelRefund{RefundNo: refundNo, OrderNo: orderNo, TradeNo: "WXTRADE", AmountFen: 50, TotalFen: 100}); err != nil || refund.State != "pending" {
		t.Fatalf("refund %+v %v", refund, err)
	}
	if refund, err := c.QueryRefund(ctx, refundNo, orderNo); err != nil || refund.State != "succeeded" || refund.BillAt == nil || !refund.BillAt.Equal(fixtureNow.Add(-24*time.Hour)) {
		t.Fatalf("query refund %+v %v", refund, err)
	}
	if calls != 5 {
		t.Fatalf("requests %d", calls)
	}
}
func TestResponseSignatureFailuresNeverConsumeTrade(t *testing.T) {
	tests := []struct {
		name   string
		change func(*http.Response)
	}{
		{"tampered", func(r *http.Response) { r.Body = io.NopCloser(strings.NewReader(`{"appid":"other"}`)) }},
		{"unknown serial", func(r *http.Response) { r.Header.Set("Wechatpay-Serial", "UNKNOWN") }},
		{"expired", func(r *http.Response) {
			r.Header.Set("Wechatpay-Timestamp", strconv.FormatInt(fixtureNow.Add(-6*time.Minute).Unix(), 10))
		}},
		{"unsigned", func(r *http.Response) { r.Header = http.Header{} }},
		{"signature probe", func(r *http.Response) { r.Header.Set("Wechatpay-Signature", "WECHATPAY/SIGNTEST/invalid") }},
		{"duplicate header", func(r *http.Response) { r.Header.Add("Wechatpay-Nonce", "another") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				r := signedResponse(t, 200, rawJSON(t, tradeFact()))
				test.change(r)
				return r, nil
			})
			_, err := newClient(t, cfg).Query(context.Background(), orderNo)
			requireCode(t, err, "PAYMENT_SIGNATURE_INVALID")
		})
	}
	cfg := testConfig(t)
	cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		r := signedResponse(t, 204, nil)
		r.Header.Del("Wechatpay-Signature")
		return r, nil
	})
	requireCode(t, newClient(t, cfg).Close(context.Background(), orderNo), "PAYMENT_SIGNATURE_INVALID")
}
func TestTradeProtocolAndIdentityBoundaries(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(map[string]any)
	}{
		{"merchant", "PAYMENT_IDENTITY_MISMATCH", func(m map[string]any) { m["mchid"] = "private-wrong-id" }},
		{"app", "PAYMENT_IDENTITY_MISMATCH", func(m map[string]any) { m["appid"] = "wrong" }},
		{"order", "PAYMENT_IDENTITY_MISMATCH", func(m map[string]any) { m["out_trade_no"] = "other" }},
		{"unknown state", "PAYMENT_CHANNEL_FAILED", func(m map[string]any) { m["trade_state"] = "REFUND" }},
		{"currency", "PAYMENT_INVALID_AMOUNT", func(m map[string]any) { m["amount"].(map[string]any)["currency"] = "USD" }},
		{"fraction", "PAYMENT_CHANNEL_FAILED", func(m map[string]any) { m["amount"].(map[string]any)["total"] = 1.5 }},
		{"overflow", "PAYMENT_INVALID_AMOUNT", func(m map[string]any) { m["amount"].(map[string]any)["total"] = int64(9007199254740992) }},
		{"missing paid time", "PAYMENT_CHANNEL_FAILED", func(m map[string]any) { delete(m, "success_time") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				fact := tradeFact()
				test.mutate(fact)
				return signedResponse(t, 200, rawJSON(t, fact)), nil
			})
			_, err := newClient(t, cfg).Query(context.Background(), orderNo)
			requireCode(t, err, test.code)
			if strings.Contains(err.Error(), "private-wrong-id") {
				t.Fatal("identity leaked")
			}
		})
	}
}
func TestNotificationDecryptionAndKeyRotation(t *testing.T) {
	cfg := testConfig(t)
	certDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(12345), NotBefore: fixtureNow.Add(-time.Hour), NotAfter: fixtureNow.Add(time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(12345)}, &merchantKey.PublicKey, merchantKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}
	cfg.VerifyKeys["3039"] = cert.PublicKey.(*rsa.PublicKey)
	c := newClient(t, cfg)
	in := encryptedNotify(t, cfg, "TRANSACTION.SUCCESS", tradeFact())
	notify, ack, err := c.ParseNotify(context.Background(), in)
	if err != nil || ack.Status != 204 || notify.Trade.AmountFen != 100 || notify.Trade.State != "paid" {
		t.Fatalf("notice %+v %v", notify, err)
	}
	old := in
	old.Header = map[string]string{}
	for k, v := range in.Header {
		old.Header[k] = v
	}
	old.Header["Wechatpay-Serial"] = "3039"
	digest := sha256.Sum256([]byte(old.Header["Wechatpay-Timestamp"] + "\n" + old.Header["Wechatpay-Nonce"] + "\n" + string(old.Body) + "\n"))
	sig, err := rsa.SignPKCS1v15(rand.Reader, merchantKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	old.Header["Wechatpay-Signature"] = base64.StdEncoding.EncodeToString(sig)
	if _, _, err = c.ParseNotify(context.Background(), old); err != nil {
		t.Fatal("old configured certificate did not verify")
	}
	// 公钥对应关系必须精确；不能在NEW签名失败后尝试旧证书。
	old.Header["Wechatpay-Serial"] = "PUB_KEY_ID_NEW"
	_, ack, err = c.ParseNotify(context.Background(), old)
	requireCode(t, err, "PAYMENT_SIGNATURE_INVALID")
	if ack.Status < 400 {
		t.Fatal("failure ack succeeded")
	}
	badKey := cfg
	badKey.APIV3Key = bytes.Repeat([]byte{7}, 32)
	_, _, err = newClient(t, badKey).ParseNotify(context.Background(), in)
	requireCode(t, err, "PAYMENT_SIGNATURE_INVALID")
}

func TestConfigRequiresExplicitCurrentVerificationSerial(t *testing.T) {
	for _, serial := range []string{"", "UNKNOWN"} {
		cfg := testConfig(t)
		cfg.VerifySerial = serial
		_, err := New(cfg)
		requireCode(t, err, "PAYMENT_CONFIG_INVALID")
	}
	cfg := testConfig(t)
	cfg.VerifyKeys["OLD"] = &merchantKey.PublicKey
	cfg.VerifySerial = "OLD"
	cfg.HTTPClient.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Wechatpay-Serial") != "OLD" {
			t.Fatal("request serial came from map order")
		}
		return signedResponse(t, 200, rawJSON(t, tradeFact())), nil
	})
	if _, err := newClient(t, cfg).Query(context.Background(), orderNo); err != nil {
		t.Fatal(err)
	}
}
func TestDuplicateJSONKeysAndMalformedAmountsAreRejected(t *testing.T) {
	cfg := testConfig(t)
	for _, body := range []string{`{"code_url":"weixin://wxpay/bizpayurl?pr=ok","code_url":"weixin://wxpay/bizpayurl?pr=bad"}`, `{"code_url":null}`, `{"code_url":"https://private.invalid"}`} {
		cfg.HTTPClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return signedResponse(t, 200, []byte(body)), nil })
		_, err := newClient(t, cfg).Create(context.Background(), ports.ChannelOrder{OrderNo: orderNo, AmountFen: 100, Description: "积分", ExpiresAt: fixtureNow.Add(time.Minute)})
		requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	}
}

func TestNotificationRejectsSignedButInvalidProtocolFacts(t *testing.T) {
	for _, test := range []struct {
		name, code string
		change     func(map[string]any)
	}{
		{name: "wrong merchant", code: "PAYMENT_IDENTITY_MISMATCH", change: func(f map[string]any) { f["mchid"] = "wrong" }},
		{name: "wrong app", code: "PAYMENT_IDENTITY_MISMATCH", change: func(f map[string]any) { f["appid"] = "wrong" }},
		{name: "invalid amount", code: "PAYMENT_INVALID_AMOUNT", change: func(f map[string]any) { f["amount"].(map[string]any)["total"] = -1 }},
		{name: "not successful", code: "PAYMENT_CHANNEL_FAILED", change: func(f map[string]any) { f["trade_state"] = "NOTPAY" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig(t)
			fact := tradeFact()
			test.change(fact)
			_, ack, err := newClient(t, cfg).ParseNotify(context.Background(), encryptedNotify(t, cfg, "TRANSACTION.SUCCESS", fact))
			requireCode(t, err, test.code)
			if ack.Status < 400 {
				t.Fatal("invalid signed fact got success suggestion")
			}
		})
	}
	for _, mode := range []string{"algorithm", "nonce", "resource", "ciphertext"} {
		t.Run(mode, func(t *testing.T) {
			cfg := testConfig(t)
			in := encryptedNotify(t, cfg, "TRANSACTION.SUCCESS", tradeFact())
			var body map[string]any
			if json.Unmarshal(in.Body, &body) != nil {
				t.Fatal("bad fixture")
			}
			resource := body["resource"].(map[string]any)
			switch mode {
			case "algorithm":
				resource["algorithm"] = "unknown"
			case "nonce":
				resource["nonce"] = "short"
			case "resource":
				body["resource_type"] = "plain"
			case "ciphertext":
				resource["ciphertext"] = "not base64"
			}
			in.Body = rawJSON(t, body)
			response := signedResponse(t, 200, in.Body)
			for key := range response.Header {
				in.Header[key] = response.Header.Get(key)
			}
			_, _, err := newClient(t, cfg).ParseNotify(context.Background(), in)
			requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
		})
	}
	cfg := testConfig(t)
	in := encryptedNotify(t, cfg, "TRANSACTION.SUCCESS", tradeFact())
	cfg.NotifyMaxBytes = 10
	_, _, err := newClient(t, cfg).ParseNotify(context.Background(), in)
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
}
