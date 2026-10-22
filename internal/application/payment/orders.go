package payment

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

type CreateOrderInput struct {
	ClientRequestID  string   `json:"clientRequestId"`
	Target           p.Target `json:"target"`
	PackageID        string   `json:"packageId"`
	Channel          string   `json:"channel"`
	AgreementVersion string   `json:"agreementVersion"`
}

func sameRequest(order p.Order, in CreateOrderInput) bool {
	return order.Target.Kind == in.Target.Kind && order.Target.ID == in.Target.ID && order.PackageID == in.PackageID && order.Channel == in.Channel && order.AgreementVersion == in.AgreementVersion
}
func (s *Service) CreateOrder(ctx context.Context, actor ports.Actor, in CreateOrderInput) (p.Order, error) {
	if err := s.requireBuyer(ctx, actor); err != nil {
		return p.Order{}, err
	}
	if !validID(in.ClientRequestID) || !validID(in.PackageID) || !in.Target.Valid() {
		return p.Order{}, fault.ErrInvalid
	}
	// 网络结果不明的重试先回读原单，不能因档位下架、限流或改价另建订单。
	prior, err := s.Store.PaymentOrderByRequest(ctx, actor.UserID, in.ClientRequestID)
	if err == nil {
		if !sameRequest(prior, in) {
			return p.Order{}, fault.New("PAYMENT_CONFLICT", "下单编号已用于其他请求")
		}
		return prior, nil
	}
	if !notFound(err) {
		return p.Order{}, err
	}
	ch, err := s.channel(in.Channel)
	if err != nil {
		return p.Order{}, err
	}
	if in.AgreementVersion != p.AgreementVersion {
		return p.Order{}, fault.New("PAYMENT_AGREEMENT_REQUIRED", "请同意当前充值协议")
	}
	if s.Targets == nil {
		return p.Order{}, fault.New("PAYMENT_TARGET_FORBIDDEN", "充值目标不可用")
	}
	target, err := s.Targets.Resolve(ctx, actor, in.Target)
	if err != nil {
		return p.Order{}, err
	}
	packages, err := s.Store.PaymentPackages(ctx)
	if err != nil {
		return p.Order{}, err
	}
	var pkg p.Package
	for _, v := range packages {
		if v.ID == in.PackageID {
			pkg = v
			break
		}
	}
	if pkg.ID == "" || !pkg.Enabled || pkg.TestOnly && !s.testPackages(actor) {
		return p.Order{}, fault.New("PAYMENT_PACKAGE_UNAVAILABLE", "充值档位不可用")
	}
	// 旧档位未补运营名称仍可充值；用户消费只校验冻结的金额与积分事实。
	if err = pkg.Snapshot().Validate(); err != nil {
		return p.Order{}, err
	}
	if s.RateLimiter == nil {
		return p.Order{}, fault.New("RUNTIME_UNAVAILABLE", "下单限流暂不可用")
	}
	allowed, err := s.RateLimiter.Allow(ctx, "payment:order:"+actor.UserID, 5, time.Minute)
	if err != nil {
		return p.Order{}, err
	}
	if !allowed {
		return p.Order{}, fault.New("RATE_LIMITED", "下单过于频繁，请稍后重试")
	}
	previous, err := s.Store.PendingPaymentOrder(ctx, actor.UserID)
	if err == nil {
		if _, err = s.closeOrder(ctx, previous, "replaced"); err != nil {
			return p.Order{}, err
		}
	} else if !notFound(err) {
		return p.Order{}, err
	}
	now := s.now()
	no, err := p.GenerateOrderNo(now)
	if err != nil {
		return p.Order{}, err
	}
	order := p.Order{OrderNo: no, BuyerUserID: actor.UserID, ClientRequestID: in.ClientRequestID, Target: target.Target, CreditAccountID: target.AccountID, PackageSnapshot: pkg.Snapshot(), Channel: in.Channel, Status: p.Pending, AgreementVersion: in.AgreementVersion, CreatedAt: now, ExpiresAt: now.Add(p.OrderLifetime), NextQueryAt: now.Add(10 * time.Second)}
	order, created, err := s.Store.CreatePaymentOrder(ctx, order)
	if err != nil {
		return p.Order{}, err
	}
	if !created {
		if !sameRequest(order, in) {
			return p.Order{}, fault.New("PAYMENT_CONFLICT", "下单编号已用于其他请求")
		}
		return order, nil
	}
	url, err := ch.Create(ctx, ports.ChannelOrder{OrderNo: order.OrderNo, Description: "积分充值", AmountFen: order.AmountFen, ExpiresAt: order.ExpiresAt})
	if err != nil || url == "" {
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		scheduleErr := s.Store.SchedulePaymentOrder(recoveryCtx, order.OrderNo, 0, s.now())
		eventErr := s.event(recoveryCtx, order.OrderNo, "create", "channel_failed", "", "", nil)
		return order, errors.Join(channelError(err), scheduleErr, eventErr)
	}
	return s.Store.SetPaymentOrderCode(ctx, order.OrderNo, url, "create")
}
func (s *Service) GetOrder(ctx context.Context, actor ports.Actor, no string) (p.Order, error) {
	if err := actor.Require(); err != nil {
		return p.Order{}, err
	}
	order, err := s.Store.PaymentOrder(ctx, no)
	if err != nil {
		return p.Order{}, orderError(err)
	}
	if order.BuyerUserID != actor.UserID {
		return p.Order{}, fault.New("PAYMENT_TARGET_FORBIDDEN", "无权读取此订单")
	}
	return order, nil
}
func (s *Service) ListOrders(ctx context.Context, actor ports.Actor, page, size int) (ports.Page[p.Order], error) {
	if err := actor.Require(); err != nil {
		return ports.Page[p.Order]{}, err
	}
	if err := ports.CheckPage(page, size); err != nil {
		return ports.Page[p.Order]{}, err
	}
	return s.Store.PaymentOrders(ctx, ports.PaymentOrderFilter{Page: page, PageSize: size, BuyerUserID: actor.UserID})
}
func (s *Service) CloseOrder(ctx context.Context, actor ports.Actor, no string) (p.Order, error) {
	order, err := s.GetOrder(ctx, actor, no)
	if err != nil {
		return p.Order{}, err
	}
	return s.closeOrder(ctx, order, "cancelled")
}
func (s *Service) closeOrder(ctx context.Context, order p.Order, reason string) (p.Order, error) {
	if order.Status != p.Pending {
		return order, nil
	}
	order, trade, err := s.queryOrder(ctx, order, "query", "")
	if err != nil {
		return order, err
	}
	if trade.State == "paid" || order.Status != p.Pending {
		return order, nil
	}
	ch, err := s.channel(order.Channel)
	if err != nil {
		return order, err
	}
	if err = ch.Close(ctx, order.OrderNo); err != nil {
		return order, errors.Join(channelError(err), s.channelFailureEvent(ctx, order.OrderNo, "close"))
	}
	return s.Store.ClosePaymentOrder(ctx, order.OrderNo, reason, "close")
}
func (s *Service) queryOrder(ctx context.Context, order p.Order, source, operator string) (p.Order, ports.ChannelTrade, error) {
	ch, err := s.channel(order.Channel)
	if err != nil {
		return order, ports.ChannelTrade{}, err
	}
	trade, err := ch.Query(ctx, order.OrderNo)
	if err != nil {
		return order, trade, errors.Join(channelError(err), s.channelFailureEvent(ctx, order.OrderNo, source))
	}
	if !validTradeState(trade.State) {
		return order, trade, channelError(fault.ErrInvalid)
	}
	if trade.OrderNo != order.OrderNo {
		if err = s.alert(ctx, "identity_mismatch", order.Channel, order.OrderNo, nil); err != nil {
			return order, trade, err
		}
		return order, trade, fault.New("PAYMENT_CONFLICT", "支付订单核对失败")
	}
	if err = s.event(ctx, order.OrderNo, source, trade.State, operator, "", map[string]any{"state": trade.State, "amountFen": trade.AmountFen, "tradeNo": trade.TradeNo}); err != nil {
		return order, trade, err
	}
	if trade.State == "paid" {
		order, err = s.ConfirmPaid(ctx, order.Channel, order.OrderNo, trade, source)
	}
	return order, trade, err
}

