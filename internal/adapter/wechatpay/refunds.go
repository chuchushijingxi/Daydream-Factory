package wechatpay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
	p "drama-agent/internal/domain/payment"
)

type refund struct {
	Channel            string `json:"channel"`
	AppID              string `json:"appid"`
	MchID              string `json:"mchid"`
	RefundID           string `json:"refund_id"`
	RefundNo           string `json:"out_refund_no"`
	OrderNo            string `json:"out_trade_no"`
	TradeNo            string `json:"transaction_id"`
	State              string `json:"status"`
	NotifyState        string `json:"refund_status"`
	CreateTime         string `json:"create_time"`
	Amount             amount `json:"amount"`
	hasAppID, hasMchID bool
}

func (v *refund) UnmarshalJSON(raw []byte) error {
	type decodedRefund refund
	var decoded decodedRefund
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*v = refund(decoded)
	_, v.hasAppID = fields["appid"]
	_, v.hasMchID = fields["mchid"]
	return nil
}

func (c *Client) Refund(ctx context.Context, in ports.ChannelRefund) (ports.ChannelRefundResult, error) {
	if !utf8.ValidString(in.Reason) || !identifier(in.RefundNo, 64) || !identifier(in.OrderNo, 32) || in.AmountFen <= 0 || in.AmountFen > in.TotalFen || in.TotalFen > p.MaxAmount {
		return ports.ChannelRefundResult{}, protocol()
	}
	body := struct {
		OrderNo   string `json:"out_trade_no"`
		RefundNo  string `json:"out_refund_no"`
		Reason    string `json:"reason"`
		NotifyURL string `json:"notify_url"`
		Amount    amount `json:"amount"`
	}{in.OrderNo, in.RefundNo, channelReason(in.Reason), c.cfg.NotifyURL, amount{Total: in.TotalFen, Refund: in.AmountFen, Currency: "CNY"}}
	var fact refund
	err := c.api(ctx, http.MethodPost, "/v3/refund/domestic/refunds", body, &fact)
	if err != nil {
		// 只对白名单业务拒绝补查同号，确认从未受理才结束；不按错误描述或次数猜失败。
		if !hasResponseCode(err, "USER_ACCOUNT_ABNORMAL") {
			return ports.ChannelRefundResult{}, err
		}
		queried, queryErr := c.QueryRefund(ctx, in.RefundNo, in.OrderNo)
		if queryErr != nil {
			return ports.ChannelRefundResult{}, queryErr
		}
		if queried.State == "not_found" {
			return ports.ChannelRefundResult{RefundNo: in.RefundNo, OrderNo: in.OrderNo, State: "failed"}, nil
		}
		if err = verifyRefundRequest(queried, in); err != nil {
			return ports.ChannelRefundResult{}, err
		}
		return queried, nil
	}
	if fact.OrderNo != in.OrderNo || fact.RefundNo != in.RefundNo || (in.TradeNo != "" && fact.TradeNo != in.TradeNo) {
		return ports.ChannelRefundResult{}, identityError()
	}
	if fact.Amount.Total != in.TotalFen || fact.Amount.Refund != in.AmountFen {
		return ports.ChannelRefundResult{}, amountError()
	}
	if fact.State == "CLOSED" {
		queried, err := c.QueryRefund(ctx, in.RefundNo, in.OrderNo)
		if err != nil {
			return ports.ChannelRefundResult{}, err
		}
		if err = verifyRefundRequest(queried, in); err != nil {
			return ports.ChannelRefundResult{}, err
		}
		return queried, nil
	}
	return c.refund(fact, false, true)
}
func (c *Client) QueryRefund(ctx context.Context, no, order string) (ports.ChannelRefundResult, error) {
	if !identifier(no, 64) || !identifier(order, 32) {
		return ports.ChannelRefundResult{}, protocol()
	}
	var fact refund
	err := c.api(ctx, http.MethodGet, "/v3/refund/domestic/refunds/"+cleanPath(no), nil, &fact)
	if hasResponseCode(err, "RESOURCE_NOT_EXISTS") {
		return ports.ChannelRefundResult{RefundNo: no, OrderNo: order, State: "not_found"}, nil
	}
	if err != nil {
		return ports.ChannelRefundResult{}, err
	}
	if fact.RefundNo != no || fact.OrderNo != order {
		return ports.ChannelRefundResult{}, identityError()
	}
	return c.refund(fact, true, true)
}
func (c *Client) refund(fact refund, queried, synchronous bool) (ports.ChannelRefundResult, error) {
	// 普通商户同步退款应答不承诺appid/mchid；商户授权请求与平台签名是其身份边界。
	// 一旦实际返回这些字段仍必须一致，通知则额外强制mchid存在。
	if (fact.hasAppID || fact.AppID != "") && fact.AppID != c.cfg.AppID || (fact.hasMchID || fact.MchID != "") && fact.MchID != c.cfg.MchID || !identifier(fact.OrderNo, 32) || !identifier(fact.RefundNo, 64) || !identifier(fact.RefundID, 64) || !identifier(fact.TradeNo, 64) {
		return ports.ChannelRefundResult{}, identityError()
	}
	if (synchronous && fact.Channel != "ORIGINAL") || (fact.Channel != "" && fact.Channel != "ORIGINAL") {
		return ports.ChannelRefundResult{}, protocol()
	}
	if (fact.Amount.Currency != "CNY" && (synchronous || fact.Amount.hasCurrency || fact.Amount.Currency != "")) || fact.Amount.Total <= 0 || fact.Amount.Total > p.MaxAmount || fact.Amount.Refund <= 0 || fact.Amount.Refund > fact.Amount.Total {
		return ports.ChannelRefundResult{}, amountError()
	}
	result := ports.ChannelRefundResult{RefundNo: fact.RefundNo, OrderNo: fact.OrderNo, ChannelRefundID: fact.RefundID, TradeNo: fact.TradeNo, AmountFen: fact.Amount.Refund, TotalFen: fact.Amount.Total}
	switch fact.State {
	case "SUCCESS":
		result.State = "succeeded"
	case "PROCESSING", "ABNORMAL":
		result.State = "pending"
	case "CLOSED":
		if !queried {
			return result, protocol()
		}
		result.State = "failed"
	default:
		return result, protocol()
	}
	if fact.CreateTime != "" {
		at, err := time.Parse(time.RFC3339, fact.CreateTime)
		if err != nil || at.IsZero() {
			return result, protocol()
		}
		result.BillAt = &at
	} else if synchronous {
		return result, protocol()
	}
	return result, nil
}

func verifyRefundRequest(result ports.ChannelRefundResult, in ports.ChannelRefund) error {
	if result.State == "not_found" {
		return nil
	}
	if result.AmountFen != in.AmountFen || result.TotalFen != in.TotalFen {
		return amountError()
	}
	if in.TradeNo != "" && result.TradeNo != in.TradeNo {
		return identityError()
	}
	return nil
}

// 仅渠道字段受80字节协议限制；完整运营原因仍保留在本地不可变审计中。
func channelReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) <= 80 {
		return reason
	}
	end := 80
	for !utf8.ValidString(reason[:end]) {
		end--
	}
	return reason[:end]
}
