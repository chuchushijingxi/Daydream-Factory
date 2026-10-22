package alipay

import (
	"context"

	"drama-agent/internal/application/ports"
	p "drama-agent/internal/domain/payment"
)

func (c *Channel) Refund(ctx context.Context, in ports.ChannelRefund) (ports.ChannelRefundResult, error) {
	if !identifier(in.OrderNo) || !identifier(in.RefundNo) || !identifier(in.TradeNo) || in.AmountFen <= 0 || in.TotalFen < in.AmountFen {
		return ports.ChannelRefundResult{}, protocol("refund")
	}
	yuan, err := p.FormatAmount(in.AmountFen)
	if err != nil {
		return ports.ChannelRefundResult{}, err
	}
	out, err := c.call(ctx, "alipay.trade.refund", map[string]string{"out_trade_no": in.OrderNo, "trade_no": in.TradeNo, "out_request_no": in.RefundNo, "refund_amount": yuan, "refund_reason": in.Reason}, false)
	if err != nil {
		return ports.ChannelRefundResult{}, err
	}
	if err = refundResponseIdentity(out, in.OrderNo, in.RefundNo, in.TradeNo); err != nil {
		return ports.ChannelRefundResult{}, err
	}
	if out.Code != "10000" {
		// 只有已验签的最终拒绝，并经原退款号新查询确认未受理，才允许恢复积分。
		// 余额不足、网络和协议错误都保留未知结果，不能据此退款失败。
		if out.Code == "40004" && (out.SubCode == "ACQ.TRADE_HAS_FINISHED" || out.SubCode == "ACQ.TRADE_NOT_ALLOW_REFUND") {
			result, queryErr := c.QueryRefund(ctx, in.RefundNo, in.OrderNo)
			if queryErr != nil {
				return ports.ChannelRefundResult{}, queryErr
			}
			if result.TradeNo != "" && result.TradeNo != in.TradeNo {
				return ports.ChannelRefundResult{}, identityError("trade_no")
			}
			if result.TotalFen != 0 && result.TotalFen != in.TotalFen {
				return ports.ChannelRefundResult{}, identityError("total_amount")
			}
			if result.AmountFen != 0 && result.AmountFen != in.AmountFen {
				return ports.ChannelRefundResult{}, identityError("refund_amount")
			}
			if result.State == "not_found" {
				result.State = "failed"
				result.AmountFen = in.AmountFen
				return result, nil
			}
			if result.AmountFen != in.AmountFen || (result.TotalFen != 0 && result.TotalFen != in.TotalFen) {
				return ports.ChannelRefundResult{}, identityError("refund_amount")
			}
			return result, nil
		}
		return ports.ChannelRefundResult{}, businessError()
	}
	if out.OrderNo != in.OrderNo || out.TradeNo != in.TradeNo {
		return ports.ChannelRefundResult{}, identityError("out_trade_no")
	}
	if out.RefundNo != "" && out.RefundNo != in.RefundNo {
		return ports.ChannelRefundResult{}, identityError("out_request_no")
	}
	var total int64
	if out.hasTotalAmount {
		total, err = amount(out.TotalAmount)
		if err != nil || total != in.TotalFen {
			return ports.ChannelRefundResult{}, identityError("total_amount")
		}
	}
	refunded, err := amount(out.RefundFee)
	if err != nil || refunded != in.AmountFen {
		return ports.ChannelRefundResult{}, identityError("refund_fee")
	}
	switch out.FundChange {
	case "Y":
		at, err := channelTime(out.RefundPaidAt, "gmt_refund_pay")
		if err != nil {
			return ports.ChannelRefundResult{}, err
		}
		// 同步退款通常不返回订单总额；缺失保持零，由应用查原退款号补足，绝不回填请求值。
		return ports.ChannelRefundResult{OrderNo: out.OrderNo, RefundNo: in.RefundNo, TradeNo: out.TradeNo, State: "succeeded", AmountFen: refunded, TotalFen: total, BillAt: at}, nil
	case "N":
		// N 也表示重复请求回读，继续查原退款号，不把它推断成失败。
		result, err := c.QueryRefund(ctx, in.RefundNo, in.OrderNo)
		if err != nil {
			return ports.ChannelRefundResult{}, err
		}
		if result.TradeNo != "" && result.TradeNo != in.TradeNo {
			return ports.ChannelRefundResult{}, identityError("trade_no")
		}
		if result.State == "not_found" {
			return ports.ChannelRefundResult{}, protocol("refund_status")
		}
		if result.AmountFen != in.AmountFen || (result.TotalFen != 0 && result.TotalFen != in.TotalFen) {
			return ports.ChannelRefundResult{}, identityError("refund_amount")
		}
		return result, nil
	default:
		return ports.ChannelRefundResult{}, protocol("fund_change")
	}
}

