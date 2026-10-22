package payment

import (
	"context"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

// ConfirmNotification 消费适配器已验签、已核验商户的事实；HTTP只负责运输和Ack。
func (s *Service) ConfirmNotification(ctx context.Context, channel string, notify ports.PaymentNotify) error {
	switch notify.Kind {
	case "refund":
		_, err := s.ConfirmRefund(ctx, channel, notify.Refund, "notify")
		return err
	case "pay":
		trade := notify.Trade
		if trade.State == "paid" {
			_, err := s.ConfirmPaid(ctx, channel, trade.OrderNo, trade, "notify")
			return err
		}
		if !validTradeState(trade.State) || trade.State == "not_found" {
			return channelError(fault.ErrInvalid)
		}
		if _, err := s.channel(channel); err != nil {
			return err
		}
		order, err := s.Store.PaymentOrder(ctx, trade.OrderNo)
		if err != nil {
			if notFound(err) {
				if alertErr := s.alert(ctx, "identity_mismatch", channel, "", map[string]any{"reportedOrderNo": trade.OrderNo}); alertErr != nil {
					return alertErr
				}
			}
			return orderError(err)
		}
		if channel != order.Channel || (order.ChannelTradeNo != "" && trade.TradeNo != "" && order.ChannelTradeNo != trade.TradeNo) {
			if err = s.alert(ctx, "identity_mismatch", channel, order.OrderNo, nil); err != nil {
				return err
			}
			return fault.New("PAYMENT_CONFLICT", "支付订单核对失败")
		}
		if trade.AmountFen != order.AmountFen {
			if err = s.alert(ctx, "amount_mismatch", channel, order.OrderNo, map[string]any{"expectedFen": order.AmountFen, "actualFen": trade.AmountFen}); err != nil {
				return err
			}
			return fault.New("PAYMENT_CONFLICT", "支付金额不一致")
		}
		// 已知非成功状态确认持久化后可Ack；不会将notpay/closed/failed当作到账或撤销已有到账。
		return s.event(ctx, order.OrderNo, "notify", trade.State, "", "", map[string]any{"state": trade.State, "amountFen": trade.AmountFen, "tradeNo": trade.TradeNo})
	default:
		return channelError(fault.ErrInvalid)
	}
}

type DemoSimulator interface {
	Simulate(context.Context, string, string) (ports.ChannelTrade, error)
}

func (s *Service) Simulate(ctx context.Context, actor ports.Actor, no, result string) (p.Order, error) {
	order, err := s.GetOrder(ctx, actor, no)
	if err != nil {
		return p.Order{}, err
	}
	if order.Channel != "demo" || (result != "paid" && result != "failed") {
		return order, fault.ErrInvalid
	}
	if order.Status != p.Pending && !(order.Status == p.Closed && s.Environment == "development" && result == "paid") {
		return order, fault.New("PAYMENT_CONFLICT", "订单状态不允许模拟支付")
	}
	ch, err := s.channel("demo")
	if err != nil {
		return order, err
	}
	simulator, ok := ch.(DemoSimulator)
	if !ok {
		return order, fault.New("PAYMENT_CHANNEL_UNAVAILABLE", "演示支付暂不可用")
	}
	trade, err := simulator.Simulate(ctx, no, result)
	if err != nil {
		return order, channelError(err)
	}
	if result == "paid" {
		return s.ConfirmPaid(ctx, "demo", no, trade, "simulate")
	}
	if trade.OrderNo != no || trade.State != "failed" {
		return order, channelError(fault.ErrInvalid)
	}
	return order, s.event(ctx, no, "simulate", "failed", "", "", map[string]any{"state": "failed"})
}
