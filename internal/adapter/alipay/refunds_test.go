package alipay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"drama-agent/internal/application/ports"
)

func refundInput() ports.ChannelRefund {
	return ports.ChannelRefund{OrderNo: orderNo, TradeNo: tradeNo, RefundNo: refundNo, AmountFen: 120, TotalFen: 200, Reason: "自造退款测试"}
}
func TestRefundUnknownCannotBecomeFinalFailure(t *testing.T) {
	cases := []struct {
		name, raw      string
		transportError bool
	}{
		{name: "balance retryable", raw: `{"code":"40004","sub_code":"ACQ.SELLER_BALANCE_NOT_ENOUGH"}`},
		{name: "system error", raw: `{"code":"20000","sub_code":"ACQ.SYSTEM_ERROR"}`},
		{name: "idempotency conflict", raw: `{"code":"40004","sub_code":"ACQ.DISCORDANT_REPEAT_REQUEST"}`},
		{name: "unknown fund change", raw: `{"code":"10000","out_trade_no":"` + orderNo + `","trade_no":"` + tradeNo + `","refund_fee":"1.20","fund_change":"UNKNOWN"}`},
		{name: "success lacks bill timestamp", raw: `{"code":"10000","out_trade_no":"` + orderNo + `","trade_no":"` + tradeNo + `","refund_fee":"1.20","fund_change":"Y"}`},
		{name: "timeout", transportError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				count++
				method, _ := request(t, r)
				if tc.transportError {
					return nil, errors.New("timeout-secret")
				}
				return signedResponse(t, method, tc.raw), nil
			}))
			result, err := c.Refund(context.Background(), refundInput())
			if err == nil || result.State != "" || count != 1 {
				t.Fatalf("%+v %v calls=%d", result, err, count)
			}
		})
	}
}
func TestRefundFinalRejectionRequiresFreshSameNumberAbsence(t *testing.T) {
	cases := []struct {
		name, queryRaw, want string
		queryError           bool
	}{
		{name: "confirmed absent", queryRaw: `{"code":"40004","sub_code":"ACQ.REFUND_NOT_EXIST"}`, want: "failed"},
		{name: "historical success", queryRaw: refundJSON("REFUND_SUCCESS"), want: "succeeded"},
		{name: "no status is unknown", queryRaw: refundJSON("")},
		{name: "query timeout", queryError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				method, biz := request(t, r)
				if stringValue(t, biz, "out_request_no") != refundNo || stringValue(t, biz, "out_trade_no") != orderNo {
					t.Fatal("changed refund identity")
				}
				if method == "alipay.trade.refund" {
					return signedResponse(t, method, `{"code":"40004","sub_code":"ACQ.TRADE_NOT_ALLOW_REFUND"}`), nil
				}
				if tc.queryError {
					return nil, errors.New("timeout")
				}
				return signedResponse(t, method, tc.queryRaw), nil
			}))
			result, err := c.Refund(context.Background(), refundInput())
			if calls != 2 || result.State != tc.want || (tc.want == "" && err == nil) || (tc.want != "" && err != nil) {
				t.Fatalf("%+v %v calls=%d", result, err, calls)
			}
			if tc.want == "failed" && (result.AmountFen != 120 || result.RefundNo != refundNo || result.OrderNo != orderNo) {
				t.Fatal(result)
			}
		})
	}
}
func TestFundChangeNQueriesInsteadOfFailing(t *testing.T) {
	for _, queryRaw := range []string{refundJSON("REFUND_SUCCESS"), refundJSON(""), `{"code":"40004","sub_code":"ACQ.REFUND_NOT_EXIST"}`} {
		calls := 0
		c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			method, _ := request(t, r)
			if method == "alipay.trade.refund" {
				return signedResponse(t, method, `{"code":"10000","out_trade_no":"`+orderNo+`","trade_no":"`+tradeNo+`","refund_fee":"1.20","fund_change":"N"}`), nil
			}
			return signedResponse(t, method, queryRaw), nil
		}))
		result, err := c.Refund(context.Background(), refundInput())
		if calls != 2 || result.State == "failed" || result.State == "not_found" || (strings.Contains(queryRaw, "REFUND_SUCCESS") && (err != nil || result.State != "succeeded")) || (!strings.Contains(queryRaw, "REFUND_SUCCESS") && err == nil) {
			t.Fatalf("%+v %v", result, err)
		}
	}
}
func TestRefundQuery10000WithoutKnownSuccessIsUnknown(t *testing.T) {
	for _, state := range []string{"", "REFUND_PROCESSING", "REFUND_FAIL", "UNKNOWN"} {
		c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			method, biz := request(t, r)
			var options []string
			if json.Unmarshal(biz["query_options"], &options) != nil || len(options) != 1 || options[0] != "gmt_refund_pay" {
				t.Fatal("refund timestamp query option")
			}
			return signedResponse(t, method, refundJSON(state)), nil
		}))
		result, err := c.QueryRefund(context.Background(), refundNo, orderNo)
		if err == nil || result.State != "" {
			t.Fatalf("%+v %v", result, err)
		}
	}
}
func TestRefundIdentityMismatchAndUnsignedErrorsNeverRestore(t *testing.T) {
	cases := []struct {
		name, raw string
		unsigned  bool
	}{
		{name: "wrong order", raw: strings.ReplaceAll(refundJSON("REFUND_SUCCESS"), orderNo, "wrong-order")},
		{name: "wrong request", raw: strings.ReplaceAll(refundJSON("REFUND_SUCCESS"), refundNo, "wrong-refund")},
		{name: "missing trade", raw: strings.ReplaceAll(refundJSON("REFUND_SUCCESS"), tradeNo, "")},
		{name: "excess precision", raw: strings.ReplaceAll(refundJSON("REFUND_SUCCESS"), `"refund_amount":"1.2"`, `"refund_amount":"1.201"`)},
		{name: "missing bill time", raw: strings.ReplaceAll(refundJSON("REFUND_SUCCESS"), `"gmt_refund_pay":"2026-10-20 12:00:00"`, `"gmt_refund_pay":""`)},
		{name: "unsigned final rejection", raw: `{"error_response":{"code":"40004","sub_code":"ACQ.TRADE_NOT_ALLOW_REFUND"}}`, unsigned: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				method, _ := request(t, r)
				if tc.unsigned {
					return rawResponse(tc.raw), nil
				}
				return signedResponse(t, method, tc.raw), nil
			}))
			result, err := c.QueryRefund(context.Background(), refundNo, orderNo)
			if err == nil || result.State != "" {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}
