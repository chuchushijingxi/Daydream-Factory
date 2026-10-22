package alipay

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

func identifier(s string) bool {
	return s != "" && len(s) <= 128 && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\r\n\x00")
}
func amount(raw string) (int64, error) {
	// Official yuan values may omit trailing zeroes; all normalization remains integer-only.
	parts := strings.Split(raw, ".")
	if len(parts) == 1 {
		raw += ".00"
	} else if len(parts) == 2 && len(parts[1]) == 1 {
		raw += "0"
	}
	return p.ParseAmount(raw)
}
func channelTime(raw, field string) (*time.Time, error) {
	at, err := time.ParseInLocation("2006-01-02 15:04:05", raw, beijing)
	if err != nil || at.Year() < 1 || at.Format("2006-01-02 15:04:05") != raw {
		return nil, protocol(field)
	}
	at = at.UTC()
	return &at, nil
}
func trade(out response, expected string) (ports.ChannelTrade, error) {
	result := ports.ChannelTrade{OrderNo: out.OrderNo, TradeNo: out.TradeNo}
	if !identifier(out.OrderNo) || (expected != "" && out.OrderNo != expected) {
		return ports.ChannelTrade{}, identityError("out_trade_no")
	}
	var err error
	result.AmountFen, err = amount(out.TotalAmount)
	if err != nil || result.AmountFen <= 0 {
		return ports.ChannelTrade{}, fault.New("PAYMENT_INVALID_AMOUNT", "alipay.total_amount")
	}
	switch out.TradeStatus {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		result.State = "paid"
		if !identifier(out.TradeNo) {
			return ports.ChannelTrade{}, protocol("trade_no")
		}
		result.PaidAt, err = channelTime(out.PaidAt, "send_pay_date")
		if err != nil {
			return ports.ChannelTrade{}, err
		}
	case "WAIT_BUYER_PAY":
		result.State = "notpay"
	case "TRADE_CLOSED":
		result.State = "closed"
	default:
		return ports.ChannelTrade{}, protocol("trade_status")
	}
	return result, nil
}
func (c *Channel) Create(ctx context.Context, in ports.ChannelOrder) (string, error) {
	if !identifier(in.OrderNo) || in.Description == "" || len(in.Description) > 256 || in.AmountFen <= 0 {
		return "", protocol("order")
	}
	yuan, err := p.FormatAmount(in.AmountFen)
	if err != nil {
		return "", err
	}
	minutes := int64(in.ExpiresAt.Sub(c.config.Now()) / time.Minute)
	if minutes < 1 || minutes > 15 {
		return "", protocol("expiresAt")
	}
	ttl := strconv.FormatInt(minutes, 10) + "m"
	out, err := c.call(ctx, "alipay.trade.precreate", map[string]string{"out_trade_no": in.OrderNo, "total_amount": yuan, "subject": in.Description, "seller_id": c.config.SellerID, "timeout_express": ttl, "qr_code_timeout_express": ttl}, true)
	if err != nil {
		return "", err
	}
	if out.Code != "10000" {
		return "", businessError()
	}
	if out.OrderNo != in.OrderNo {
		return "", identityError("out_trade_no")
	}
	u, err := url.Parse(out.QRCode)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", protocol("qr_code")
	}
	return out.QRCode, nil
}
func (c *Channel) Query(ctx context.Context, no string) (ports.ChannelTrade, error) {
	if !identifier(no) {
		return ports.ChannelTrade{}, protocol("out_trade_no")
	}
	out, err := c.call(ctx, "alipay.trade.query", map[string]string{"out_trade_no": no}, false)
	if err != nil {
		return ports.ChannelTrade{}, err
	}
	if out.Code == "40004" && out.SubCode == "ACQ.TRADE_NOT_EXIST" {
		return ports.ChannelTrade{OrderNo: no, State: "not_found"}, nil
	}
	if out.Code != "10000" {
		return ports.ChannelTrade{}, businessError()
	}
	return trade(out, no)
}
func (c *Channel) Close(ctx context.Context, no string) error {
	if !identifier(no) {
		return protocol("out_trade_no")
	}
	out, err := c.call(ctx, "alipay.trade.close", map[string]string{"out_trade_no": no}, false)
	if err != nil {
		return err
	}
	if out.Code == "40004" && out.SubCode == "ACQ.TRADE_NOT_EXIST" {
		return nil
	}
	if out.Code != "10000" {
		return businessError()
	}
	if out.OrderNo != no {
		return identityError("out_trade_no")
	}
	return nil
}
func (c *Channel) ParseNotify(ctx context.Context, input ports.NotifyInput) (ports.PaymentNotify, ports.NotifyAck, error) {
	ack := ports.NotifyAck{Status: 200, ContentType: "text/plain; charset=utf-8", Body: "success"}
	fail := func(err error) (ports.PaymentNotify, ports.NotifyAck, error) {
		return ports.PaymentNotify{}, ports.NotifyAck{Status: 400, ContentType: "text/plain; charset=utf-8", Body: "fail"}, err
	}
	if ctx.Err() != nil {
		return fail(channelError())
	}
	if int64(len(input.Body)) > appconfig.PaymentNotifyMaxBytes {
		return fail(protocol("size"))
	}
	// ParseQuery decodes exactly once; preserving all fields also signs future protocol additions.
	values, err := url.ParseQuery(string(input.Body))
	if err != nil {
		return fail(protocol("form"))
	}
	for k, v := range values {
		if k == "" || len(v) != 1 {
			return fail(protocol("duplicate"))
		}
	}
	if values.Get("sign_type") != "RSA2" {
		return fail(signatureError())
	}
	if err = verify(c.public, []byte(canonical(values, true)), values.Get("sign")); err != nil {
		return fail(err)
	}
	if values.Get("app_id") != c.config.AppID {
		return fail(identityError("app_id"))
	}
	if values.Get("seller_id") != c.config.SellerID {
		return fail(identityError("seller_id"))
	}
	out := response{OrderNo: values.Get("out_trade_no"), TradeNo: values.Get("trade_no"), TradeStatus: values.Get("trade_status"), TotalAmount: values.Get("total_amount"), PaidAt: values.Get("gmt_payment")}
	result, err := trade(out, "")
	if err != nil {
		return fail(err)
	}
	if !identifier(result.TradeNo) {
		return fail(protocol("trade_no"))
	}
	// refund_fee is cumulative and cannot identify one out_request_no; refund facts use QueryRefund.
	return ports.PaymentNotify{Kind: "pay", Trade: result}, ack, nil
}