// ConfirmPaid 是通知、主动查询、演示和对账的唯一到账核对入口。
func (s *Service) ConfirmPaid(ctx context.Context, channel, no string, trade ports.ChannelTrade, source string) (p.Order, error) {
	if !validSource(source) {
		return p.Order{}, fault.ErrInvalid
	}
	if _, err := s.channel(channel); err != nil {
		return p.Order{}, err
	}
	order, err := s.Store.PaymentOrder(ctx, no)
	if err != nil {
		if notFound(err) {
			if alertErr := s.alert(ctx, "identity_mismatch", channel, "", map[string]any{"reportedOrderNo": no}); alertErr != nil {
				return p.Order{}, alertErr
			}
		}
		return p.Order{}, orderError(err)
	}
	if channel != order.Channel || trade.OrderNo != no || (order.ChannelTradeNo != "" && order.ChannelTradeNo != trade.TradeNo) {
		if err = s.alert(ctx, "identity_mismatch", channel, no, nil); err != nil {
			return order, err
		}
		return order, fault.New("PAYMENT_CONFLICT", "支付订单核对失败")
	}
	if trade.State != "paid" || trade.TradeNo == "" || trade.PaidAt == nil || trade.PaidAt.IsZero() {
		return order, fault.New("PAYMENT_CONFLICT", "支付尚未成功")
	}
	if trade.AmountFen != order.AmountFen {
		if err = s.alert(ctx, "amount_mismatch", channel, no, map[string]any{"expectedFen": order.AmountFen, "actualFen": trade.AmountFen}); err != nil {
			return order, err
		}
		return order, fault.New("PAYMENT_CONFLICT", "支付金额不一致")
	}
	result, err := s.Store.CreditPaymentOrder(ctx, ports.PaymentConfirmation{OrderNo: no, Channel: channel, Source: source, Trade: trade})
	if codeIs(err, "PAYMENT_COMMIT_UNKNOWN") {
		// 只能凭原稳定账务编号确认提交，读单状态本身不能证明两笔流水已提交。
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		confirmed, ok, readErr := s.Store.ConfirmedPaymentCredit(readCtx, no)
		if readErr == nil && ok {
			return confirmed, nil
		}
	}
	if err == nil && result.Status == p.Closed {
		slogLatePayment(ctx, channel, no)
	}
	return result, err
}
func slogLatePayment(ctx context.Context, channel, no string) {
	// closed 的异常与退款由同一存储事务记录，此处只输出安全告警。
	slog.ErrorContext(ctx, "payment_alert", "kind", "paid_after_close", "channel", channel, "order_no", no)
}

// ScanOrders 领取后逐单调用渠道，不持有数据库事务等待网络；重启沿持久化领取事实继续。
func (s *Service) ScanOrders(ctx context.Context) error {
	orders, err := s.Store.ClaimPaymentOrders(ctx, s.now(), 50, time.Minute)
	if err != nil {
		return err
	}
	return runClaimed(ctx, orders, func(ctx context.Context, order p.Order) error {
		var err error
		if !s.now().Before(order.ExpiresAt) {
			_, err = s.closeOrder(ctx, order, "expired")
		} else {
			_, _, err = s.queryOrder(ctx, order, "query", "")
		}
		next := s.now().Add(queryDelay(order.QueryAttempts))
		if next.After(order.ExpiresAt) && s.now().Before(order.ExpiresAt) {
			next = order.ExpiresAt
		}
		return errors.Join(err, s.Store.SchedulePaymentOrder(ctx, order.OrderNo, order.QueryAttempts, next))
	})
}

func queryDelay(attempt int) time.Duration {
	switch attempt {
	case 0:
		return 10 * time.Second
	case 1:
		return 30 * time.Second
	case 2:
		return time.Minute
	case 3:
		return 2 * time.Minute
	default:
		return 3 * time.Minute
	}
}