func TestSignedNotFoundQueryAndClose(t *testing.T) {
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		method, _ := request(t, r)
		return signedResponse(t, method, `{"code":"40004","sub_code":"ACQ.TRADE_NOT_EXIST"}`), nil
	}))
	tr, err := c.Query(context.Background(), orderNo)
	if err != nil || tr.State != "not_found" || tr.OrderNo != orderNo {
		t.Fatalf("%+v %v", tr, err)
	}
	if err = c.Close(context.Background(), orderNo); err != nil {
		t.Fatal(err)
	}
	rr, err := c.QueryRefund(context.Background(), refundNo, orderNo)
	if err != nil || rr.State != "not_found" || rr.RefundNo != refundNo {
		t.Fatalf("%+v %v", rr, err)
	}
}

func TestRefundQueryReturnsActualOrderTotalAndTradeNumber(t *testing.T) {
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		method, _ := request(t, r)
		return signedResponse(t, method, refundJSON("REFUND_SUCCESS")), nil
	}))
	result, err := c.QueryRefund(context.Background(), refundNo, orderNo)
	if err != nil || result.TotalFen != 200 || result.TradeNo != tradeNo {
		t.Fatalf("%+v %v", result, err)
	}
	for _, tc := range []struct {
		name, from, to string
		wantError      bool
	}{{"missing total", `"total_amount":"2",`, ``, false}, {"refund exceeds total", `"total_amount":"2"`, `"total_amount":"1"`, true}, {"invalid total precision", `"total_amount":"2"`, `"total_amount":"2.001"`, true}} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				method, _ := request(t, r)
				return signedResponse(t, method, strings.Replace(refundJSON("REFUND_SUCCESS"), tc.from, tc.to, 1)), nil
			}))
			result, err := c.QueryRefund(context.Background(), refundNo, orderNo)
			if tc.wantError {
				if err == nil || result.State != "" {
					t.Fatalf("%+v %v", result, err)
				}
			} else if err != nil || result.TotalFen != 0 {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}
func TestSynchronousRefundDoesNotInventOrderTotal(t *testing.T) {
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		method, _ := request(t, r)
		return signedResponse(t, method, `{"code":"10000","out_trade_no":"`+orderNo+`","trade_no":"`+tradeNo+`","refund_fee":"1.20","fund_change":"Y","gmt_refund_pay":"2026-10-20 12:00:00"}`), nil
	}))
	result, err := c.Refund(context.Background(), refundInput())
	if err != nil || result.TotalFen != 0 || result.TradeNo != tradeNo || result.State != "succeeded" {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestFundChangeNRejectsMismatchedOriginalTradeAndTotal(t *testing.T) {
	for _, change := range []func(string) string{func(s string) string { return strings.ReplaceAll(s, tradeNo, "other-trade") }, func(s string) string { return strings.ReplaceAll(s, `"total_amount":"2"`, `"total_amount":"3"`) }} {
		c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			method, _ := request(t, r)
			if method == "alipay.trade.refund" {
				return signedResponse(t, method, `{"code":"10000","out_trade_no":"`+orderNo+`","trade_no":"`+tradeNo+`","refund_fee":"1.20","fund_change":"N"}`), nil
			}
			return signedResponse(t, method, change(refundJSON("REFUND_SUCCESS"))), nil
		}))
		result, err := c.Refund(context.Background(), refundInput())
		if codeOf(err) != "PAYMENT_IDENTITY_MISMATCH" || result.State != "" {
			t.Fatalf("%+v %v", result, err)
		}
	}
}

