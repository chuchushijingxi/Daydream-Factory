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

type CreateRefundInput struct {
	RefundID string `json:"refundId"`
	Credits  int64  `json:"credits"`
	Reason   string `json:"reason"`
}

func (s *Service) RefundQuote(ctx context.Context, actor ports.Actor, no string, credits int64) (p.RefundQuote, error) {
	if err := actor.RequireOperator(); err != nil {
		return p.RefundQuote{}, err
	}
	if credits < 0 || credits > p.MaxAmount {
		return p.RefundQuote{}, fault.ErrInvalid
	}
	order, err := s.Store.PaymentOrder(ctx, no)
	if err != nil {
		return p.RefundQuote{}, orderError(err)
	}
	if err = s.refundable(order); err != nil {
		return p.RefundQuote{}, err
	}
	return s.Store.PaymentRefundQuote(ctx, no, credits)
}
func (s *Service) refundable(order p.Order) error {
	if order.Status != p.Credited {
		return fault.New("PAYMENT_CONFLICT", "仅已到账订单可退款")
	}
	if !s.now().Before(order.CreatedAt.Add(365 * 24 * time.Hour)) {
		return fault.New("PAYMENT_REFUND_EXCEEDED", "已超过可发起退款期限")
	}
	return nil
}
func (s *Service) CreateRefund(ctx context.Context, actor ports.Actor, no string, in CreateRefundInput) (p.Refund, error) {
	if err := actor.RequireOperator(); err != nil {
		return p.Refund{}, err
	}
	if !validID(in.RefundID) || !validReason(in.Reason) || in.Credits <= 0 || in.Credits > p.MaxAmount {
		return p.Refund{}, fault.ErrInvalid
	}
	order, err := s.Store.PaymentOrder(ctx, no)
	if err != nil {
		return p.Refund{}, orderError(err)
	}
	if _, err = s.channel(order.Channel); err != nil {
		return p.Refund{}, err
	}
	// 退款同号重试交给事务幂等判断，已成功退款后也不能丢失原回执。
	existing, lookupErr := s.Store.PaymentRefundByRequest(ctx, in.RefundID)
	if lookupErr == nil {
		if existing.OrderNo != no || existing.OperatorID != actor.UserID || existing.RechargeCredits != in.Credits || existing.Reason != in.Reason {
			return p.Refund{}, fault.New("PAYMENT_CONFLICT", "退款编号已用于其他请求")
		}
		return existing, nil
	}
	if !notFound(lookupErr) {
		return p.Refund{}, lookupErr
	}
	if err = s.refundable(order); err != nil {
		return p.Refund{}, err
	}
	// 先只读拒绝折算为0分的请求；存储仍须在锁内重算并在任何写入前拒绝。
	quote, err := s.Store.PaymentRefundQuote(ctx, no, in.Credits)
	if err != nil {
		return p.Refund{}, err
	}
	if quote.AmountFen <= 0 {
		return p.Refund{}, fault.New("PAYMENT_INVALID_AMOUNT", "退款金额不足1分，请增加退款积分")
	}
	return s.Store.CreatePaymentRefund(ctx, p.RefundRequest{RefundID: in.RefundID, OrderNo: no, OperatorID: actor.UserID, Reason: in.Reason, Credits: in.Credits})
}

