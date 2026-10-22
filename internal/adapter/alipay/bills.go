package alipay

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func (c *Channel) Bill(ctx context.Context, date time.Time) ([]ports.BillRow, error) {
	if date.Year() < 1 || date.Year() > 9999 {
		return nil, protocol("bill_date")
	}
	out, err := c.call(ctx, "alipay.data.dataservice.bill.downloadurl.query", map[string]string{"bill_type": "trade", "bill_date": date.In(beijing).Format("2006-01-02")}, false)
	if err != nil {
		return nil, err
	}
	if out.Code != "10000" {
		return nil, businessError()
	}
	target, err := billURL(out.BillURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, protocol("bill_download_url")
	}
	// A separate unsigned GET prevents forwarding the gateway signature to the token URL.
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, channelError()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, channelError()
	}
	data, err := limitedRead(resp.Body, c.config.BillCompressedMaxBytes)
	if err != nil {
		return nil, err
	}
	return c.parseZIP(data)
}
func billURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Fragment != "" || (u.Host != "dwbillcenter.alipay.com" && u.Host != "dwbillcenter.alipay.com:443") || strings.Contains(raw, "#") || u.EscapedPath() != "/downloadBillFile.resource" {
		return "", protocol("bill_download_url")
	}
	// Legacy official examples use HTTP only on this exact endpoint. Never send HTTP.
	if u.Scheme == "http" && u.Port() == "" {
		u.Scheme = "https"
	}
	if u.Scheme != "https" {
		return "", protocol("bill_download_url")
	}
	return u.String(), nil
}
func decodeCSV(raw []byte) ([]byte, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if utf8.Valid(raw) {
		if bytes.Contains(raw, []byte("\ufffd")) {
			return nil, protocol("bill_encoding")
		}
		return raw, nil
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(raw)
	if err != nil || !utf8.Valid(decoded) || bytes.Contains(decoded, []byte("\ufffd")) {
		return nil, protocol("bill_encoding")
	}
	return decoded, nil
}
func (c *Channel) parseZIP(raw []byte) ([]ports.BillRow, error) {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(archive.File) == 0 || len(archive.File) > c.config.BillMaxEntries {
		return nil, protocol("bill_zip")
	}
	rows := []ports.BillRow{}
	var total int64
	detailSeen := false
	rowCount := 0
	names := map[string]bool{}
	for _, file := range archive.File {
		name, err := decodeCSV([]byte(file.Name))
		if err != nil {
			return nil, err
		}
		filename := string(name)
		if filename == "" || path.IsAbs(filename) || path.Clean(filename) != filename || strings.ContainsAny(filename, "/\\\x00") || !file.Mode().IsRegular() || file.Flags&1 != 0 || !strings.HasSuffix(strings.ToLower(filename), ".csv") || names[filename] {
			return nil, protocol("bill_entry")
		}
		names[filename] = true
		maximum := min(c.config.BillCSVMaxBytes, c.config.BillUncompressedMaxBytes-total)
		if maximum <= 0 || file.UncompressedSize64 > uint64(maximum) {
			return nil, protocol("bill_size")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, protocol("bill_zip")
		}
		data, readErr := limitedRead(reader, maximum)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return nil, protocol("bill_zip")
		}
		total += int64(len(data))
		decoded, err := decodeCSV(data)
		if err != nil {
			return nil, err
		}
		// Encoding conversion can expand GBK; bound the actual decoded CSV as well.
		if int64(len(decoded)) > c.config.BillCSVMaxBytes {
			return nil, protocol("bill_size")
		}
		parsed, detail, err := c.parseCSV(decoded, strings.Contains(filename, "汇总"), &rowCount)
		if err != nil {
			return nil, err
		}
		detailSeen = detailSeen || detail
		rows = append(rows, parsed...)
	}
	if !detailSeen {
		return nil, protocol("bill_detail")
	}
	return rows, nil
}
func cell(raw string) string { return strings.TrimPrefix(strings.TrimSpace(raw), "`") }
func header(raw string) string {
	return strings.NewReplacer("（", "(", "）", ")", " ", "", "\t", "").Replace(cell(raw))
}
func (c *Channel) parseCSV(raw []byte, summaryFile bool, count *int) ([]ports.BillRow, bool, error) {
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = -1
	rows := []ports.BillRow{}
	columns := map[string]int{}
	detail := false
	width := 0
	summary := false
	summarySeen := false
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, protocol("bill_csv")
		}
		*count++
		if *count > c.config.BillMaxRows {
			return nil, false, protocol("bill_rows")
		}
		for _, v := range record {
			if len(v) > c.config.BillMaxFieldBytes {
				return nil, false, protocol("bill_field")
			}
		}
		if len(record) == 0 {
			continue
		}
		blank := true
		for _, value := range record {
			blank = blank && cell(value) == ""
		}
		first := cell(record[0])
		if blank || strings.HasPrefix(first, "#") {
			continue
		}
		candidate := map[string]int{}
		duplicate := false
		for i, v := range record {
			normalized := header(v)
			if _, exists := candidate[normalized]; exists && normalized != "" {
				duplicate = true
			}
			candidate[normalized] = i
		}
		_, hasOrder := candidate["商户订单号"]
		_, hasTrade := candidate["支付宝交易号"]
		if hasOrder || hasTrade {
			if !hasOrder || !hasTrade || duplicate {
				return nil, false, protocol("bill_header")
			}
			for _, name := range []string{"业务类型", "订单金额(元)"} {
				if _, exists := candidate[name]; !exists {
					return nil, false, protocol("bill_header")
				}
			}
			if summaryFile {
				return nil, false, protocol("bill_summary")
			}
			columns = candidate
			width = len(record)
			detail = true
			summary = false
			continue
		}
		if _, ok := candidate["总笔数"]; ok {
			if duplicate {
				return nil, false, protocol("bill_header")
			}
			summary = true
			summarySeen = true
			continue
		}
		if summary {
			continue
		}
		if !detail {
			return nil, false, protocol("bill_header")
		}
		get := func(name string) string {
			i, ok := columns[name]
			if !ok || i >= len(record) {
				return ""
			}
			return cell(record[i])
		}
		if len(record) != width {
			return nil, false, protocol("bill_columns")
		}
		if value := get("商户PID"); value != "" && value != c.config.SellerID {
			return nil, false, identityError("seller_id")
		}
		row := ports.BillRow{OrderNo: get("商户订单号"), TradeNo: get("支付宝交易号")}
		if value := get("应用ID"); value != "" && value != c.config.AppID {
			row.ForeignApplication = true
		}
		if !identifier(row.OrderNo) || !identifier(row.TradeNo) {
			return nil, false, protocol("bill_order")
		}
		money := get("订单金额(元)")
		switch get("业务类型") {
		case "交易", "支付", "PAYMENT", "TRADE":
			row.Kind = "pay"
			row.State = "paid"
		case "退款", "REFUND":
			row.Kind = "refund"
			row.State = "succeeded"
			row.RefundNo = get("退款批次号")
			if row.RefundNo == "" {
				row.RefundNo = get("退款请求号")
			}
			if !identifier(row.RefundNo) {
				return nil, false, protocol("bill_refund_no")
			}
			if value := get("退款金额(元)"); value != "" {
				money = value
			}
			// Negative amounts are an official refund-row representation, never a payment fallback.
			money = strings.TrimPrefix(money, "-")
		default:
			return nil, false, protocol("bill_business_type")
		}
		row.AmountFen, err = amount(money)
		if err != nil || row.AmountFen <= 0 {
			return nil, false, protocol("bill_amount")
		}
		rows = append(rows, row)
	}
	if summaryFile && !summarySeen {
		return nil, false, protocol("bill_summary")
	}
	return rows, detail, nil
}
