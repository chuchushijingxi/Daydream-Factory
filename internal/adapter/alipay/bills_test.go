package alipay

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const billHeader = "商户订单号,支付宝交易号,业务类型,订单金额（元）,商家实收（元）,退款批次号\n"
const billPay = orderNo + "," + tradeNo + ",交易,2.00,0.01,\n"
const billRefund = orderNo + "," + tradeNo + ",退款,-1.20,-0.01," + refundNo + "\n"

type zipEntry struct {
	name string
	body []byte
	mode os.FileMode
}

func zipBytes(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		if entry.mode != 0 {
			h.SetMode(entry.mode)
		}
		out, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = out.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func basicZIP(t *testing.T) []byte {
	return zipBytes(t, zipEntry{name: "交易明细.csv", body: []byte(billHeader + billPay + billRefund)})
}
func TestBillSignedApplicationAndUnsignedSafeDownload(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("# 支付宝自造账单\n" + billHeader + billPay + billRefund + "# 结束\n"))
	if err != nil {
		t.Fatal(err)
	}
	data := zipBytes(t, zipEntry{name: "业务明细.csv", body: gbk}, zipEntry{name: "业务汇总.csv", body: []byte("总笔数,总金额（元）\n2,3.20\n")})
	downloads := 0
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			method, biz := request(t, r)
			if method != "alipay.data.dataservice.bill.downloadurl.query" || stringValue(t, biz, "bill_type") != "trade" || stringValue(t, biz, "bill_date") != "2026-10-20" {
				t.Fatal("bill application")
			}
			return signedResponse(t, method, `{"code":"10000","bill_download_url":"http://dwbillcenter.alipay.com/downloadBillFile.resource?token=fake-token"}`), nil
		}
		downloads++
		if r.URL.Scheme != "https" || r.URL.Host != "dwbillcenter.alipay.com" || r.Header.Get("Authorization") != "" || r.URL.Query().Get("sign") != "" || r.Body != nil {
			t.Fatal("download leaked credentials or used HTTP")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	}))
	rows, err := c.Bill(context.Background(), testNow)
	if err != nil || len(rows) != 2 || downloads != 1 {
		t.Fatalf("%+v %v", rows, err)
	}
	if rows[0].Kind != "pay" || rows[0].AmountFen != 200 || rows[1].Kind != "refund" || rows[1].AmountFen != 120 || rows[1].RefundNo != refundNo || rows[1].State != "succeeded" {
		t.Fatal(rows)
	}
}
func TestBillUTF8BOMAndHeaderMapping(t *testing.T) {
	c := newChannel(t, nil)
	body := "\ufeff# fixture\n退款批次号,业务类型,订单金额(元),支付宝交易号,商户订单号,商家实收(元)\n,支付,2," + tradeNo + "," + orderNo + ",0.01\n" + refundNo + ",退款,-1.2," + tradeNo + "," + orderNo + ",-0.01\n"
	rows, err := c.parseZIP(zipBytes(t, zipEntry{name: "detail.csv", body: []byte(body)}))
	if err != nil || len(rows) != 2 || rows[0].AmountFen != 200 || rows[1].AmountFen != 120 {
		t.Fatalf("%+v %v", rows, err)
	}
}
func TestBillURLAllowlist(t *testing.T) {
	good := []string{"https://dwbillcenter.alipay.com/downloadBillFile.resource?token=fake", "https://dwbillcenter.alipay.com:443/downloadBillFile.resource", "http://dwbillcenter.alipay.com/downloadBillFile.resource?token=fake"}
	for _, raw := range good {
		got, err := billURL(raw)
		if err != nil || !strings.HasPrefix(got, "https://") {
			t.Fatalf("%q %v", got, err)
		}
	}
	bad := []string{"https://dwbillcenter.alipay.com.evil.invalid/downloadBillFile.resource", "https://evil.invalid@dwbillcenter.alipay.com/downloadBillFile.resource", "https://dwbillcenter.alipay.com@evil.invalid/downloadBillFile.resource", "https://127.0.0.1/downloadBillFile.resource", "https://localhost/downloadBillFile.resource", "https://dwbillcenter.alipay.com:444/downloadBillFile.resource", "https://dwbillcenter.alipay.com:/downloadBillFile.resource", "https://dwbillcenter.alipay.com/downloadBillFile.resource#token", "https://dwbillcenter.alipay.com/downloadBillFile.resource#", "http://dwbillcenter.alipay.com/other", "http://dwbillcenter.alipay.com:80/downloadBillFile.resource", "ftp://dwbillcenter.alipay.com/downloadBillFile.resource", "https://dwbillcenter.alipay.com/%64ownloadBillFile.resource"}
	for _, raw := range bad {
		t.Run(raw, func(t *testing.T) {
			_, err := billURL(raw)
			if err == nil || strings.Contains(err.Error(), raw) {
				t.Fatal(err)
			}
		})
	}
}
func TestBillRedirectDoesNotFollowOrLeak(t *testing.T) {
	calls := 0
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method == "POST" {
			method, _ := request(t, r)
			return signedResponse(t, method, `{"code":"10000","bill_download_url":"https://dwbillcenter.alipay.com/downloadBillFile.resource?token=fake"}`), nil
		}
		if r.URL.Host != "dwbillcenter.alipay.com" {
			t.Fatal("followed redirect")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://evil.invalid/token"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	rows, err := c.Bill(context.Background(), testNow)
	if err == nil || rows != nil || calls != 2 {
		t.Fatalf("%+v %v calls=%d", rows, err, calls)
	}
}
func TestBillZIPRejectsUnsafeEntriesAndCRC(t *testing.T) {
	c := newChannel(t, nil)
	cases := []struct {
		name string
		zip  []byte
	}{
		{"traversal", zipBytes(t, zipEntry{name: "../detail.csv", body: []byte(billHeader + billPay)})},
		{"absolute", zipBytes(t, zipEntry{name: "/detail.csv", body: []byte(billHeader + billPay)})},
		{"windows path", zipBytes(t, zipEntry{name: "..\\detail.csv", body: []byte(billHeader + billPay)})},
		{"symlink", zipBytes(t, zipEntry{name: "detail.csv", body: []byte("target.csv"), mode: os.ModeSymlink | 0777})},
		{"nested archive", zipBytes(t, zipEntry{name: "nested.zip", body: basicZIP(t)})},
		{"directory", zipBytes(t, zipEntry{name: "directory/", mode: os.ModeDir | 0755})},
		{"duplicate name", zipBytes(t, zipEntry{name: "detail.csv", body: []byte(billHeader)}, zipEntry{name: "detail.csv", body: []byte(billHeader)})},
		{"only summary", zipBytes(t, zipEntry{name: "汇总.csv", body: []byte("总笔数,总金额（元）\n0,0\n")})},
		{"invalid zip", []byte("invalid")},
		{"invalid encoding", zipBytes(t, zipEntry{name: "detail.csv", body: []byte{0x81}})},
	}
	corrupt := basicZIP(t)
	at := bytes.Index(corrupt, []byte(orderNo))
	if at < 0 {
		t.Fatal("fixture")
	}
	corrupt[at] = 'X'
	cases = append(cases, struct {
		name string
		zip  []byte
	}{"bad CRC", corrupt})
	encrypted := append([]byte(nil), basicZIP(t)...)
	central := bytes.Index(encrypted, []byte{'P', 'K', 1, 2})
	if central < 0 {
		t.Fatal("fixture")
	}
	flags := binary.LittleEndian.Uint16(encrypted[central+8:])
	binary.LittleEndian.PutUint16(encrypted[central+8:], flags|1)
	cases = append(cases, struct {
		name string
		zip  []byte
	}{"encrypted", encrypted})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := c.parseZIP(tc.zip)
			if err == nil || rows != nil {
				t.Fatalf("%+v %v", rows, err)
			}
		})
	}
}
func TestBillAllLimitsFailWholeBill(t *testing.T) {
	body := []byte(billHeader + billPay + billRefund)
	for _, tc := range []struct {
		name   string
		change func(*Config)
		data   []byte
	}{
		{"entry count", func(c *Config) { c.BillMaxEntries = 1 }, zipBytes(t, zipEntry{name: "one.csv", body: body}, zipEntry{name: "two.csv", body: body})},
		{"CSV bytes", func(c *Config) { c.BillCSVMaxBytes = int64(len(body) - 1) }, basicZIP(t)},
		{"decompressed total", func(c *Config) { c.BillUncompressedMaxBytes = int64(len(body)*2 - 1) }, zipBytes(t, zipEntry{name: "one.csv", body: body}, zipEntry{name: "two.csv", body: body})},
		{"row count", func(c *Config) { c.BillMaxRows = 2 }, basicZIP(t)},
		{"field bytes", func(c *Config) { c.BillMaxFieldBytes = 8 }, basicZIP(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config(t, nil)
			tc.change(&cfg)
			c, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := c.parseZIP(tc.data)
			if err == nil || rows != nil {
				t.Fatalf("%+v %v", rows, err)
			}
		})
	}
	t.Run("compressed bytes", func(t *testing.T) {
		data := basicZIP(t)
		cfg := config(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == "POST" {
				method, _ := request(t, r)
				return signedResponse(t, method, `{"code":"10000","bill_download_url":"https://dwbillcenter.alipay.com/downloadBillFile.resource"}`), nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
		}))
		cfg.BillCompressedMaxBytes = int64(len(data) - 1)
		c, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := c.Bill(context.Background(), testNow)
		if err == nil || rows != nil {
			t.Fatalf("%+v %v", rows, err)
		}
	})
	// The archive's claimed sizes cannot bypass the actual decompression/CRC checks.
	t.Run("forged decompressed size", func(t *testing.T) {
		data := basicZIP(t)
		central := bytes.Index(data, []byte{'P', 'K', 1, 2})
		binary.LittleEndian.PutUint32(data[central+24:], 1)
		c := newChannel(t, nil)
		rows, err := c.parseZIP(data)
		if err == nil || rows != nil {
			t.Fatalf("%+v %v", rows, err)
		}
	})
}
func TestBillCSVRejectsUnknownIncompleteOrDirtyRows(t *testing.T) {
	c := newChannel(t, nil)
	for _, tc := range []struct{ name, body string }{
		{"unknown business", billHeader + strings.Replace(billPay, ",交易,", ",未知,", 1)},
		{"negative pay", billHeader + strings.Replace(billPay, ",2.00,", ",-2.00,", 1)},
		{"exponent", billHeader + strings.Replace(billPay, ",2.00,", ",2e0,", 1)},
		{"extra precision", billHeader + strings.Replace(billPay, ",2.00,", ",2.001,", 1)},
		{"missing refund number", billHeader + strings.Replace(billRefund, refundNo, "", 1)},
		{"missing header", "商户订单号,支付宝交易号\n" + orderNo + "," + tradeNo + "\n"},
		{"duplicate header", billHeader + ",商户订单号\n"},
		{"malformed quotes", billHeader + `"broken`},
		{"partial data returns none", billHeader + billPay + "invalid,row\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := c.parseZIP(zipBytes(t, zipEntry{name: "detail.csv", body: []byte(tc.body)}))
			if err == nil || rows != nil {
				t.Fatalf("%+v %v", rows, err)
			}
		})
	}
}