// ConfirmRefund 只把明确的渠道终态交给事务；网络错误和未知结果绝不触发积分恢复。
func (s *Service) ConfirmRefund(ctx context.Context, channel string, result ports.ChannelRefundResult, source string) (p.Refund, error) {
	if source != "notify" && source != "query" && source != "refund" && source != "bill" {
		return p.Refund{}, fault.ErrInvalid
	}
	if _, err := s.channel(channel); err != nil {
		return p.Refund{}, err
	}
	refund, err := s.Store.PaymentRefund(ctx, result.RefundNo)
	if err != nil {
		if notFound(err) {
			if alertErr := s.alert(ctx, "refund_mismatch", channel, "", map[string]any{"reportedRefundNo": result.RefundNo, "reportedOrderNo": result.OrderNo}); alertErr != nil {
				return p.Refund{}, alertErr
			}
		}
		return p.Refund{}, err
	}
	order, err := s.Store.PaymentOrder(ctx, refund.OrderNo)
	if err != nil {
		return p.Refund{}, orderError(err)
	}
	if order.Channel != channel || result.OrderNo != refund.OrderNo || result.RefundNo != refund.RefundNo {
		if err = s.alert(ctx, "refund_mismatch", channel, order.OrderNo, nil); err != nil {
			return refund, err
		}
		return refund, fault.New("PAYMENT_CONFLICT", "退款订单核对失败")
	}
	if (result.TotalFen != 0 && result.TotalFen != order.AmountFen) || (result.TradeNo != "" && result.TradeNo != order.ChannelTradeNo) {
		if err = s.alert(ctx, "refund_mismatch", channel, order.OrderNo, map[string]any{"refundNo": refund.RefundNo}); err != nil {
			return refund, err
		}
		return refund, fault.New("PAYMENT_CONFLICT", "退款原交易核对失败")
	}
	if result.State != "pending" && result.State != "succeeded" && result.State != "failed" {
		return refund, channelError(fault.ErrInvalid)
	}
	if (result.State == "succeeded" || result.AmountFen != 0) && result.AmountFen != refund.AmountFen {
		if err = s.alert(ctx, "refund_mismatch", channel, order.OrderNo, map[string]any{"expectedFen": refund.AmountFen, "actualFen": result.AmountFen}); err != nil {
			return refund, err
		}
		return refund, fault.New("PAYMENT_CONFLICT", "退款金额不一致")
	}
	if result.BillAt != nil && result.BillAt.IsZero() {
		return refund, channelError(fault.ErrInvalid)
	}
	if result.State == "succeeded" && (result.TotalFen == 0 || result.TradeNo == "" || (refund.BillAt == nil && result.BillAt == nil)) {
		// 回调可能缺少原交易号、总额或账单归属时间，必须按原号补查；
		// 不能用请求金额或本地完成时间冒充已验签渠道事实。
		ch, _ := s.channel(channel)
		queried, queryErr := ch.QueryRefund(ctx, refund.RefundNo, refund.OrderNo)
		if queryErr != nil {
			return refund, errors.Join(channelError(queryErr), s.channelFailureEvent(ctx, order.OrderNo, "refund"))
		}
		if queried.RefundNo != refund.RefundNo || queried.OrderNo != refund.OrderNo || (queried.AmountFen != 0 && queried.AmountFen != refund.AmountFen) || (queried.TotalFen != 0 && queried.TotalFen != order.AmountFen) || (queried.TradeNo != "" && queried.TradeNo != order.ChannelTradeNo) {
			if err = s.alert(ctx, "refund_mismatch", channel, order.OrderNo, map[string]any{"refundNo": refund.RefundNo}); err != nil {
				return refund, err
			}
			return refund, fault.New("PAYMENT_CONFLICT", "退款订单核对失败")
		}
		if queried.State != "succeeded" || queried.AmountFen != refund.AmountFen {
			return refund, channelError(fault.New("PAYMENT_CHANNEL_FAILED", "渠道退款缺少可核验事实"))
		}
		if result.TotalFen == 0 {
			result.TotalFen = queried.TotalFen
		}
		if result.TradeNo == "" {
			result.TradeNo = queried.TradeNo
		}
		if result.BillAt == nil {
			result.BillAt = queried.BillAt
		}
		if result.TotalFen == 0 || result.TradeNo == "" || (refund.BillAt == nil && (result.BillAt == nil || result.BillAt.IsZero())) {
			return refund, channelError(fault.New("PAYMENT_CHANNEL_FAILED", "渠道退款缺少账单归属时间"))
		}
	}
	if result.State == "pending" {
		if result.BillAt != nil {
			return s.Store.FinishPaymentRefund(ctx, refund.RefundNo, result, source)
		}
		if err = s.event(ctx, order.OrderNo, "refund", "pending", "", "", map[string]any{"refundNo": refund.RefundNo, "source": source}); err != nil {
			return refund, err
		}
		return refund, nil
	}
	updated, err := s.Store.FinishPaymentRefund(ctx, refund.RefundNo, result, source)
	if codeIs(err, "PAYMENT_REFUND_RESTORE_PENDING") {
		// 渠道已明确拒绝，但分类恢复事务整体回滚。独立留下可核查证据，
		// 不把pending写成failed；后续仍按原退款号查询并重试原稳定恢复编号。
		return refund, errors.Join(err, s.recordRefundRestorePending(ctx, channel, order.OrderNo, refund))
	}
	if err == nil && updated.Status == "failed" {
		slogRefundFailed(ctx, channel, order.OrderNo)
	}
	return updated, err
}
func (s *Service) recordRefundRestorePending(ctx context.Context, channel, no string, refund p.Refund) error {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	const code = "PAYMENT_REFUND_RESTORE_PENDING"
	slog.ErrorContext(auditCtx, "payment_alert", "kind", "refund_failed", "stage", "restore_pending", "channel", channel, "order_no", no, "error_code", code)
	id := "refund-restore-pending:" + refund.RefundNo
	facts := map[string]any{"refundNo": refund.RefundNo, "stage": "restore_pending", "errorCode": code}
	anomalyErr := s.Store.RecordPaymentAnomaly(auditCtx, p.Anomaly{ID: id, OrderNo: no, Channel: channel, Kind: "refund_failed", Detail: facts, Status: "open", CreatedAt: s.now()})
	eventErr := s.Store.RecordPaymentEvent(auditCtx, p.Event{ID: id, OrderNo: no, RefundID: refund.ID, Source: "refund", Outcome: "restore_pending", Facts: facts, CreatedAt: s.now()})
	return errors.Join(anomalyErr, eventErr)
}

