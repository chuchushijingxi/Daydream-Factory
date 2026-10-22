package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/demopay"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/accounts"
	creditsapp "drama-agent/internal/application/credits"
	paymentapp "drama-agent/internal/application/payment"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/payment"
	"drama-agent/internal/testsupport/pgfixture"
	"drama-agent/internal/testsupport/redisruntime"
	"github.com/jackc/pgx/v5"
)

type paymentHTTPStore struct {
	ports.PaymentStore
	unknown atomic.Bool
}

func (s *paymentHTTPStore) CreditPaymentOrder(ctx context.Context, in ports.PaymentConfirmation) (p.Order, error) {
	if s.unknown.Load() {
		return p.Order{}, fault.New("PAYMENT_COMMIT_UNKNOWN", "隔离故障注入")
	}
	return s.PaymentStore.CreditPaymentOrder(ctx, in)
}
func (s *paymentHTTPStore) ConfirmedPaymentCredit(ctx context.Context, no string) (p.Order, bool, error) {
	if s.unknown.Load() {
		return p.Order{}, false, nil
	}
	return s.PaymentStore.ConfirmedPaymentCredit(ctx, no)
}
func TestPaymentRealPGHTTPDemoAndOperations(t *testing.T) {
	address := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if address == "" {
		t.Skip("isolated payment PG/Redis opt-in required")
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() != "127.0.0.1" || !strings.HasPrefix(parsed.Path, "/drama_accounts_test_") {
		t.Fatal("isolated accounts database required")
	}
	ctx := context.Background()
	store, err := pgfixture.Open(t, ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	ids := []string{identity.New("payment_http_buyer"), identity.New("payment_http_other"), identity.New("payment_http_operator")}
	for i, id := range ids {
		role := ports.RoleUser
		if i == 2 {
			role = ports.RoleOperator
		}
		seedPostgresFixtureUser(t, id, role)
	}
	// 沿现有真实Redis会话摘要与PG用户认证，绝不伪造Actor中间件。
	auth := &accounts.Service{Store: store}
	runtime := redisruntime.Connect(t)
	demo, err := demopay.New(store)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &paymentHTTPStore{PaymentStore: store}
	payments := &paymentapp.Service{Store: wrapper, Targets: paymentapp.PersonalTargets{}, Users: store, RateLimiter: runtime, Channels: map[string]ports.PaymentChannel{"demo": demo}, Environment: "development"}
	host, handler := httpapi.New(&projectapp.Service{}, nil, t.TempDir(), 0)
	host.Authenticator = auth
	host.Accounts = auth
	host.Credits = &creditsapp.Service{Ledger: store, Queries: store, Updates: runtime, Users: store}
	host.StreamHeartbeat = appconfig.StreamHeartbeat
	host.StreamWriteTimeout = appconfig.StreamWriteTimeout
	if err = host.InstallPayments(payments, appconfig.PaymentNotifyMaxBytes); err != nil {
		t.Fatal(err)
	}
	defer host.Shutdown(ctx)
	server := httptest.NewServer(handler)
	defer server.Close()
	token := func(index int, audience string) string { return postgresFixtureToken(ids[index], audience) }
	request := func(method, path, session string, body any) (int, []byte) {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		if session != "" {
			addSessionCookie(req, session)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		response, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, response
	}
	expect := func(status int, raw []byte, want int) {
		t.Helper()
		if status != want {
			t.Fatalf("HTTP want %d got %d: %s", want, status, raw)
		}
	}
	operatorToken, buyerToken, otherToken := token(2, accounts.Operations), token(0, accounts.Workspace), token(1, accounts.Workspace)
	opActor := ports.Actor{UserID: ids[2], Role: ports.RoleOperator}
	pkg, err := store.SavePaymentPackage(ctx, p.Package{Name: "测试档位", ID: identity.New("payment_http_package"), OriginalAmountFen: 125, AmountFen: 100, RechargeCredits: 1000, GiftCredits: 100, Enabled: true}, opActor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range host.Routes() {
		if !strings.Contains(route.Pattern, "/payments/") {
			continue
		}
		if route.Access == "public" {
			continue
		}
		method, path, _ := strings.Cut(route.Pattern, " ")
		path = strings.ReplaceAll(strings.ReplaceAll(path, "{no}", "unknown"), "{id}", "unknown")
		status, raw := request(method, path, "", map[string]any{})
		expect(status, raw, 401)
		if route.Access == "operator" {
			for _, deniedToken := range []string{token(0, accounts.Operations), token(2, accounts.Workspace)} {
				status, raw = request(method, path, deniedToken, map[string]any{})
				expect(status, raw, 403)
			}
		}
	}
	createInput := func(user, requestID string) map[string]any {
		return map[string]any{"clientRequestId": requestID, "target": map[string]string{"kind": "personal", "id": user}, "packageId": pkg.ID, "channel": "demo", "agreementVersion": p.AgreementVersion}
	}
	input := createInput(ids[0], identity.New("payment_request"))
	status, raw := request("POST", "/api/payments/orders", buyerToken, input)
	expect(status, raw, 201)
	var order p.Order
	if json.Unmarshal(raw, &order) != nil || order.CodeURL != "demo://"+order.OrderNo || order.OriginalAmountFen != 125 || order.AmountFen != 100 {
		t.Fatal("missing demo checkout", string(raw))
	}
	status, raw = request("POST", "/api/operations/payments/packages", operatorToken, map[string]any{"id": pkg.ID, "revision": pkg.Revision, "name": "改价档位", "originalAmountFen": 250, "amountFen": 200, "rechargeCredits": 2000, "giftCredits": 200, "enabled": true, "sort": 0, "testOnly": false})
	expect(status, raw, 200)
	status, raw = request("GET", "/api/operations/payments/packages", operatorToken, nil)
	expect(status, raw, 200)
	var packageList struct {
		Items []p.Package `json:"items"`
	}
	if json.Unmarshal(raw, &packageList) != nil || len(packageList.Items) == 0 || !bytes.Contains(raw, []byte(`"name":"改价档位"`)) {
		t.Fatal("package list envelope")
	}
	status, raw = request("POST", "/api/payments/orders", buyerToken, input)
	expect(status, raw, 201)
	var duplicate p.Order
	_ = json.Unmarshal(raw, &duplicate)
	if duplicate.OrderNo != order.OrderNo || duplicate.AmountFen != 100 || duplicate.OriginalAmountFen != 125 || duplicate.RechargeCredits != 1000 {
		t.Fatal("HTTP idempotency created new order")
	}
	for _, path := range []string{"/api/payments/orders/" + order.OrderNo, "/api/payments/orders/" + order.OrderNo + "/close", "/api/payments/demo/orders/" + order.OrderNo + "/simulate"} {
		method := "GET"
		body := any(nil)
		if strings.HasSuffix(path, "close") {
			method = "POST"
			body = map[string]any{}
		}
		if strings.HasSuffix(path, "simulate") {
			method = "POST"
			body = map[string]string{"result": "paid"}
		}
		status, raw = request(method, path, otherToken, body)
		expect(status, raw, 403)
	}
	bad := createInput(ids[0], identity.New("team_request"))
	bad["target"] = map[string]string{"kind": "team", "id": "unimplemented-team"}
	status, raw = request("POST", "/api/payments/orders", buyerToken, bad)
	expect(status, raw, 403)
	// 首次订阅只发ready，支付提交后才由原积分触发器/Redis投影发changed。
	streamReq, _ := http.NewRequest("GET", server.URL+"/api/credits/events", nil)
	addSessionCookie(streamReq, buyerToken)
	streamResp, err := server.Client().Do(streamReq)
	if err != nil || streamResp.StatusCode != 200 {
		t.Fatal("SSE open", err)
	}
	defer streamResp.Body.Close()
	events := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(streamResp.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "event: ") {
				events <- strings.TrimPrefix(scanner.Text(), "event: ")
			}
		}
	}()
	await := func(want string) {
		t.Helper()
		select {
		case got := <-events:
			if got != want {
				t.Fatal("SSE", got, want)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("missing SSE", want)
		}
	}
	await("credits.ready")
	status, raw = request("POST", "/api/payments/demo/orders/"+order.OrderNo+"/simulate", buyerToken, map[string]string{"result": "failed"})
	expect(status, raw, 200)
	wallet, err := store.CreditWallet(ctx, ids[0])
	if err != nil || wallet.Balance != 0 {
		t.Fatal("failed simulation credited")
	}
	wrapper.unknown.Store(true)
	status, raw = request("POST", "/api/payments/demo/orders/"+order.OrderNo+"/simulate", buyerToken, map[string]string{"result": "paid"})
	expect(status, raw, 503)
	wallet, err = store.CreditWallet(ctx, ids[0])
	if err != nil || wallet.Balance != 0 {
		t.Fatal("unknown commit claimed credit")
	}
	wrapper.unknown.Store(false)
	status, raw = request("POST", "/api/payments/demo/orders/"+order.OrderNo+"/simulate", buyerToken, map[string]string{"result": "paid"})
	expect(status, raw, 200)
	await("credits.changed")
	status, raw = request("GET", "/api/credits/balance", buyerToken, nil)
	expect(status, raw, 200)
	wallet, err = store.CreditWallet(ctx, ids[0])
	if err != nil || wallet.RechargeBalance != 1000 || wallet.GiftBalance != 100 || wallet.Balance != 1100 {
		t.Fatal("wrong payment wallet", wallet, err)
	}
	var additions int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE operation_id=ANY($1) AND kind='add'`, []string{"recharge:order:" + order.OrderNo, "gift:recharge-bonus:" + order.OrderNo}).Scan(&additions); err != nil || additions != 2 {
		t.Fatal("payment must create two exact additions", additions, err)
	}
	status, raw = request("GET", "/api/payments/orders", otherToken, nil)
	expect(status, raw, 200)
	if strings.Contains(string(raw), order.OrderNo) {
		t.Fatal("order history leaked across buyers")
	}
	status, raw = request("GET", "/api/payments/options", buyerToken, nil)
	expect(status, raw, 200)
	if !bytes.Contains(raw, []byte(`"name":"改价档位"`)) {
		t.Fatal("package name missing from user options", string(raw))
	}
	var publicOptions struct {
		Packages []map[string]any `json:"packages"`
	}
	if err := json.Unmarshal(raw, &publicOptions); err != nil {
		t.Fatal(err)
	}
	for _, v := range publicOptions.Packages {
		if _, ok := v["name"]; !ok {
			t.Fatal("name field missing in public options", v)
		}
	}
	var options paymentapp.Options
	_ = json.Unmarshal(raw, &options)
	if !options.Agreement.Agreed || len(options.Targets) != 1 || options.Targets[0].ID != ids[0] {
		t.Fatal("agreement/target facts not persisted")
	}
	status, raw = request("GET", "/api/operations/payments/orders/"+order.OrderNo+"/refund-quote", operatorToken, nil)
	expect(status, raw, 200)
	var quote p.RefundQuote
	_ = json.Unmarshal(raw, &quote)
	if quote.MaxCredits != 1000 || quote.AmountFen != 100 {
		t.Fatal("refund quote", string(raw))
	}
	status, raw = request("POST", "/api/operations/payments/orders/"+order.OrderNo+"/refunds", operatorToken, map[string]any{"refundId": identity.New("no_reason"), "credits": 500})
	expect(status, raw, 400)
	status, raw = request("POST", "/api/operations/payments/orders/"+order.OrderNo+"/refunds", operatorToken, map[string]any{"refundId": identity.New("refund_request"), "credits": 500, "reason": "隔离误充退款"})
	expect(status, raw, 201)
	var createdRefund p.Refund
	if json.Unmarshal(raw, &createdRefund) != nil {
		t.Fatal("refund response invalid")
	}
	// 只跟进本夹具订单，不领取同一隔离库中其他阶段保留的待处理证据。
	refundResult, e := demo.Refund(ctx, ports.ChannelRefund{RefundNo: createdRefund.RefundNo, OrderNo: order.OrderNo, TradeNo: "demo:" + order.OrderNo, AmountFen: createdRefund.AmountFen, TotalFen: order.AmountFen, Reason: createdRefund.Reason})
	if e != nil {
		t.Fatal(e)
	}
	if _, err = payments.ConfirmRefund(ctx, "demo", refundResult, "refund"); err != nil {
		t.Fatal(err)
	}
	wallet, err = store.CreditWallet(ctx, ids[0])
	if err != nil || wallet.RechargeBalance != 500 || wallet.GiftBalance != 50 {
		t.Fatal("refund did not reclaim proportional classes", wallet, err)
	}
	status, raw = request("GET", "/api/operations/payments/orders/"+order.OrderNo, operatorToken, nil)
	expect(status, raw, 200)
	var detail ports.PaymentOrderDetails
	_ = json.Unmarshal(raw, &detail)
	if detail.Order.Status != p.Refunded || len(detail.Refunds) != 1 || detail.Refunds[0].OperatorID != ids[2] || detail.Refunds[0].Status != "succeeded" {
		t.Fatal("refund progress/audit", string(raw))
	}
	// 配置保存不要求原因；查单保留真实Actor与原因，核查不制造第二笔到账。
	status, raw = request("POST", "/api/operations/payments/orders/"+order.OrderNo+"/query", operatorToken, map[string]string{})
	expect(status, raw, 400)
	status, raw = request("POST", "/api/operations/payments/orders/"+order.OrderNo+"/query", operatorToken, map[string]string{"reason": "隔离重新核查"})
	expect(status, raw, 200)
	status, raw = request("GET", "/api/operations/payments/orders?buyerUserId="+ids[0]+"&channelTradeNo=demo:"+order.OrderNo, operatorToken, nil)
	expect(status, raw, 200)
	var listed ports.Page[p.Order]
	if json.Unmarshal(raw, &listed) != nil || listed.Total != 1 || len(listed.Items) != 1 {
		t.Fatal("operations order filter", string(raw))
	}
	anomalyID := identity.New("payment_http_anomaly")
	if err = store.RecordPaymentAnomaly(ctx, p.Anomaly{ID: anomalyID, OrderNo: order.OrderNo, Channel: "demo", Kind: "notification_invalid", Detail: map[string]any{"stage": "isolated_fixture"}}); err != nil {
		t.Fatal(err)
	}
	status, raw = request("GET", "/api/operations/payments/anomalies?kind=notification_invalid", operatorToken, nil)
	expect(status, raw, 200)
	status, raw = request("POST", "/api/operations/payments/anomalies/"+anomalyID+"/resolve", operatorToken, map[string]string{})
	expect(status, raw, 400)
	status, raw = request("POST", "/api/operations/payments/anomalies/"+anomalyID+"/resolve", operatorToken, map[string]string{"note": "隔离核查完成"})
	expect(status, raw, 200)
	var resolved p.Anomaly
	if json.Unmarshal(raw, &resolved) != nil || resolved.ResolvedBy != ids[2] || resolved.Note != "隔离核查完成" {
		t.Fatal("resolve lost audit", string(raw))
	}
	settings, err := store.PaymentSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status, raw = request("POST", "/api/operations/payments/settings", operatorToken, map[string]any{"revision": settings.Revision, "dailyLimitFen": settings.DailyLimitFen})
	expect(status, raw, 200)
	var audited int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.payment_events WHERE operator_id=$1 AND ((source IN ('package','settings') AND outcome='saved' AND reason='') OR (source='query' AND outcome='requested' AND reason='隔离重新核查'))`, ids[2]).Scan(&audited); err != nil || audited != 4 {
		// 初始化档位、HTTP改价、限额保存与查单各一条，配置原因不伪造。
		t.Fatal("operator actions missing immutable audit", audited, err)
	}
	status, raw = request("POST", "/api/operations/payments/settings", operatorToken, map[string]any{"revision": settings.Revision + 1, "dailyLimitFen": 0, "reason": "removed input"})
	expect(status, raw, 400)
	status, raw = request("POST", "/api/operations/payments/packages", operatorToken, map[string]any{"reason": "removed input"})
	expect(status, raw, 400)
	status, raw = request("POST", "/api/operations/payments/settings", operatorToken, map[string]any{"revision": 0, "dailyLimitFen": 0})
	expect(status, raw, 409)
	status, raw = request("POST", "/api/payments/orders", otherToken, map[string]any{"clientRequestId": "forged", "buyerUserId": ids[0]})
	expect(status, raw, 400)
	status, raw = request("GET", "/api/operations/payments/orders?pageSize=101", operatorToken, nil)
	expect(status, raw, 400)
	status, raw = request("GET", "/api/payments/orders?page=1&page=2", buyerToken, nil)
	expect(status, raw, 400)
	// 新的隔离用户走真实Redis支付限流，换单必须用新幂等号；旧成功请求回读不消耗额度。
	rateUser := identity.New("payment_http_rate")
	seedPostgresFixtureUser(t, rateUser, ports.RoleUser)
	rateToken := postgresFixtureToken(rateUser, accounts.Workspace)
	var lastSuccessful map[string]any
	for i := 0; i < 6; i++ {
		in := createInput(rateUser, identity.New("rate_request"))
		status, raw = request("POST", "/api/payments/orders", rateToken, in)
		if i < 5 {
			expect(status, raw, 201)
			lastSuccessful = in
		} else {
			expect(status, raw, 429)
			var limitError map[string]string
			_ = json.Unmarshal(raw, &limitError)
			if limitError["code"] != "RATE_LIMITED" {
				t.Fatal("not payment Redis limiter", string(raw))
			}
		}
	}
	status, raw = request("POST", "/api/payments/orders", rateToken, lastSuccessful)
	expect(status, raw, 201)
	var pendingCount int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.payment_orders WHERE buyer_user_id=$1 AND status='pending'`, rateUser).Scan(&pendingCount); err != nil || pendingCount != 1 {
		t.Fatal("rate/change order pending invariant", pendingCount, err)
	}
	// 无渠道正式装配仍提供空选项；演示路径对匿名与已登录均确实不存在。
	noHost, noHandler := httpapi.New(&projectapp.Service{}, nil, t.TempDir(), 0)
	defer noHost.Shutdown(ctx)
	noHost.Authenticator = auth
	if err = noHost.InstallPayments(&paymentapp.Service{Store: store, Targets: paymentapp.PersonalTargets{}, Channels: map[string]ports.PaymentChannel{}, Environment: "production"}, appconfig.PaymentNotifyMaxBytes); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"", buyerToken} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/payments/demo/orders/no/simulate", strings.NewReader(`{"result":"paid"}`))
		if session != "" {
			addSessionCookie(r, session)
		}
		w := httptest.NewRecorder()
		noHandler.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("production demo route exists", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8787/api/payments/options", nil)
	addSessionCookie(r, buyerToken)
	w := httptest.NewRecorder()
	noHandler.ServeHTTP(w, r)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &options) != nil || len(options.Channels) != 0 {
		t.Fatal("no-channel options", w.Code, w.Body.String())
	}
	productionHost, _ := httpapi.New(&projectapp.Service{}, nil, t.TempDir(), 0)
	defer productionHost.Shutdown(ctx)
	if err = productionHost.InstallPayments(&paymentapp.Service{Channels: map[string]ports.PaymentChannel{"demo": demo}, Environment: "production"}, appconfig.PaymentNotifyMaxBytes); err == nil {
		t.Fatal("production demo installation succeeded")
	}
	for _, route := range productionHost.Routes() {
		if strings.Contains(route.Pattern, "/payments/demo/") {
			t.Fatal("failed production install registered demo route")
		}
	}
	t.Log("隔离PG/Redis真实认证：全部支付路由权限、幂等、失败/提交未知不误到账、两笔积分/SSE、比例退款/运营审计、严格输入、真实Redis五次限流、无渠道及正式演示404/启动拒绝已验证；团队项依赖团队空间")
}
