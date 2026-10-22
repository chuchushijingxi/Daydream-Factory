package wechatpay

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/csv"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
)

func billFixture(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	records := [][]string{
		{"公众账号ID", "交易状态", "商户号", "商户订单号", "微信订单号", "货币种类", "订单金额", "应结订单金额", "申请退款金额", "退款金额", "商户退款单号", "退款状态", "商品名称"},
		{"`testapp", "`SUCCESS", "`testmerchant", "`" + orderNo, "`WXTRADE", "`CNY", "`1.00", "`0.60", "`0.00", "`0.00", "`0", "`", "`优惠商品"},
		{"`testapp", "`REFUND", "`testmerchant", "``" + orderNo, "`WXTRADE", "`CNY", "`0.00", "`0.00", "`0.50", "`0.30", "`" + refundNo, "`PROCESSING", "`优惠商品"},
		{"总交易单数", "应结订单总金额", "退款总金额", "充值券退款总金额", "手续费总金额", "订单总金额", "申请退款总金额"},
		{"`2", "`0.60", "`0.30", "`0.00", "`0.01", "`1.00", "`0.50"},
	}
	// 每格仅剥离一个官方格式前缀；正常订单号只保留一个前缀。
	records[2][3] = "`" + orderNo
	for _, r := range records {
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	if w.Error() != nil {
		t.Fatal(w.Error())
	}
	return b.Bytes()
}
func gzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func billTransport(t *testing.T, raw, download []byte, mutate func(map[string]any), downloadStatus int) roundTrip {
	return roundTrip(func(req *http.Request) (*http.Response, error) {
		checkRequestSignature(t, req)
		if req.URL.Path == "/v3/bill/tradebill" {
			if req.URL.Query().Get("bill_type") != "ALL" || req.URL.Query().Get("tar_type") != "GZIP" {
				t.Fatal("wrong bill request")
			}
			digest := sha1.Sum(raw)
			metadata := map[string]any{"hash_type": "SHA1", "hash_value": hex.EncodeToString(digest[:]), "download_url": endpoint + "/v3/billdownload/file?token=private-download-token"}
			if mutate != nil {
				mutate(metadata)
			}
			return signedResponse(t, 200, rawJSON(t, metadata)), nil
		}
		if req.URL.Path != "/v3/billdownload/file" {
			t.Fatal("unexpected download destination")
		}
		// 官方下载体没有Wechatpay-Signature，只有申请接口返回的可信hash。
		return &http.Response{StatusCode: downloadStatus, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(download))}, nil
	})
}
func TestBillGzipHashAndAmountsUseOrderTotalNotSettlement(t *testing.T) {
	cfg := testConfig(t)
	raw := billFixture(t)
	cfg.HTTPClient.Transport = billTransport(t, raw, gzipBytes(t, raw), nil, 200)
	rows, err := newClient(t, cfg).Bill(context.Background(), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].AmountFen != 100 || rows[0].Kind != "pay" || rows[1].AmountFen != 50 || rows[1].State != "pending" || rows[1].RefundNo != refundNo {
		t.Fatalf("wrong bill projection %+v", rows)
	}
	// 同商户其他应用仍保留为外应用差异，不使整张账单失败。
	mixed := bytes.Replace(raw, []byte("`testapp"), []byte("`another-app"), 1)
	cfg.HTTPClient.Transport = billTransport(t, mixed, mixed, nil, 200)
	rows, err = newClient(t, cfg).Bill(context.Background(), fixtureNow)
	if err != nil || !rows[0].ForeignApplication || rows[1].ForeignApplication {
		t.Fatalf("mixed app bill %+v %v", rows, err)
	}
}
func TestBillFailuresNeverReturnPartialOrEmptySuccess(t *testing.T) {
	tests := []struct {
		name     string
		change   func(*Config, *[]byte, *[]byte)
		metadata func(map[string]any)
		code     string
	}{
		{name: "bad digest", metadata: func(m map[string]any) { m["hash_value"] = strings.Repeat("0", 40) }, code: "PAYMENT_SIGNATURE_INVALID"},
		{name: "unknown hash", metadata: func(m map[string]any) { m["hash_type"] = "MD5" }},
		{name: "compressed limit", change: func(c *Config, _, _ *[]byte) { c.MaxBillCompressedBytes = 8 }},
		{name: "decompressed limit", change: func(c *Config, _, _ *[]byte) { c.MaxBillDecompressedBytes = 8 }},
		{name: "CSV limit", change: func(c *Config, _, _ *[]byte) { c.MaxBillCSVBytes = 8 }},
		{name: "row limit", change: func(c *Config, _, _ *[]byte) { c.MaxBillRows = 1 }},
		{name: "field limit", change: func(c *Config, _, _ *[]byte) { c.MaxBillFieldBytes = 4 }},
		{name: "gzip corruption", change: func(_ *Config, _ *[]byte, wire *[]byte) { (*wire)[len(*wire)-5] ^= 0x40 }},
		{name: "merchant mismatch", change: func(_ *Config, raw, wire *[]byte) {
			*raw = bytes.ReplaceAll(*raw, []byte("`testmerchant"), []byte("`other"))
			*wire = *raw
		}, code: "PAYMENT_IDENTITY_MISMATCH"},
		{name: "unknown trade state", change: func(_ *Config, raw, wire *[]byte) {
			*raw = bytes.Replace(*raw, []byte("`SUCCESS"), []byte("`MYSTERY"), 1)
			*wire = *raw
		}},
		{name: "count mismatch", change: func(_ *Config, raw, wire *[]byte) {
			*raw = bytes.Replace(*raw, []byte("`2,"), []byte("`3,"), 1)
			*wire = *raw
		}},
		{name: "truncated summary", change: func(_ *Config, raw, wire *[]byte) {
			*raw = (*raw)[:bytes.Index(*raw, []byte("总交易单数"))]
			*wire = *raw
		}},
		{name: "not CSV zip", change: func(_ *Config, raw, wire *[]byte) { *raw = []byte("PK\x03\x04not-a-wechat-csv"); *wire = *raw }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig(t)
			raw := billFixture(t)
			wire := gzipBytes(t, raw)
			if test.change != nil {
				test.change(&cfg, &raw, &wire)
			}
			cfg.HTTPClient.Transport = billTransport(t, raw, wire, test.metadata, 200)
			rows, err := newClient(t, cfg).Bill(context.Background(), fixtureNow)
			code := test.code
			if code == "" {
				code = "PAYMENT_CHANNEL_FAILED"
			}
			requireCode(t, err, code)
			if rows != nil {
				t.Fatal("failed bill leaked partial success")
			}
		})
	}
}
func TestBillDownloadRejectsSSRFAndRedirects(t *testing.T) {
	for _, address := range []string{"http://api.mch.weixin.qq.com/file", "https://api.mch.weixin.qq.com.evil.invalid/file", "https://evil.invalid/file", "https://127.0.0.1/file", "https://api.mch.weixin.qq.com:8443/file", "https://user:private@api.mch.weixin.qq.com/file", "https://api.mch.weixin.qq.com/file#private"} {
		t.Run(address, func(t *testing.T) {
			cfg := testConfig(t)
			calls := 0
			cfg.HTTPClient.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					t.Fatal("unsafe download requested")
				}
				digest := sha1.Sum([]byte("x"))
				return signedResponse(t, 200, rawJSON(t, map[string]any{"download_url": address, "hash_type": "SHA1", "hash_value": hex.EncodeToString(digest[:])})), nil
			})
			_, err := newClient(t, cfg).Bill(context.Background(), fixtureNow)
			requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
			if calls != 1 || strings.Contains(err.Error(), "private") {
				t.Fatal("unsafe URL escaped validation or leaked")
			}
		})
	}
	cfg := testConfig(t)
	raw := billFixture(t)
	calls := 0
	cfg.HTTPClient.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return billTransport(t, raw, raw, nil, 200)(req)
		}
		if calls > 2 {
			t.Fatal("redirect followed")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.invalid/secret"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	})
	_, err := newClient(t, cfg).Bill(context.Background(), fixtureNow)
	requireCode(t, err, "PAYMENT_CHANNEL_FAILED")
	if calls != 2 {
		t.Fatalf("requests %d", calls)
	}
}
func TestOfficialEscapedCustomFieldDoesNotBreakCSV(t *testing.T) {
	cfg := testConfig(t)
	raw := billFixture(t)
	raw = bytes.ReplaceAll(raw, []byte("`优惠商品"), []byte("`商品\\\"促销"))
	rows, err := newClient(t, cfg).parseBill(context.Background(), raw)
	if err != nil || len(rows) != 2 {
		t.Fatalf("official literal quotes %+v %v", rows, err)
	}
}