func slogRefundFailed(ctx context.Context, channel, no string) {
	// 退款失败异常与分类恢复积分同事务记录，日志不重复写另一条异常。
	slog.ErrorContext(ctx, "payment_alert", "kind", "refund_failed", "channel", channel, "order_no", no)
}
func (s *Service) ScanRefunds(ctx context.Context) error {
	refunds, err := s.Store.ClaimPaymentRefunds(ctx, s.now(), 50, time.Minute)
	if err != nil {
		return err
	}
	return runClaimed(ctx, refunds, s.followRefund)
}

func (s *Service) followRefund(ctx context.Context, refund p.Refund) error {
	order, err := s.Store.PaymentOrder(ctx, refund.OrderNo)
	if err != nil {
		return orderError(err)
	}
	ch, err := s.channel(order.Channel)
	if err != nil {
		return err
	}
	request := ports.ChannelRefund{RefundNo: refund.RefundNo, OrderNo: order.OrderNo, TradeNo: order.ChannelTradeNo, Reason: refund.Reason, AmountFen: refund.AmountFen, TotalFen: order.AmountFen}
	var result ports.ChannelRefundResult
	if refund.Attempts == 1 {
		result, err = ch.Refund(ctx, request)
	} else {
		result, err = ch.QueryRefund(ctx, refund.RefundNo, refund.OrderNo)
		// 仅渠道明确证明退款不存在时按原编号重发；超时、HTTP错误和未知状态始终继续查询。
		if err == nil && result.State == "not_found" {
			if result.RefundNo != refund.RefundNo || result.OrderNo != refund.OrderNo || (result.TradeNo != "" && result.TradeNo != order.ChannelTradeNo) || (result.TotalFen != 0 && result.TotalFen != order.AmountFen) || (result.AmountFen != 0 && result.AmountFen != refund.AmountFen) {
				// 不存在也是带身份的渠道事实；附带的交易号或金额冲突时，不能借此原号重发。
				if alertErr := s.alert(ctx, "refund_mismatch", order.Channel, order.OrderNo, map[string]any{"refundNo": refund.RefundNo}); alertErr != nil {
					return alertErr
				}
				return fault.New("PAYMENT_CONFLICT", "退款订单核对失败")
			}
			result, err = ch.Refund(ctx, request)
		}
	}
	if err != nil {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		auditErr := s.event(auditCtx, order.OrderNo, "refund", "channel_failed", "", "", map[string]any{"refundNo": refund.RefundNo, "errorCode": "PAYMENT_CHANNEL_FAILED"})
		return errors.Join(channelError(err), auditErr)
	}
	_, err = s.ConfirmRefund(ctx, order.Channel, result, "refund")
	return err
}