func TestBillDuplicateDataValuesAreNotDuplicateHeaders(t *testing.T) {
	c := newChannel(t, nil)
	body := billHeader + strings.Replace(billPay, ",0.01,", ",2.00,", 1) + strings.Replace(billRefund, ",-0.01,", ",-1.20,", 1)
	rows, err := c.parseZIP(zipBytes(t, zipEntry{name: "detail.csv", body: []byte(body)}))
	if err != nil || len(rows) != 2 {
		t.Fatalf("%+v %v", rows, err)
	}
}
func TestBillForeignApplicationAndMerchantIdentity(t *testing.T) {
	c := newChannel(t, nil)
	for _, tc := range []struct {
		seller, app        string
		foreign, wantError bool
	}{{"test-seller", "test-app", false, false}, {"test-seller", "other-app", true, false}, {"other-seller", "test-app", false, true}} {
		body := strings.TrimSuffix(billHeader, "\n") + ",商户PID,应用ID\n" + strings.TrimSuffix(billPay, "\n") + "," + tc.seller + "," + tc.app + "\n"
		rows, err := c.parseZIP(zipBytes(t, zipEntry{name: "detail.csv", body: []byte(body)}))
		if tc.wantError {
			if codeOf(err) != "PAYMENT_IDENTITY_MISMATCH" || rows != nil {
				t.Fatalf("%+v %v", rows, err)
			}
		} else if err != nil || len(rows) != 1 || rows[0].ForeignApplication != tc.foreign {
			t.Fatalf("%+v %v", rows, err)
		}
	}
}
func TestBillSummaryRequiresAHeaderAndEmptyDetailIsExplicit(t *testing.T) {
	c := newChannel(t, nil)
	rows, err := c.parseZIP(zipBytes(t, zipEntry{name: "detail.csv", body: []byte(billHeader)}))
	if err != nil || len(rows) != 0 {
		t.Fatalf("%+v %v", rows, err)
	}
	rows, err = c.parseZIP(zipBytes(t, zipEntry{name: "detail.csv", body: []byte(billHeader)}, zipEntry{name: "汇总.csv", body: []byte("arbitrary-content\n")}))
	if err == nil || rows != nil {
		t.Fatalf("%+v %v", rows, err)
	}
}
