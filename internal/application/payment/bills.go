package payment

import (
	"context"
	"errors"
	"sort"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

// ReconcileBills 每日北京时间10点起领取前日账单；幂等、每小时重试及24次上限由存储领取保证。
func (s *Service) ReconcileBills(ctx context.Context) error {
	local := s.now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	var candidate *time.Time
	if local.Hour() >= 10 {
		date := time.Date(local.Year(), local.Month(), local.Day()-1, 0, 0, 0, 0, local.Location())
		candidate = &date
	}

	var ids []string
	for id := range s.Channels {
		if id != "demo" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var failures []error
	for _, id := range ids {
		ch, err := s.channel(id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		claim, claimed, err := s.Store.ClaimPaymentBill(ctx, id, candidate, s.now())
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !claimed {
			continue
		}
		billCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		rows, err := ch.Bill(billCtx, claim.Date)
		summary := map[string]int64{}
		if err == nil {
			summary, err = s.CompareBill(billCtx, id, claim.Date, rows)
		}
		cancel()
		// 失败不把报文或渠道错误落入summary，只保留结果和下一次领取预算。
		finishErr := s.Store.FinishPaymentBill(ctx, id, claim, err == nil, summary)
		if err != nil {
			failures = append(failures, channelError(err))
		}
		if finishErr != nil {
			failures = append(failures, finishErr)
		}
	}
	return errors.Join(failures...)
}

// CompareBill 比对已解析事实；只有查单确认成功才补到账，差异从不直接扣积分。
func (s *Service) CompareBill(ctx context.Context, channel string, date time.Time, rows []ports.BillRow) (map[string]int64, error) {
	ch, err := s.channel(channel)
	if err != nil {
		return nil, err
	}
	start, end := p.BeijingDay(date)
	local, err := s.Store.PaymentOrdersForBill(ctx, channel, start, end)
	if err != nil {
		return nil, err
	}
	summary := map[string]int64{"payments": 0, "refunds": 0, "anomalies": 0, "repaired": 0}
	paymentRepresented := map[string]bool{}
	seenRefunds := map[string]bool{}
	anomaly := func(kind, no string, detail map[string]any) error {
		summary["anomalies"]++
		return s.alert(ctx, kind, channel, no, detail)
	}
	for _, row := range rows {
		if err = ctx.Err(); err != nil {
			return summary, err
		}
		if row.AmountFen <= 0 || row.AmountFen > p.MaxAmount || (row.Kind != "pay" && row.Kind != "refund") {
			return summary, channelError(fault.ErrInvalid)
		}
		order, lookupErr := s.Store.PaymentOrder(ctx, row.OrderNo)
		if row.ForeignApplication {
			// 同一商户账单可以混合其他应用；其事实不能驱动本应用查询、补单或代表已到账交易。
			if lookupErr != nil && !notFound(lookupErr) {
				return summary, lookupErr
			}
			if notFound(lookupErr) {
				err = anomaly("bill_missing_local", "", map[string]any{"reportedOrderNo": row.OrderNo, "foreignApplication": true})
			} else {
				err = anomaly("identity_mismatch", order.OrderNo, map[string]any{"foreignApplication": true})
			}
			if err != nil {
				return summary, err
			}
			continue
		}

		if lookupErr != nil {
			if !notFound(lookupErr) {
				return summary, lookupErr
			}
			kind := "bill_missing_local"
			if row.Kind == "refund" {
				kind = "refund_mismatch"
			}
			if err = anomaly(kind, "", map[string]any{"reportedOrderNo": row.OrderNo, "amountFen": row.AmountFen, "refundNo": row.RefundNo}); err != nil {
				return summary, err
			}
			continue
		}
		if order.Channel != channel {
			if err = anomaly("identity_mismatch", order.OrderNo, nil); err != nil {
				return summary, err
			}
			continue
		}
		if row.Kind == "pay" {
			summary["payments"]++
			paymentRepresented[order.OrderNo] = true
			if row.AmountFen != order.AmountFen || (order.ChannelTradeNo != "" && order.ChannelTradeNo != row.TradeNo) {
				if err = anomaly("bill_amount_mismatch", order.OrderNo, map[string]any{"expectedFen": order.AmountFen, "actualFen": row.AmountFen}); err != nil {
					return summary, err
				}
				continue
			}
			if order.Status == p.Pending || order.Status == p.Closed {
				// 即使账单已验签，也再次查单再走统一ConfirmPaid，不能把账单行当第二种加分入口。
				updated, trade, queryErr := s.queryOrder(ctx, order, "bill", "")
				if queryErr != nil {
					return summary, queryErr
				}
				if trade.State == "paid" && (updated.Status == p.Credited || updated.Status == p.Closed) {
					summary["repaired"]++
				} else {
					if err = anomaly("bill_amount_mismatch", order.OrderNo, map[string]any{"queryState": trade.State}); err != nil {
						return summary, err
					}
				}
			}
			continue
		}
		summary["refunds"]++
		seenRefunds[row.RefundNo] = true
		refund, refundErr := s.Store.PaymentRefund(ctx, row.RefundNo)
		if refundErr != nil && !notFound(refundErr) {
			return summary, refundErr
		}
		state := row.State
		if state != "succeeded" {
			// 微信退款账单可能仅表示处理中/异常快照，不能将所有退款行都当作成功。
			result, queryErr := ch.QueryRefund(ctx, row.RefundNo, row.OrderNo)
			if queryErr != nil {
				return summary, channelError(queryErr)
			}
			if result.RefundNo != row.RefundNo || result.OrderNo != row.OrderNo {
				return summary, fault.New("PAYMENT_CONFLICT", "退款账单核对失败")
			}
			if result.State != "pending" && result.State != "succeeded" && result.State != "failed" && result.State != "not_found" {
				return summary, channelError(fault.ErrInvalid)
			}
			state = result.State
			if (result.AmountFen != 0 && result.AmountFen != row.AmountFen) || (result.TotalFen != 0 && result.TotalFen != order.AmountFen) || (result.TradeNo != "" && result.TradeNo != order.ChannelTradeNo) {
				if err = anomaly("refund_mismatch", order.OrderNo, map[string]any{"refundNo": row.RefundNo}); err != nil {
					return summary, err
				}
				continue
			}
		}
		// 同日支付并退款的账单可能只有退款行；可信原交易仍证明渠道存在该支付。
		// 跨日退款不覆盖本账单日的支付存在性，也不凭退款行补单或改退款终态。
		if refundErr == nil && refund.OrderNo == order.OrderNo && refund.AmountFen == row.AmountFen && state != "not_found" && order.ChannelTradeNo != "" && row.TradeNo == order.ChannelTradeNo && order.PaidAt != nil && !order.PaidAt.Before(start) && order.PaidAt.Before(end) {
			paymentRepresented[order.OrderNo] = true
		}
		if refundErr != nil || refund.OrderNo != order.OrderNo || refund.AmountFen != row.AmountFen || (state == "succeeded" && refund.Status != "succeeded") || (refund.Status == "succeeded" && state != "succeeded") {
			if err = anomaly("refund_mismatch", order.OrderNo, map[string]any{"refundNo": row.RefundNo, "amountFen": row.AmountFen, "channelState": state}); err != nil {
				return summary, err
			}
		}
	}
	for _, order := range local {
		if order.PaidAt != nil && !order.PaidAt.Before(start) && order.PaidAt.Before(end) && (order.Status == p.Credited || order.Status == p.Refunded) && !paymentRepresented[order.OrderNo] {
			if err = anomaly("bill_missing_channel", order.OrderNo, nil); err != nil {
				return summary, err
			}
		}
		details, detailErr := s.Store.PaymentOrderDetails(ctx, order.OrderNo)
		if detailErr != nil {
			return summary, detailErr
		}
		for _, refund := range details.Refunds {
			if refund.Status == "succeeded" && refund.BillAt != nil && !refund.BillAt.Before(start) && refund.BillAt.Before(end) && !seenRefunds[refund.RefundNo] {
				if err = anomaly("refund_mismatch", order.OrderNo, map[string]any{"refundNo": refund.RefundNo}); err != nil {
					return summary, err
				}
			}
		}
	}
	return summary, nil
}
