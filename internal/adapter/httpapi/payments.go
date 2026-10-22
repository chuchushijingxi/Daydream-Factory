package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	paymentapp "drama-agent/internal/application/payment"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

// InstallPayments 只在Listen前装配一次；模拟路由仅在真实装配演示渠道后注册。
func (h *Handler) InstallPayments(service *paymentapp.Service, notifyMaxBytes int64) error {
	if service == nil || h.registry == nil || h.Payments != nil || notifyMaxBytes <= 0 {
		return fault.New("PAYMENT_CONFIG_INVALID", "支付HTTP装配无效")
	}
	if demo := service.Channels["demo"]; demo != nil {
		if service.Environment != "development" && service.Environment != "staging" {
			return fault.New("PAYMENT_CONFIG_INVALID", "正式环境不能启用演示支付")
		}
		if _, ok := demo.(paymentapp.DemoSimulator); !ok {
			return fault.New("PAYMENT_CONFIG_INVALID", "演示支付能力无效")
		}
	}
	h.Payments = service
	h.paymentNotifyMaxBytes = notifyMaxBytes
	if service.Channels["demo"] != nil {
		h.registry.HandleFunc("POST /api/payments/demo/orders/{no}/simulate", h.paymentSimulate)
	}
	return nil
}
func (h *Handler) paymentReady(w http.ResponseWriter) bool {
	if h.Payments == nil {
		fail(w, fault.New("PAYMENT_CHANNEL_UNAVAILABLE", "支付服务尚未配置"))
		return false
	}
	return true
}
func (h *Handler) registerPayments(root *routes) {
	root.HandleFunc("GET /api/payments/options", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		out, err := h.Payments.Options(r.Context(), actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
	root.HandleFunc("POST /api/payments/orders", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		var in struct {
			ClientRequestID string `json:"clientRequestId"`
			Target          struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"target"`
			PackageID        string `json:"packageId"`
			Channel          string `json:"channel"`
			AgreementVersion string `json:"agreementVersion"`
		}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		out, err := h.Payments.CreateOrder(r.Context(), actor(r), paymentapp.CreateOrderInput{ClientRequestID: in.ClientRequestID, Target: p.Target{Kind: in.Target.Kind, ID: in.Target.ID}, PackageID: in.PackageID, Channel: in.Channel, AgreementVersion: in.AgreementVersion})
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 201, out)
	})
	root.HandleFunc("GET /api/payments/orders/{no}", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		out, err := h.Payments.GetOrder(r.Context(), actor(r), r.PathValue("no"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
	root.HandleFunc("POST /api/payments/orders/{no}/close", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		var in struct{}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		out, err := h.Payments.CloseOrder(r.Context(), actor(r), r.PathValue("no"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
	root.HandleFunc("GET /api/payments/orders", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		if _, err := paymentQuery(r, "page", "pageSize"); err != nil {
			fail(w, err)
			return
		}
		page, size, err := page(r)
		if err != nil {
			fail(w, err)
			return
		}
		out, err := h.Payments.ListOrders(r.Context(), actor(r), page, size)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
	for _, id := range []string{"wechatpay", "alipay"} {
		root.public().HandleFunc("POST /api/payments/notify/"+id, func(w http.ResponseWriter, r *http.Request) { h.paymentNotify(w, r, id) })
	}
	ops := root.operations()
	ops.HandleFunc("GET /api/operations/payments/orders", h.paymentOperationsOrders)
	ops.HandleFunc("GET /api/operations/payments/orders/{no}", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		out, err := h.Payments.OrderDetails(r.Context(), actor(r), r.PathValue("no"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
	ops.HandleFunc("POST /api/operations/payments/orders/{no}/query", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		var in struct {
			Reason string `json:"reason"`
		}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		out, err := h.Payments.QueryOrder(r.Context(), actor(r), r.PathValue("no"), in.Reason)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
	ops.HandleFunc("GET /api/operations/payments/orders/{no}/refund-quote", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		query, err := paymentQuery(r, "credits")
		if err != nil {
			fail(w, err)
			return
		}
		var credits int64
		if raw := query.Get("credits"); raw != "" {
			credits, err = strconv.ParseInt(raw, 10, 64)
		}
		if err != nil {
			fail(w, fault.ErrInvalid)
			return
		}
		out, err := h.Payments.RefundQuote(r.Context(), actor(r), r.PathValue("no"), credits)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
	ops.HandleFunc("POST /api/operations/payments/orders/{no}/refunds", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		var in paymentapp.CreateRefundInput
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		out, err := h.Payments.CreateRefund(r.Context(), actor(r), r.PathValue("no"), in)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 201, out)
	})
	ops.HandleFunc("GET /api/operations/payments/anomalies", h.paymentAnomalies)
	ops.HandleFunc("POST /api/operations/payments/anomalies/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		var in struct {
			Note string `json:"note"`
		}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		out, err := h.Payments.ResolveAnomaly(r.Context(), actor(r), r.PathValue("id"), in.Note)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
	ops.HandleFunc("GET /api/operations/payments/packages", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		out, err := h.Payments.Packages(r.Context(), actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, map[string]any{"items": out})
	})
	ops.HandleFunc("POST /api/operations/payments/packages", h.paymentSavePackage)
	ops.HandleFunc("GET /api/operations/payments/settings", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		out, err := h.Payments.Settings(r.Context(), actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
	ops.HandleFunc("POST /api/operations/payments/settings", func(w http.ResponseWriter, r *http.Request) {
		if !h.paymentReady(w) {
			return
		}
		var in struct {
			Revision      uint64 `json:"revision"`
			DailyLimitFen int64  `json:"dailyLimitFen"`
		}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		out, err := h.Payments.SaveSettings(r.Context(), actor(r), p.Settings{Revision: in.Revision, DailyLimitFen: in.DailyLimitFen})
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, out)
	})
}
func (h *Handler) paymentSimulate(w http.ResponseWriter, r *http.Request) {
	if !h.paymentReady(w) {
		return
	}
	var in struct {
		Result string `json:"result"`
	}
	if err := readPayload(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	out, err := h.Payments.Simulate(r.Context(), actor(r), r.PathValue("no"), in.Result)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, out)
}
func paymentQuery(r *http.Request, allowed ...string) (url.Values, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fault.ErrInvalid
	}
	keys := map[string]bool{"audience": true}
	for _, key := range allowed {
		keys[key] = true
	}
	for key, values := range query {
		if !keys[key] || len(values) != 1 {
			return nil, fault.ErrInvalid
		}
	}
	return query, nil
}
func (h *Handler) paymentOperationsOrders(w http.ResponseWriter, r *http.Request) {
	if !h.paymentReady(w) {
		return
	}
	q, err := paymentQuery(r, "page", "pageSize", "orderNo", "channelTradeNo", "buyerUserId", "targetKind", "targetId", "channel", "status", "start", "end")
	if err != nil {
		fail(w, err)
		return
	}
	page, size, err := page(r)
	if err != nil {
		fail(w, err)
		return
	}
	filter := ports.PaymentOrderFilter{Page: page, PageSize: size, OrderNo: q.Get("orderNo"), TradeNo: q.Get("channelTradeNo"), BuyerUserID: q.Get("buyerUserId"), TargetKind: q.Get("targetKind"), TargetID: q.Get("targetId"), Channel: q.Get("channel"), Status: q.Get("status")}
	for name, target := range map[string]**time.Time{"start": &filter.Start, "end": &filter.End} {
		if raw := q.Get(name); raw != "" {
			at, e := time.Parse(time.RFC3339Nano, raw)
			if e != nil {
				fail(w, fault.ErrInvalid)
				return
			}
			*target = &at
		}
	}
	out, err := h.Payments.Orders(r.Context(), actor(r), filter)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, out)
}
func (h *Handler) paymentAnomalies(w http.ResponseWriter, r *http.Request) {
	if !h.paymentReady(w) {
		return
	}
	q, err := paymentQuery(r, "page", "pageSize", "status", "kind", "channel")
	if err != nil {
		fail(w, err)
		return
	}
	page, size, err := page(r)
	if err != nil {
		fail(w, err)
		return
	}
	out, err := h.Payments.Anomalies(r.Context(), actor(r), ports.PaymentAnomalyFilter{Page: page, PageSize: size, Status: q.Get("status"), Kind: q.Get("kind"), Channel: q.Get("channel")})
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, out)
}
func (h *Handler) paymentSavePackage(w http.ResponseWriter, r *http.Request) {
	if !h.paymentReady(w) {
		return
	}
	var in struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		OriginalAmountFen int64  `json:"originalAmountFen"`
		Revision          uint64 `json:"revision"`
		AmountFen         int64  `json:"amountFen"`
		RechargeCredits   int64  `json:"rechargeCredits"`
		GiftCredits       int64  `json:"giftCredits"`
		Enabled           bool   `json:"enabled"`
		Sort              int    `json:"sort"`
		TestOnly          bool   `json:"testOnly"`
	}
	if err := readPayload(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	out, err := h.Payments.SavePackage(r.Context(), actor(r), p.Package{ID: in.ID, Name: in.Name, OriginalAmountFen: in.OriginalAmountFen, Revision: in.Revision, AmountFen: in.AmountFen, RechargeCredits: in.RechargeCredits, GiftCredits: in.GiftCredits, Enabled: in.Enabled, Sort: in.Sort, TestOnly: in.TestOnly})
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, out)
}
func paymentNotifyPath(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/api/payments/notify/wechatpay" || r.URL.Path == "/api/payments/notify/alipay")
}
func notifyFailure(w http.ResponseWriter, channel string, status int) {
	if channel == "alipay" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "failure")
		return
	}
	write(w, status, map[string]string{"code": "FAIL", "message": "支付通知处理失败，请重试"})
}
func (h *Handler) notifyRejected(ctx context.Context, channel string, err error) error {
	kind := "notification_invalid"
	var f *fault.Error
	if errors.As(err, &f) {
		switch f.Code {
		case "PAYMENT_SIGNATURE_INVALID":
			kind = "signature_invalid"
		case "PAYMENT_IDENTITY_MISMATCH":
			kind = "identity_mismatch"
		case "PAYMENT_INVALID_AMOUNT":
			kind = "amount_mismatch"
		}
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return h.Payments.RejectNotification(auditCtx, channel, kind)
}
func (h *Handler) paymentNotify(w http.ResponseWriter, r *http.Request, channel string) {
	if h.Payments == nil || h.paymentNotifyMaxBytes <= 0 {
		notifyFailure(w, channel, 503)
		return
	}
	ch := h.Payments.Channels[channel]
	if ch == nil || ch.ID() != channel {
		notifyFailure(w, channel, 503)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.paymentNotifyMaxBytes))
	if err != nil {
		_ = h.notifyRejected(r.Context(), channel, fault.ErrInvalid)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			notifyFailure(w, channel, 413)
		} else {
			notifyFailure(w, channel, 400)
		}
		return
	}
	headers := map[string]string{}
	for name, values := range r.Header {
		canonical := http.CanonicalHeaderKey(name)
		signatureHeader := strings.HasPrefix(strings.ToLower(canonical), "wechatpay-")
		critical := signatureHeader || canonical == "Content-Type"
		_, seen := headers[canonical]
		if critical && (len(values) != 1 || seen) {
			err = fault.ErrInvalid
			if signatureHeader {
				err = fault.New("PAYMENT_SIGNATURE_INVALID", "支付通知签名头无效")
			}
			_ = h.notifyRejected(r.Context(), channel, err)
			notifyFailure(w, channel, 400)
			return
		}
		if len(values) > 0 {
			headers[canonical] = values[0]
		}
	}
	notice, ack, err := ch.ParseNotify(r.Context(), ports.NotifyInput{Header: headers, Body: raw})
	if err != nil {
		auditErr := h.notifyRejected(r.Context(), channel, err)
		if auditErr != nil {
			notifyFailure(w, channel, 503)
		} else {
			notifyFailure(w, channel, 400)
		}
		return
	}
	if err = h.Payments.ConfirmNotification(r.Context(), channel, notice); err != nil {
		status, _ := errorResponse(err)
		if status < 400 {
			status = 500
		}
		notifyFailure(w, channel, status)
		return
	}
	// Ack仅在应用确认PG事务提交/幂等回读后发送；解析成功本身不表示到账。
	if ack.Status < 200 || ack.Status >= 300 {
		notifyFailure(w, channel, 500)
		return
	}
	if ack.ContentType != "" {
		w.Header().Set("Content-Type", ack.ContentType)
	}
	w.WriteHeader(ack.Status)
	if ack.Body != "" {
		_, _ = io.WriteString(w, ack.Body)
	}
}
