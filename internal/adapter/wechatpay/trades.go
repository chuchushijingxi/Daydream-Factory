package wechatpay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"drama-agent/internal/application/ports"
	p "drama-agent/internal/domain/payment"
)

type amount struct {
	Total       int64  `json:"total"`
	Refund      int64  `json:"refund,omitempty"`
	Currency    string `json:"currency"`
	hasCurrency bool
}

func (v *amount) UnmarshalJSON(raw []byte) error {
	type decodedAmount amount
	var decoded decodedAmount
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*v = amount(decoded)
	_, v.hasCurrency = fields["currency"]
	return nil
}

type trade struct {
	AppID       string `json:"appid"`
	MchID       string `json:"mchid"`
	OrderNo     string `json:"out_trade_no"`
	TradeNo     string `json:"transaction_id"`
	State       string `json:"trade_state"`
	SuccessTime string `json:"success_time"`
	Amount      amount `json:"amount"`
}

func (c *Client) Create(ctx context.Context, in ports.ChannelOrder) (string, error) {
	if !identifier(in.OrderNo, 32) || in.AmountFen <= 0 || in.AmountFen > p.MaxAmount || !in.ExpiresAt.After(c.now()) || strings.TrimSpace(in.Description) == "" {
		return "", protocol()
	}
	body := struct {
		AppID       string `json:"appid"`
		MchID       string `json:"mchid"`
		Description string `json:"description"`
		OrderNo     string `json:"out_trade_no"`
		NotifyURL   string `json:"notify_url"`
		Expires     string `json:"time_expire"`
		Amount      amount `json:"amount"`
	}{c.cfg.AppID, c.cfg.MchID, in.Description, in.OrderNo, c.cfg.NotifyURL, in.ExpiresAt.Format(time.RFC3339), amount{Total: in.AmountFen, Currency: "CNY"}}
	var result struct {
		CodeURL string `json:"code_url"`
	}
	if err := c.api(ctx, http.MethodPost, "/v3/pay/transactions/native", body, &result); err != nil {
		return "", err
	}
	code, err := url.Parse(result.CodeURL)
	if err != nil || code.Scheme != "weixin" || code.Host != "wxpay" || code.User != nil || code.Fragment != "" || len(result.CodeURL) > 4096 {
		return "", protocol()
	}
	return result.CodeURL, nil
}
func (c *Client) Query(ctx context.Context, no string) (ports.ChannelTrade, error) {
	if !identifier(no, 32) {
		return ports.ChannelTrade{}, protocol()
	}
	var fact trade
	err := c.api(ctx, http.MethodGet, "/v3/pay/transactions/out-trade-no/"+cleanPath(no)+"?mchid="+url.QueryEscape(c.cfg.MchID), nil, &fact)
	if hasResponseCode(err, "ORDER_NOT_EXIST") {
		return ports.ChannelTrade{OrderNo: no, State: "notpay"}, nil
	}
	if err != nil {
		return ports.ChannelTrade{}, err
	}
	if fact.OrderNo != no {
		return ports.ChannelTrade{}, identityError()
	}
	return c.trade(fact)
}
func (c *Client) trade(fact trade) (ports.ChannelTrade, error) {
	if fact.AppID != c.cfg.AppID || fact.MchID != c.cfg.MchID || !identifier(fact.OrderNo, 32) {
		return ports.ChannelTrade{}, identityError()
	}
	if fact.Amount.Currency != "CNY" || fact.Amount.Total <= 0 || fact.Amount.Total > p.MaxAmount {
		return ports.ChannelTrade{}, amountError()
	}
	result := ports.ChannelTrade{OrderNo: fact.OrderNo, TradeNo: fact.TradeNo, AmountFen: fact.Amount.Total}
	switch fact.State {
	case "SUCCESS":
		if !identifier(fact.TradeNo, 64) {
			return result, protocol()
		}
		paid, err := time.Parse(time.RFC3339, fact.SuccessTime)
		if err != nil || paid.IsZero() {
			return result, protocol()
		}
		result.PaidAt = &paid
		result.State = "paid"
	case "NOTPAY":
		result.State = "notpay"
	case "CLOSED":
		result.State = "closed"
	default:
		return result, protocol()
	}
	return result, nil
}
func (c *Client) Close(ctx context.Context, no string) error {
	if !identifier(no, 32) {
		return protocol()
	}
	body := struct {
		MchID string `json:"mchid"`
	}{c.cfg.MchID}
	err := c.api(ctx, http.MethodPost, "/v3/pay/transactions/out-trade-no/"+cleanPath(no)+"/close", body, nil)
	if hasResponseCode(err, "ORDER_NOT_EXIST") {
		return nil
	}
	return err
}