// QueryRefund 与应用端口一致，参数依次为稳定退款号、原订单号。
func (c *Channel) QueryRefund(ctx context.Context, refundNo, orderNo string) (ports.ChannelRefundResult, error) {
	if !identifier(orderNo) || !identifier(refundNo) {
		return ports.ChannelRefundResult{}, protocol("refund")
	}
	out, err := c.call(ctx, "alipay.trade.fastpay.refund.query", map[string]any{"out_trade_no": orderNo, "out_request_no": refundNo, "query_options": []string{"gmt_refund_pay"}}, false)
	if err != nil {
		return ports.ChannelRefundResult{}, err
	}
	if err = refundResponseIdentity(out, orderNo, refundNo, ""); err != nil {
		return ports.ChannelRefundResult{}, err
	}
	// 无已知交易号的查询端口保留实际返回值，交应用与冻结订单核对，负面结果也不丢证据。
	result := ports.ChannelRefundResult{OrderNo: orderNo, RefundNo: refundNo, TradeNo: out.TradeNo}
	// 不存在应答附带的金额仍是渠道事实，不能丢成零让应用误作缺省而重发或恢复积分。
	if out.hasRefundAmount {
		result.AmountFen, err = amount(out.RefundAmount)
		if err != nil || result.AmountFen <= 0 {
			return ports.ChannelRefundResult{}, protocol("refund_amount")
		}
	}
	if out.hasTotalAmount {
		result.TotalFen, err = amount(out.TotalAmount)
		if err != nil || result.TotalFen <= 0 || result.TotalFen < result.AmountFen {
			return ports.ChannelRefundResult{}, protocol("total_amount")
		}
	}
	if out.Code == "40004" && (out.SubCode == "ACQ.TRADE_NOT_EXIST" || out.SubCode == "ACQ.REFUND_NOT_EXIST") {
		result.State = "not_found"
		return result, nil
	}
	if out.Code != "10000" {
		return ports.ChannelRefundResult{}, businessError()
	}
	if out.OrderNo != orderNo || out.RefundNo != refundNo || !identifier(out.TradeNo) {
		return ports.ChannelRefundResult{}, identityError("out_request_no")
	}
	if result.AmountFen <= 0 {
		return ports.ChannelRefundResult{}, protocol("refund_amount")
	}
	// 10000 只说明查询执行成功；缺少成功状态仍是未知，不能恢复积分。
	if out.RefundStatus != "REFUND_SUCCESS" {
		return ports.ChannelRefundResult{}, protocol("refund_status")
	}
	result.BillAt, err = channelTime(out.RefundPaidAt, "gmt_refund_pay")
	if err != nil {
		return ports.ChannelRefundResult{}, err
	}
	result.TradeNo = out.TradeNo
	result.State = "succeeded"
	return result, nil
}

// 已验签只确认来源；含关联字段的负面应答也须先核对，才能接受不存在或最终拒绝。
func refundResponseIdentity(out response, orderNo, refundNo, tradeNo string) error {
	if out.hasOrderNo && out.OrderNo != orderNo {
		return identityError("out_trade_no")
	}
	if out.hasRefundNo && out.RefundNo != refundNo {
		return identityError("out_request_no")
	}
	if out.hasTradeNo {
		if !identifier(out.TradeNo) {
			return protocol("trade_no")
		}
		if tradeNo != "" && out.TradeNo != tradeNo {
			return identityError("trade_no")
		}
	}
	return nil
}
