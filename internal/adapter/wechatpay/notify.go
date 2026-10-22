package wechatpay

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"net/http"

	"drama-agent/internal/application/ports"
)

type notification struct {
	EventType    string `json:"event_type"`
	ResourceType string `json:"resource_type"`
	Resource     struct {
		Algorithm      string `json:"algorithm"`
		Ciphertext     string `json:"ciphertext"`
		Nonce          string `json:"nonce"`
		AssociatedData string `json:"associated_data"`
		OriginalType   string `json:"original_type"`
	} `json:"resource"`
}

func (c *Client) ParseNotify(ctx context.Context, in ports.NotifyInput) (ports.PaymentNotify, ports.NotifyAck, error) {
	failure := ports.NotifyAck{Status: http.StatusInternalServerError, ContentType: "application/json", Body: `{"code":"FAIL","message":"处理失败"}`}
	fail := func(err error) (ports.PaymentNotify, ports.NotifyAck, error) {
		return ports.PaymentNotify{}, failure, err
	}
	if ctx.Err() != nil {
		return fail(transportError())
	}
	if int64(len(in.Body)) > c.cfg.NotifyMaxBytes {
		return fail(protocol())
	}
	headers := make(http.Header, len(in.Header))
	for name, value := range in.Header {
		headers.Add(name, value)
	}
	if err := c.verify(headers, in.Body); err != nil {
		return fail(err)
	}
	var notice notification
	if decodeJSON(in.Body, &notice) != nil {
		return fail(protocol())
	}
	if notice.ResourceType != "encrypt-resource" || notice.Resource.Algorithm != "AEAD_AES_256_GCM" {
		return fail(protocol())
	}
	block, err := aes.NewCipher(c.cfg.APIV3Key)
	if err != nil {
		return fail(protocol())
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fail(protocol())
	}
	encrypted, err := base64.StdEncoding.DecodeString(notice.Resource.Ciphertext)
	if err != nil || len(notice.Resource.Nonce) != gcm.NonceSize() {
		return fail(protocol())
	}
	plain, err := gcm.Open(nil, []byte(notice.Resource.Nonce), encrypted, []byte(notice.Resource.AssociatedData))
	if err != nil {
		return fail(signatureError())
	}
	var result ports.PaymentNotify
	switch notice.EventType {
	case "TRANSACTION.SUCCESS":
		if notice.Resource.OriginalType != "transaction" {
			return fail(protocol())
		}
		var fact trade
		if decodeJSON(plain, &fact) != nil {
			return fail(protocol())
		}
		if fact.State != "SUCCESS" {
			return fail(protocol())
		}
		result.Kind = "pay"
		result.Trade, err = c.trade(fact)
	case "REFUND.SUCCESS", "REFUND.ABNORMAL", "REFUND.CLOSED":
		if notice.Resource.OriginalType != "refund" {
			return fail(protocol())
		}
		var fact refund
		if decodeJSON(plain, &fact) != nil {
			return fail(protocol())
		}
		if fact.MchID != c.cfg.MchID {
			return fail(identityError())
		}
		expected := map[string]string{"REFUND.SUCCESS": "SUCCESS", "REFUND.ABNORMAL": "ABNORMAL", "REFUND.CLOSED": "CLOSED"}[notice.EventType]
		if fact.NotifyState != expected {
			return fail(protocol())
		}
		fact.State = fact.NotifyState
		result.Kind = "refund"
		// 通知通常没有create_time，应用会按原号补查；CLOSED也必须查询最新状态才可恢复。
		if fact.State == "CLOSED" {
			if _, err = c.refund(refundWithState(fact, "PROCESSING"), false, false); err != nil {
				return fail(err)
			}
			result.Refund, err = c.QueryRefund(ctx, fact.RefundNo, fact.OrderNo)
			if err == nil {
				err = verifyRefundRequest(result.Refund, ports.ChannelRefund{RefundNo: fact.RefundNo, OrderNo: fact.OrderNo, TradeNo: fact.TradeNo, AmountFen: fact.Amount.Refund, TotalFen: fact.Amount.Total})
			}
		} else {
			result.Refund, err = c.refund(fact, false, false)
		}
	default:
		return fail(protocol())
	}
	if err != nil {
		return fail(err)
	}
	return result, ports.NotifyAck{Status: http.StatusNoContent}, nil
}
func refundWithState(fact refund, state string) refund { fact.State = state; return fact }
