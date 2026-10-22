package wechatpay

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/csv"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
	p "drama-agent/internal/domain/payment"
)

func (c *Client) Bill(ctx context.Context, date time.Time) ([]ports.BillRow, error) {
	query := url.Values{"bill_date": {date.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")}, "bill_type": {"ALL"}, "tar_type": {"GZIP"}}
	var metadata struct {
		DownloadURL string `json:"download_url"`
		HashType    string `json:"hash_type"`
		HashValue   string `json:"hash_value"`
	}
	if err := c.api(ctx, http.MethodGet, "/v3/bill/tradebill?"+query.Encode(), nil, &metadata); err != nil {
		return nil, err
	}
	if !strings.EqualFold(metadata.HashType, "SHA1") {
		return nil, protocol()
	}
	expected, err := hex.DecodeString(metadata.HashValue)
	if err != nil || len(expected) != sha1.Size {
		return nil, protocol()
	}
	download, err := safeDownloadURL(metadata.DownloadURL)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	resp, err := c.request(callCtx, http.MethodGet, download.String(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// 下载响应没有平台签名头，信任已验签申请中的摘要；不走普通JSON应答验签。
	if resp.StatusCode != http.StatusOK {
		return nil, transportError()
	}
	compressed, err := readLimited(resp.Body, c.cfg.MaxBillCompressedBytes)
	if err != nil {
		return nil, err
	}
	raw := compressed
	if len(compressed) >= 2 && compressed[0] == 0x1f && compressed[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, protocol()
		}
		raw, err = readLimited(zr, c.cfg.MaxBillDecompressedBytes)
		closeErr := zr.Close()
		if err != nil || closeErr != nil {
			return nil, protocol()
		}
	} else if int64(len(raw)) > c.cfg.MaxBillDecompressedBytes {
		return nil, protocol()
	}
	digest := sha1.Sum(raw)
	if subtle.ConstantTimeCompare(expected, digest[:]) != 1 {
		return nil, signatureError()
	}
	if int64(len(raw)) > c.cfg.MaxBillCSVBytes {
		return nil, protocol()
	}
	return c.parseBill(ctx, raw)
}
func (c *Client) parseBill(ctx context.Context, raw []byte) ([]ports.BillRow, error) {
	if !utf8.Valid(raw) {
		return nil, protocol()
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff")))
	reader.FieldsPerRecord = -1
	// 官方自定义字段以反斜杠转义引号而非RFC4180双引号，允许字段内的字面引号。
	reader.LazyQuotes = true
	header, err := reader.Read()
	if err != nil {
		return nil, protocol()
	}
	columns := map[string]int{}
	for i, name := range header {
		if len(name) > c.cfg.MaxBillFieldBytes {
			return nil, protocol()
		}
		name = strings.ReplaceAll(strings.TrimSpace(name), " ", "")
		if _, exists := columns[name]; exists {
			return nil, protocol()
		}
		columns[name] = i
	}
	for _, name := range []string{"公众账号ID", "商户号", "货币种类", "商户订单号", "微信订单号", "交易状态", "订单金额", "商户退款单号", "申请退款金额", "退款状态"} {
		if _, ok := columns[name]; !ok {
			return nil, protocol()
		}
	}
	rows := []ports.BillRow{}
	summarized := false
	count := 0
	for {
		if ctx.Err() != nil {
			return nil, transportError()
		}
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, protocol()
		}
		for _, field := range record {
			if len(field) > c.cfg.MaxBillFieldBytes {
				return nil, protocol()
			}
		}
		if len(record) > 0 && strings.TrimPrefix(record[0], "`") == "总交易单数" {
			if summarized {
				return nil, protocol()
			}
			summary, err := reader.Read()
			if err != nil || len(summary) != len(record) {
				return nil, protocol()
			}
			for _, field := range summary {
				if len(field) > c.cfg.MaxBillFieldBytes {
					return nil, protocol()
				}
			}
			total, err := strconv.Atoi(strings.TrimPrefix(summary[0], "`"))
			if err != nil || total != count {
				return nil, protocol()
			}
			if _, err = reader.Read(); err != io.EOF {
				return nil, protocol()
			}
			summarized = true
			break
		}
		if len(record) != len(header) || len(rows) >= c.cfg.MaxBillRows {
			return nil, protocol()
		}
		count++
		get := func(name string) string { return strings.TrimPrefix(record[columns[name]], "`") }
		if get("商户号") != c.cfg.MchID || !identifier(get("公众账号ID"), 128) {
			return nil, identityError()
		}
		if get("货币种类") != "CNY" {
			return nil, amountError()
		}
		no, trade := get("商户订单号"), get("微信订单号")
		if !identifier(no, 32) || !identifier(trade, 64) {
			return nil, identityError()
		}
		row := ports.BillRow{OrderNo: no, TradeNo: trade, ForeignApplication: get("公众账号ID") != c.cfg.AppID}
		switch get("交易状态") {
		case "SUCCESS":
			row.Kind = "pay"
			row.State = "paid"
			row.AmountFen, err = p.ParseAmount(get("订单金额"))
		case "REFUND":
			row.Kind = "refund"
			row.RefundNo = get("商户退款单号")
			if !identifier(row.RefundNo, 64) {
				return nil, identityError()
			}
			row.AmountFen, err = p.ParseAmount(get("申请退款金额"))
			switch get("退款状态") {
			case "SUCCESS":
				row.State = "succeeded"
			case "PROCESSING", "CHANGE":
				row.State = "pending"
			case "FAIL":
				row.State = "failed"
			default:
				return nil, protocol()
			}
		default:
			return nil, protocol()
		}
		if err != nil || row.AmountFen <= 0 {
			return nil, amountError()
		}
		rows = append(rows, row)
	}
	if !summarized {
		return nil, protocol()
	}
	return rows, nil
}
