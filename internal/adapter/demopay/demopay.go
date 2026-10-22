// Package demopay persists simulated channel facts without touching wallets or payment orders.
package demopay

import (
	"context"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
	"errors"
	"time"
)

type Client struct {
	store ports.DemoPaymentStore
	now   func() time.Time
}

func New(store ports.DemoPaymentStore) (*Client, error) {
	if store == nil {
		return nil, fault.New("PAYMENT_CHANNEL_UNAVAILABLE", "演示支付存储尚未装配")
	}
	return &Client{store: store, now: time.Now}, nil
}
func (c *Client) ID() string { return "demo" }
func (c *Client) Create(ctx context.Context, in ports.ChannelOrder) (string, error) {
	if !p.ValidOrderNo(in.OrderNo) || in.AmountFen <= 0 || in.AmountFen > p.MaxAmount || in.ExpiresAt.IsZero() || !c.now().Before(in.ExpiresAt) {
		return "", fault.ErrInvalid
	}
	if _, err := c.store.CreateDemoPayment(ctx, in.OrderNo, in.AmountFen); err != nil {
		return "", err
	}
	return "demo://" + in.OrderNo, nil
}
func (c *Client) Query(ctx context.Context, no string) (ports.ChannelTrade, error) {
	if !p.ValidOrderNo(no) {
		return ports.ChannelTrade{}, fault.ErrInvalid
	}
	t, err := c.store.DemoPayment(ctx, no)
	if errors.Is(err, fault.ErrNotFound) {
		return ports.ChannelTrade{OrderNo: no, State: "not_found"}, nil
	}
	if err != nil {
		return ports.ChannelTrade{}, err
	}
	return demoTrade(t)
}
func (c *Client) Close(ctx context.Context, no string) error {
	if !p.ValidOrderNo(no) {
		return fault.ErrInvalid
	}
	_, err := c.store.CloseDemoPayment(ctx, no)
	if errors.Is(err, fault.ErrNotFound) {
		return nil
	}
	return err
}
func (c *Client) Simulate(ctx context.Context, no, result string) (ports.ChannelTrade, error) {
	if !p.ValidOrderNo(no) || result != "paid" && result != "failed" {
		return ports.ChannelTrade{}, fault.ErrInvalid
	}
	t, err := c.store.SimulateDemoPayment(ctx, no, result)
	if err != nil {
		return ports.ChannelTrade{}, err
	}
	return demoTrade(t)
}
func (c *Client) Refund(ctx context.Context, in ports.ChannelRefund) (ports.ChannelRefundResult, error) {
	if !p.ValidOrderNo(in.OrderNo) || in.RefundNo == "" || len(in.RefundNo) > 64 || in.TradeNo != "demo:"+in.OrderNo || in.AmountFen <= 0 || in.AmountFen > p.MaxAmount || in.TotalFen < in.AmountFen || in.TotalFen > p.MaxAmount {
		return ports.ChannelRefundResult{}, fault.ErrInvalid
	}
	t, err := c.store.RefundDemoPayment(ctx, in)
	if err != nil {
		return ports.ChannelRefundResult{}, err
	}
	return demoRefund(t)
}
func (c *Client) QueryRefund(ctx context.Context, no, orderNo string) (ports.ChannelRefundResult, error) {
	if no == "" || len(no) > 64 || !p.ValidOrderNo(orderNo) {
		return ports.ChannelRefundResult{}, fault.ErrInvalid
	}
	t, err := c.store.DemoPayment(ctx, orderNo)
	if errors.Is(err, fault.ErrNotFound) {
		return ports.ChannelRefundResult{RefundNo: no, OrderNo: orderNo, State: "not_found"}, nil
	}
	if err != nil {
		return ports.ChannelRefundResult{}, err
	}
	if t.RefundNo != no {
		return ports.ChannelRefundResult{RefundNo: no, OrderNo: orderNo, State: "not_found"}, nil
	}
	return demoRefund(t)
}
func (c *Client) ParseNotify(context.Context, ports.NotifyInput) (ports.PaymentNotify, ports.NotifyAck, error) {
	return ports.PaymentNotify{}, ports.NotifyAck{Status: 400, ContentType: "text/plain", Body: "fail"}, fault.New("PAYMENT_NOTIFY_UNSUPPORTED", "演示支付不接受渠道回调")
}
func (c *Client) Bill(context.Context, time.Time) ([]ports.BillRow, error) {
	return nil, fault.New("PAYMENT_BILL_UNSUPPORTED", "演示支付不提供渠道账单")
}
func demoTrade(t ports.DemoPaymentTrade) (ports.ChannelTrade, error) {
	if t.AmountFen <= 0 || t.AmountFen > p.MaxAmount {
		return ports.ChannelTrade{}, fault.New("PAYMENT_CHANNEL_FAILED", "演示交易事实无效")
	}
	switch t.State {
	case "notpay", "failed", "closed":
	case "paid":
		if t.PaidAt == nil || t.PaidAt.IsZero() {
			return ports.ChannelTrade{}, fault.New("PAYMENT_CHANNEL_FAILED", "演示支付缺少付款时间")
		}
	default:
		return ports.ChannelTrade{}, fault.New("PAYMENT_CHANNEL_FAILED", "演示交易状态无效")
	}
	trade := ports.ChannelTrade{OrderNo: t.OrderNo, State: t.State, AmountFen: t.AmountFen, PaidAt: t.PaidAt}
	if t.State == "paid" {
		trade.TradeNo = "demo:" + t.OrderNo
	}
	return trade, nil
}
func demoRefund(t ports.DemoPaymentTrade) (ports.ChannelRefundResult, error) {
	if t.State != "paid" || t.RefundNo == "" || t.RefundedFen <= 0 || t.RefundedFen > t.AmountFen || t.RefundBillAt == nil || t.RefundBillAt.IsZero() {
		return ports.ChannelRefundResult{}, fault.New("PAYMENT_CHANNEL_FAILED", "演示退款事实无效")
	}
	return ports.ChannelRefundResult{RefundNo: t.RefundNo, OrderNo: t.OrderNo, TradeNo: "demo:" + t.OrderNo, ChannelRefundID: "demo:" + t.RefundNo, State: "succeeded", AmountFen: t.RefundedFen, TotalFen: t.AmountFen, BillAt: t.RefundBillAt}, nil
}

var _ ports.PaymentChannel = (*Client)(nil)