func TestQueryRefundPortParameterOrder(t *testing.T) {
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		method, biz := request(t, r)
		if method != "alipay.trade.fastpay.refund.query" {
			t.Fatalf("unexpected method %s", method)
		}
		if stringValue(t, biz, "out_trade_no") != orderNo {
			t.Error("PaymentChannel orderNo must become out_trade_no")
		}
		if stringValue(t, biz, "out_request_no") != refundNo {
			t.Error("PaymentChannel refundNo must become out_request_no")
		}
		return signedResponse(t, method, refundJSON("REFUND_SUCCESS")), nil
	}))
	// 应用跟进与对账均按端口合同先传退款号，再传订单号，不能只验证具体适配器的内部调用。
	var channel ports.PaymentChannel = c
	result, err := channel.QueryRefund(context.Background(), refundNo, orderNo)
	if err != nil || result.RefundNo != refundNo || result.OrderNo != orderNo || result.TradeNo != tradeNo || result.TotalFen != 200 || result.State != "succeeded" {
		t.Fatalf("refund port result %+v error %v", result, err)
	}
}

func TestRefundNegativeResponsesCheckOptionalIdentity(t *testing.T) {
	for _, tc := range []struct{ name, field, value string }{{"wrong order", "out_trade_no", "other-order"}, {"wrong refund", "out_request_no", "other-refund"}, {"wrong trade", "trade_no", "other-trade"}, {"empty order", "out_trade_no", ""}, {"empty refund", "out_request_no", ""}, {"empty trade", "trade_no", ""}} {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			calls := 0
			c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				method, _ := request(t, r)
				if method == "alipay.trade.refund" {
					raw, _ := json.Marshal(map[string]string{"code": "40004", "sub_code": "ACQ.TRADE_NOT_ALLOW_REFUND", tc.field: tc.value})
					return signedResponse(t, method, string(raw)), nil
				}
				return signedResponse(t, method, `{"code":"40004","sub_code":"ACQ.REFUND_NOT_EXIST"}`), nil
			}))
			result, err := c.Refund(context.Background(), refundInput())
			if err == nil || result.State != "" || calls != 1 {
				t.Fatalf("mismatched rejection must not query or fail refund: %+v %v calls=%d", result, err, calls)
			}
		})
		if tc.field == "trade_no" && tc.value != "" {
			continue
		} // 查退款端口没有已知交易号，只能保留实际返回值交应用核对。
		t.Run("not_found/"+tc.name, func(t *testing.T) {
			c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				method, _ := request(t, r)
				raw, _ := json.Marshal(map[string]string{"code": "40004", "sub_code": "ACQ.REFUND_NOT_EXIST", tc.field: tc.value})
				return signedResponse(t, method, string(raw)), nil
			}))
			var channel ports.PaymentChannel = c
			result, err := channel.QueryRefund(context.Background(), refundNo, orderNo)
			if err == nil || result.State != "" {
				t.Fatalf("mismatched absence must not be accepted: %+v %v", result, err)
			}
		})
	}
}
func TestRefundNotFoundPreservesActualTradeNumber(t *testing.T) {
	for _, actual := range []string{tradeNo, "other-trade"} {
		c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			method, _ := request(t, r)
			raw, _ := json.Marshal(map[string]string{"code": "40004", "sub_code": "ACQ.REFUND_NOT_EXIST", "out_trade_no": orderNo, "out_request_no": refundNo, "trade_no": actual})
			return signedResponse(t, method, string(raw)), nil
		}))
		var channel ports.PaymentChannel = c
		result, err := channel.QueryRefund(context.Background(), refundNo, orderNo)
		if err != nil || result.State != "not_found" || result.TradeNo != actual {
			t.Fatalf("absence must preserve actual trade for application check: %+v %v", result, err)
		}
	}
}
func TestRefundRejectionCannotUseWrongTradeAbsence(t *testing.T) {
	c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		method, _ := request(t, r)
		if method == "alipay.trade.refund" {
			return signedResponse(t, method, `{"code":"40004","sub_code":"ACQ.TRADE_NOT_ALLOW_REFUND"}`), nil
		}
		return signedResponse(t, method, `{"code":"40004","sub_code":"ACQ.REFUND_NOT_EXIST","trade_no":"other-trade"}`), nil
	}))
	result, err := c.Refund(context.Background(), refundInput())
	if codeOf(err) != "PAYMENT_IDENTITY_MISMATCH" || result.State != "" {
		t.Fatalf("wrong-trade absence must not restore credits: %+v %v", result, err)
	}
}

func TestRefundNotFoundRetainsAndChecksAmountEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, total, refund string
		totalFen, refundFen int64
		conflict            bool
	}{{"wrong total", "3.00", "1.20", 300, 120, true}, {"wrong refund", "2.00", "1.30", 200, 130, true}, {"matching evidence", "2.00", "1.20", 200, 120, false}} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				method, _ := request(t, r)
				if method == "alipay.trade.refund" {
					return signedResponse(t, method, `{"code":"40004","sub_code":"ACQ.TRADE_NOT_ALLOW_REFUND"}`), nil
				}
				raw, _ := json.Marshal(map[string]string{"code": "40004", "sub_code": "ACQ.REFUND_NOT_EXIST", "out_trade_no": orderNo, "out_request_no": refundNo, "trade_no": tradeNo, "total_amount": tc.total, "refund_amount": tc.refund})
				return signedResponse(t, method, string(raw)), nil
			}))
			var channel ports.PaymentChannel = c
			evidence, err := channel.QueryRefund(context.Background(), refundNo, orderNo)
			if err != nil || evidence.State != "not_found" || evidence.TotalFen != tc.totalFen || evidence.AmountFen != tc.refundFen {
				t.Fatalf("negative response must preserve actual amounts for application: %+v %v", evidence, err)
			}
			result, err := channel.Refund(context.Background(), refundInput())
			if tc.conflict {
				if codeOf(err) != "PAYMENT_IDENTITY_MISMATCH" || result.State != "" {
					t.Fatalf("conflicting negative amounts cannot restore credits: %+v %v", result, err)
				}
			} else if err != nil || result.State != "failed" || result.TotalFen != 200 || result.AmountFen != 120 {
				t.Fatalf("matching negative evidence: %+v %v", result, err)
			}
		})
	}
}
func TestRefundNotFoundRejectsInvalidAmountEvidence(t *testing.T) {
	for _, field := range []string{"total_amount", "refund_amount"} {
		for _, invalid := range []string{"2e0", "2.001", " 2", "-2", "0"} {
			t.Run(field+"/"+invalid, func(t *testing.T) {
				c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
					method, _ := request(t, r)
					raw, _ := json.Marshal(map[string]string{"code": "40004", "sub_code": "ACQ.REFUND_NOT_EXIST", field: invalid})
					return signedResponse(t, method, string(raw)), nil
				}))
				var channel ports.PaymentChannel = c
				result, err := channel.QueryRefund(context.Background(), refundNo, orderNo)
				if err == nil || result.State != "" {
					t.Fatalf("invalid negative amount must not be treated as absent: %+v %v", result, err)
				}
			})
		}
	}
}

func TestRefundAmountsDistinguishMissingFromEmptyOrNull(t *testing.T) {
	for _, phase := range []string{"not_found", "query_success", "refund_success"} {
		fields := []string{"total_amount"}
		if phase == "not_found" {
			fields = append(fields, "refund_amount")
		}
		for _, field := range fields {
			for _, value := range []any{"", nil} {
				label := "empty"
				if value == nil {
					label = "null"
				}
				t.Run(phase+"/"+field+"/"+label, func(t *testing.T) {
					c := newChannel(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
						method, _ := request(t, r)
						base := refundJSON("REFUND_SUCCESS")
						if phase == "not_found" {
							base = `{"code":"40004","sub_code":"ACQ.REFUND_NOT_EXIST"}`
						}
						if phase == "refund_success" {
							base = `{"code":"10000","out_trade_no":"` + orderNo + `","trade_no":"` + tradeNo + `","refund_fee":"1.20","fund_change":"Y","gmt_refund_pay":"2026-10-20 12:00:00"}`
						}
						var payload map[string]any
						if err := json.Unmarshal([]byte(base), &payload); err != nil {
							t.Fatal(err)
						}
						payload[field] = value
						raw, err := json.Marshal(payload)
						if err != nil {
							t.Fatal(err)
						}
						return signedResponse(t, method, string(raw)), nil
					}))
					var channel ports.PaymentChannel = c
					var result ports.ChannelRefundResult
					var err error
					if phase == "refund_success" {
						result, err = channel.Refund(context.Background(), refundInput())
					} else {
						result, err = channel.QueryRefund(context.Background(), refundNo, orderNo)
					}
					if err == nil || result.State != "" {
						t.Fatalf("present empty/null amount is not missing: %+v %v", result, err)
					}
				})
			}
		}
	}
}
