package httpapi_test

import (
	"bytes"
	"context"
	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/accounts"
	creditapp "drama-agent/internal/application/credits"
	"drama-agent/internal/application/modelusage"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type frozenFixturePrices struct {
	mu      sync.Mutex
	rule    c.PriceRule
	version uint64
}

func (p *frozenFixturePrices) CreditPrice(context.Context, string) (c.PriceRule, uint64, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rule, p.version, "fixture-connection", nil
}
func (p *frozenFixturePrices) set(rule c.PriceRule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rule = rule
	p.version++
}
func TestIdentityOwnerBillingAndUsageRealPGHTTP(t *testing.T) {
	path := os.Getenv("DRAMA_TEST_CONFIG_FILE")
	if path == "" {
		t.Skip("dedicated PG/S3 required")
	}
	cfg, e := appconfig.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(cfg.Database.URL)
	if !strings.HasPrefix(u.Path, "/drama_accounts_test_") || !strings.HasPrefix(cfg.Storage.Bucket, "drama-script-test-accounts-") {
		t.Skip("dedicated accounts resources required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	store, e := pgfixture.Open(t, ctx, cfg.Database.URL)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	conn, e := pgx.Connect(ctx, cfg.Database.URL)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close(context.Background())
	accountService := &accounts.Service{Store: store, Hasher: passwordhash.Bcrypt{}}
	newUser := func() accounts.User {
		email := identity.New("http") + "@example.test"
		codeID := accounts.RandomToken()
		if e := store.PutVerification(ctx, codeID, email, "register", accounts.Digest(codeID+":163842"), time.Now().Add(time.Minute)); e != nil {
			t.Fatal(e)
		}
		store.ActivateVerification(ctx, codeID, true)
		_, raw, e := accountService.CreateCode(ctx, ports.Actor{UserID: "fixture-op", Role: ports.RoleOperator}, time.Now().Add(time.Hour))
		if e != nil {
			t.Fatal(e)
		}
		user, e := accountService.Register(ctx, email, "测试用户", "fixture-password-1234", "163842", raw)
		if e != nil {
			t.Fatal(e)
		}
		return user
	}
	a, b := newUser(), newUser()
	op, _, e := accountService.InitializeOperator(ctx, "fixture-operator-password-123")
	if e != nil {
		t.Fatal(e)
	}
	objects, e := s3objects.New(cfg.Storage)
	if e != nil {
		t.Fatal(e)
	}
	if e = objects.EnsureBucket(ctx); e != nil {
		t.Fatal(e)
	}
	files := &uploadapp.Service{Store: store, Objects: objects, CommitCheckTimeout: time.Second, CleanupTimeout: time.Second}
	model := &protocolModel{}
	prices := &frozenFixturePrices{rule: c.PriceRule{UnitPrice: 2}, version: 1}
	service := &app.Service{Store: store, Files: files, Credits: store, Prices: prices, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: model}, Authorize: freeAuthorization}
	host, handler := httpapi.New(service, nil, "../../../demo", 0)
	host.Files = files
	host.Accounts = accountService
	host.Authenticator = accountService
	host.Credits = &creditapp.Service{Ledger: store, Queries: store, Users: store}
	host.Usage = &modelusage.Service{Queries: store}
	defer host.Shutdown(context.Background())
	request := func(method, path, token string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, bytes.NewReader(raw))
		if token != "" {
			addSessionCookie(req, token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	login := func(email, password, audience string) string {
		w := request("POST", "/api/auth/login", "", map[string]string{"email": email, "password": password, "audience": audience})
		if w.Code != 200 {
			t.Fatal("login", w.Code, w.Body.String())
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
			t.Fatal("cookie contract")
		}
		if strings.Contains(w.Body.String(), cookies[0].Value) {
			t.Fatal("token in JSON")
		}
		return cookies[0].Value
	}
	tokenA := login(a.Email, "fixture-password-1234", "workspace")
	tokenB := login(b.Email, "fixture-password-1234", "workspace")
	tokenOp := login(op.Email, "fixture-operator-password-123", "operations")
	t.Run("anonymous-and-role-boundaries", func(t *testing.T) {
		for _, route := range host.Routes() {
			if route.Access == "public" {
				continue
			}
			method, path, _ := strings.Cut(route.Pattern, " ")
			for strings.Contains(path, "{") {
				i := strings.Index(path, "{")
				j := strings.Index(path[i:], "}") + i
				path = path[:i] + "unknown" + path[j+1:]
			}
			if w := request(method, path, "", map[string]any{}); w.Code != 401 {
				t.Fatal(route.Pattern, w.Code)
			}
		}
		if w := request("GET", "/api/model-config", tokenA, nil); w.Code != 403 {
			t.Fatal(w.Code)
		}
		if w := request("GET", "/api/operations/model-usage", tokenA, nil); w.Code != 403 {
			t.Fatal(w.Code)
		}
	})
	upload := func(token, text string) httpapi.FileView {
		body, kind := multipartFile(t, "private-script.txt", []byte(text), nil)
		req := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/upload", bytes.NewReader(body))
		req.Header.Set("Content-Type", kind)
		addSessionCookie(req, token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var out httpapi.FileView
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("upload", w.Code, w.Body.String())
		}
		return out
	}
	source := "祖父在工具间修理钟表，手中拿着铜钥匙。\n"
	fileA := upload(tokenA, source)
	fileB := upload(tokenB, "乙用户的私有原文和私有标题。")
	settings := d.ProjectSettings{LLMModel: "deepseek-flash"}
	q := request("POST", "/api/projects/quote", tokenA, map[string]any{"fileId": fileA.FileID, "settings": settings})
	var quote app.ChargeQuote
	if q.Code != 200 || json.Unmarshal(q.Body.Bytes(), &quote) != nil {
		t.Fatal("quote", q.Code, q.Body.String())
	}
	balance, _ := store.Balance(ctx, a.ID)
	if balance != 0 {
		t.Fatal("quote charged user")
	}
	if _, e = store.Add(ctx, c.Change{AccountID: a.ID, Amount: 100, OperationID: identity.New("grant"), Source: "fixture", Subject: op.ID, Reason: "fixture", Category: c.Gift}); e != nil {
		t.Fatal(e)
	}
	title := "PRIVATE-TITLE-" + identity.New("private")
	created := request("POST", "/api/projects", tokenA, map[string]any{"fileId": fileA.FileID, "title": title, "settings": settings, "billingBasis": quote.Basis})
	var project struct {
		ID string `json:"id"`
	}
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &project) != nil {
		t.Fatal("create", created.Code, created.Body.String())
	}
	if e = host.WaitUntilIdle(ctx); e != nil {
		t.Fatal(e)
	}
	saved, e := store.Snapshot(ctx, project.ID)
	if e != nil {
		t.Fatal(e)
	}
	t.Run("credit-http-integers-and-role-before-ledger", func(t *testing.T) {
		for _, amount := range []any{1.5, 0, -1, int64(9007199254740992)} {
			w := request("POST", "/api/operations/users/"+a.ID+"/credits", tokenOp, map[string]any{"kind": "add", "amount": amount, "operationId": identity.New("invalid"), "reason": "fixture"})
			if w.Code != 400 {
				t.Fatal("invalid integer accepted", w.Code, w.Body.String())
			}
		}
		w := request("POST", "/api/operations/users/"+b.ID+"/credits", tokenA, map[string]any{"kind": "add", "amount": 10, "operationId": identity.New("forbidden"), "reason": "fixture"})
		if w.Code != 403 {
			t.Fatal("role after ledger", w.Code, w.Body.String())
		}
		w = request("GET", "/api/credits/balance?userId="+b.ID, tokenA, nil)
		var body struct {
			Balance int64 `json:"balance"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		actual, _ := store.Balance(ctx, a.ID)
		if body.Balance != actual {
			t.Fatal("self-reported identity read another wallet")
		}
	})

	t.Run("natural-business-and-one-charge-multiple-runs", func(t *testing.T) {
		if saved.OwnerID != a.ID || len(saved.Runs) != 2 || saved.LatestRun().Status != d.RunCompleted || saved.Runs[0].Credit == nil || saved.Runs[1].Credit.ID != saved.Runs[0].Credit.ID {
			raw, _ := json.Marshal(saved.Runs)
			t.Fatal("chain", string(raw))
		}
		if len(saved.Assets) == 0 || saved.Assets[0].Name != "祖父" || len(saved.VisualPrompts) == 0 {
			t.Fatal("expected fixture content not preserved")
		}
		entries, n, e := store.CreditEntries(ctx, a.ID, 1, 100)
		if e != nil || n != 2 {
			t.Fatal(n, e)
		}
		var debit int64
		for _, v := range entries {
			if v.Kind == "deduct" {
				debit += v.Amount
			}
		}
		balance, _ := store.Balance(ctx, a.ID)
		if debit != quote.Amount || balance != 100-quote.Amount {
			t.Fatal(debit, balance, quote.Amount)
		}
	})
	t.Run("AB-operator-content-files-range-head-and-reference-isolation", func(t *testing.T) {
		paths := []string{"/api/projects/" + project.ID, "/api/projects/" + project.ID + "/source", "/api/projects/" + project.ID + "/source/file", "/api/projects/" + project.ID + "/calls", "/api/projects/" + project.ID + "/storyboard", "/api/files/" + fileA.FileID, "/api/files/" + fileA.FileID + "/original", "/api/files/" + fileA.FileID + "/thumbnail"}
		for _, token := range []string{tokenB, tokenOp} {
			for _, path := range paths {
				for _, method := range []string{"GET", "HEAD"} {
					w := request(method, path, token, nil)
					if w.Code != 404 {
						t.Fatal(method, path, w.Code, w.Body.String())
					}
					if strings.Contains(w.Body.String(), source) || strings.Contains(w.Body.String(), title) {
						t.Fatal("private content leaked")
					}
				}
			}
		}
		for _, token := range []string{tokenB, tokenOp} {
			w := request("GET", "/api/projects", token, nil)
			if w.Code != 200 || strings.Contains(w.Body.String(), project.ID) {
				t.Fatal("owner list", w.Code, w.Body.String())
			}
		}
		if w := request("POST", "/api/projects/quote", tokenA, map[string]any{"fileId": fileB.FileID, "settings": settings}); w.Code != 404 {
			t.Fatal("cross-user file reference", w.Code, w.Body.String())
		}
		if w := request("POST", "/api/projects/"+project.ID+"/runs", tokenB, map[string]any{"revision": saved.Revision, "billingBasis": "forged"}); w.Code != 404 {
			t.Fatal("foreign write", w.Code, w.Body.String())
		}
		req := httptest.NewRequest("GET", "http://127.0.0.1:8787"+fileA.OriginalURL, nil)
		addSessionCookie(req, tokenA)
		req.Header.Set("Range", "bytes=0-2")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 206 || w.Body.Len() != 3 {
			t.Fatal("owned range", w.Code, w.Body.String())
		}
		before := saved
		before.OwnerID = b.ID
		before.Revision++
		if e := store.Commit(ctx, before, saved.Revision); e == nil {
			t.Fatal("owner transferred through commit")
		}
		after, _ := store.Snapshot(ctx, saved.ID)
		if after.OwnerID != a.ID || after.Revision != saved.Revision {
			t.Fatal("owner/revision not atomic")
		}
	})
	t.Run("operations-projection-and-identical-filter-stats", func(t *testing.T) {
		for _, view := range []string{"tasks", "calls"} {
			w := request("GET", "/api/operations/model-usage?view="+view+"&userId="+url.QueryEscape(a.ID), tokenOp, nil)
			if w.Code != 200 {
				t.Fatal("usage", w.Code, w.Body.String())
			}
			raw := w.Body.String()
			for _, private := range []string{title, source, "祖父", "request\":{", "password", "secret", "objectKey", "http://"} {
				if strings.Contains(raw, private) {
					t.Fatal("usage leaked", private, raw)
				}
			}
			var got modelusage.Result
			if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Total == 0 || got.Total != got.Stats.Count {
				t.Fatal(raw)
			}
			if view == "tasks" && (got.Total != 1 || got.Stats.Charged != quote.Amount) {
				t.Fatal("task billing", raw)
			}
			for _, search := range []string{a.ID, got.Items[0].TaskID} {
				w := request("GET", "/api/operations/model-usage?view="+view+"&q="+url.QueryEscape(" "+search+" "), tokenOp, nil)
				var unified modelusage.Result
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &unified) != nil || unified.Total != got.Total || unified.Stats != got.Stats {
					t.Fatal("unified HTTP user/task lookup changed statistics", w.Code, w.Body.String())
				}
			}
			if view == "calls" {
				callID := got.Items[0].ID
				w := request("GET", "/api/operations/model-usage?view=tasks&q="+url.QueryEscape(callID), tokenOp, nil)
				var parent modelusage.Result
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &parent) != nil || parent.Total != 1 || parent.Items[0].TaskID != got.Items[0].TaskID || parent.Stats.Charged != quote.Amount {
					t.Fatal("call lookup did not return its single parent task and frozen charge", w.Code, w.Body.String())
				}
			}
		}
	})
	t.Run("cancel-refunds-frozen-charge-and-late-cost-does-not-restore-output", func(t *testing.T) {
		current, e := store.Snapshot(ctx, project.ID)
		if e != nil {
			t.Fatal(e)
		}
		target := current.Assets[0].ID
		q := request("POST", "/api/projects/"+project.ID+"/quote", tokenA, map[string]any{"revision": current.Revision, "operation": "single-visual", "assetId": target})
		var quote app.ChargeQuote
		if q.Code != 200 || json.Unmarshal(q.Body.Bytes(), &quote) != nil {
			t.Fatal(q.Code, q.Body.String())
		}
		barrier := &delayedFixtureModel{delegate: model, entered: make(chan struct{}), release: make(chan struct{})}
		var once sync.Once
		defer once.Do(func() { close(barrier.release) })
		service.Models = &modelregistry.Registry{Model: barrier}
		defer func() { service.Models = &modelregistry.Registry{Model: model}; prices.set(c.PriceRule{UnitPrice: 2}) }()
		w := request("POST", "/api/projects/"+project.ID+"/assets/"+target+"/visual-prompt-runs", tokenA, map[string]any{"revision": current.Revision, "billingBasis": quote.Basis})
		if w.Code != 202 {
			t.Fatal("start", w.Code, w.Body.String())
		}
		select {
		case <-barrier.entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		active, e := store.Snapshot(ctx, project.ID)
		if e != nil {
			t.Fatal(e)
		}
		charge := active.LatestRun().Credit
		if charge == nil {
			t.Fatal("no frozen charge")
		}
		prices.set(c.PriceRule{UnitPrice: 99})
		w = request("POST", "/api/projects/"+project.ID+"/cancel", tokenA, map[string]any{"revision": active.Revision})
		if w.Code != 200 {
			t.Fatal("cancel", w.Code, w.Body.String())
		}
		once.Do(func() { close(barrier.release) })
		if e = host.WaitUntilIdle(ctx); e != nil {
			t.Fatal(e)
		}
		service.Models = &modelregistry.Registry{Model: model}
		prices.set(c.PriceRule{UnitPrice: 2})
		final, e := store.Snapshot(ctx, project.ID)
		if e != nil {
			t.Fatal(e)
		}
		run := final.LatestRun()
		if run.Status != d.RunCancelled || run.Credit.Amount != quote.Amount || len(run.Calls) == 0 || !run.Calls[0].Usage.Known {
			t.Fatal("late receipt or frozen price", run)
		}
		refund, e := store.CreditOperation(ctx, "generation:"+charge.ID+":refund")
		if e != nil || refund.Amount != quote.Amount {
			t.Fatal("refund", refund, e)
		}
		original, _ := store.CreditOperation(ctx, "generation:"+saved.Runs[0].Credit.ID+":deduct")
		balance, _ := store.Balance(ctx, a.ID)
		if balance != 100-original.Amount {
			t.Fatal("prior completed charge lost", balance)
		}
		for _, p := range final.VisualPrompts {
			if p.RunID == run.ID {
				t.Fatal("cancelled late output adopted")
			}
		}
	})
	t.Run("failed-unknown-provider-cost-still-refunds", func(t *testing.T) {
		current, _ := store.Snapshot(ctx, project.ID)
		target := current.Assets[0].ID
		q := request("POST", "/api/projects/"+project.ID+"/quote", tokenA, map[string]any{"revision": current.Revision, "operation": "single-visual", "assetId": target})
		var quote app.ChargeQuote
		if q.Code != 200 || json.Unmarshal(q.Body.Bytes(), &quote) != nil {
			t.Fatal(q.Code, q.Body.String())
		}
		failed := &failedFixtureModel{}
		service.Models = &modelregistry.Registry{Model: failed}
		defer func() { service.Models = &modelregistry.Registry{Model: model} }()
		w := request("POST", "/api/projects/"+project.ID+"/assets/"+target+"/visual-prompt-runs", tokenA, map[string]any{"revision": current.Revision, "billingBasis": quote.Basis})
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		if e = host.WaitUntilIdle(ctx); e != nil {
			t.Fatal(e)
		}
		service.Models = &modelregistry.Registry{Model: model}
		final, _ := store.Snapshot(ctx, project.ID)
		r := final.LatestRun()
		if r.Status != d.RunFailed || r.Credit == nil || len(r.Calls) != 1 || r.Calls[0].Usage.Known {
			t.Fatal(r)
		}
		refund, e := store.CreditOperation(ctx, "generation:"+r.Credit.ID+":refund")
		if e != nil || refund.Amount != quote.Amount {
			t.Fatal(refund, e)
		}
		w = request("GET", "/api/operations/model-usage?view=tasks&taskId="+r.Credit.ID, tokenOp, nil)
		var got modelusage.Result
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Items) != 1 || !got.Items[0].UnknownCost || got.Items[0].Settlement != "refunded" {
			t.Fatal(w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "PRIVATE-SUPPLIER-ERROR") {
			t.Fatal("raw provider error leaked")
		}
		if got.Items[0].InputTokens != nil || got.Items[0].OutputTokens != nil || got.Items[0].CacheHitTokens != nil {
			t.Fatal("unknown token usage advertised as zero")
		}

	})
	t.Run("committed-debit-response-lost-refunds-without-model-call", func(t *testing.T) {
		current, _ := store.Snapshot(ctx, project.ID)
		target := current.Assets[0].ID
		q := request("POST", "/api/projects/"+project.ID+"/quote", tokenA, map[string]any{"revision": current.Revision, "operation": "single-visual", "assetId": target})
		var quote app.ChargeQuote
		if q.Code != 200 || json.Unmarshal(q.Body.Bytes(), &quote) != nil {
			t.Fatal(q.Code, q.Body.String())
		}
		ledger := &uncertainFixtureLedger{CreditLedger: store}
		ledger.debitUnknown.Store(true)
		service.Credits = ledger
		defer func() { service.Credits = store }()
		w := request("POST", "/api/projects/"+project.ID+"/assets/"+target+"/visual-prompt-runs", tokenA, map[string]any{"revision": current.Revision, "billingBasis": quote.Basis})
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		if e = host.WaitUntilIdle(ctx); e != nil {
			t.Fatal(e)
		}
		p, _ := store.Snapshot(ctx, project.ID)
		r := p.LatestRun()
		if r.Status != d.RunFailed || len(r.Calls) != 0 || r.ErrorCode != "CREDIT_COMMIT_UNKNOWN" {
			t.Fatal(r.Status, len(r.Calls), r.ErrorCode)
		}
		debit, e := store.CreditOperation(ctx, "generation:"+r.Credit.ID+":deduct")
		if e != nil {
			t.Fatal(e)
		}
		refund, e := store.CreditOperation(ctx, "generation:"+r.Credit.ID+":refund")
		if e != nil || refund.Amount != debit.Amount {
			t.Fatal(refund, e)
		}
		if e = service.RecoverCredits(ctx); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("refund-not-submitted-stays-pending-and-recovery-uses-original-id", func(t *testing.T) {
		current, _ := store.Snapshot(ctx, project.ID)
		target := current.Assets[0].ID
		q := request("POST", "/api/projects/"+project.ID+"/quote", tokenA, map[string]any{"revision": current.Revision, "operation": "single-visual", "assetId": target})
		var quote app.ChargeQuote
		if q.Code != 200 || json.Unmarshal(q.Body.Bytes(), &quote) != nil {
			t.Fatal(q.Code, q.Body.String())
		}
		barrier := &delayedFixtureModel{delegate: model, entered: make(chan struct{}), release: make(chan struct{})}
		var once sync.Once
		defer once.Do(func() { close(barrier.release) })
		service.Models = &modelregistry.Registry{Model: barrier}
		defer func() { service.Models = &modelregistry.Registry{Model: model} }()
		ledger := &uncertainFixtureLedger{CreditLedger: store}
		ledger.refundNotSubmitted.Store(true)
		service.Credits = ledger
		defer func() { service.Credits = store }()
		w := request("POST", "/api/projects/"+project.ID+"/assets/"+target+"/visual-prompt-runs", tokenA, map[string]any{"revision": current.Revision, "billingBasis": quote.Basis})
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		select {
		case <-barrier.entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		active, _ := store.Snapshot(ctx, project.ID)
		charge := active.LatestRun().Credit
		w = request("POST", "/api/projects/"+project.ID+"/cancel", tokenA, map[string]any{"revision": active.Revision})
		if w.Code != 503 {
			t.Fatal("refund failure hidden", w.Code, w.Body.String())
		}
		if _, e = store.CreditOperation(ctx, "generation:"+charge.ID+":refund"); e == nil {
			t.Fatal("refund fabricated")
		}
		w = request("GET", "/api/operations/model-usage?view=tasks&taskId="+charge.ID, tokenOp, nil)
		var result modelusage.Result
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].Settlement != "refund-pending" {
			t.Fatal(w.Code, w.Body.String())
		}
		if e = service.RecoverCredits(ctx); e != nil {
			t.Fatal(e)
		}
		refund, e := store.CreditOperation(ctx, "generation:"+charge.ID+":refund")
		if e != nil || refund.Amount != quote.Amount {
			t.Fatal(refund, e)
		}
		once.Do(func() { close(barrier.release) })
		if e = host.WaitUntilIdle(ctx); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("insufficient-balance-never-calls-provider", func(t *testing.T) {
		q := request("POST", "/api/projects/quote", tokenB, map[string]any{"fileId": fileB.FileID, "settings": settings})
		var quote app.ChargeQuote
		if q.Code != 200 || json.Unmarshal(q.Body.Bytes(), &quote) != nil {
			t.Fatal(q.Code, q.Body.String())
		}
		w := request("POST", "/api/projects", tokenB, map[string]any{"fileId": fileB.FileID, "title": "insufficient balance fixture", "settings": settings, "billingBasis": quote.Basis})
		var out struct {
			ID string `json:"id"`
		}
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		if e = host.WaitUntilIdle(ctx); e != nil {
			t.Fatal(e)
		}
		p, _ := store.Snapshot(ctx, out.ID)
		r := p.LatestRun()
		if r.Status != d.RunFailed || r.ErrorCode != "CREDIT_INSUFFICIENT" || len(r.Calls) != 0 {
			t.Fatal(r.Status, r.ErrorCode, len(r.Calls))
		}
		baseRevision, baseSHA := p.Revision, p.ScriptSource.SHA256
		w = request("POST", "/api/projects/"+p.ID+"/source", tokenB, map[string]any{"revision": baseRevision, "fileId": fileB.FileID})
		if w.Code != 409 {
			t.Fatal("unquoted source changed", w.Code, w.Body.String())
		}
		same, _ := store.Snapshot(ctx, p.ID)
		if same.Revision != baseRevision || same.ScriptSource.SHA256 != baseSHA {
			t.Fatal("quote rejection overwrote source")
		}
		sq := request("POST", "/api/projects/"+p.ID+"/quote", tokenB, map[string]any{"revision": baseRevision, "operation": "replace-source", "fileId": fileB.FileID})
		var sourceQuote app.ChargeQuote
		if sq.Code != 200 || json.Unmarshal(sq.Body.Bytes(), &sourceQuote) != nil || sourceQuote.SourceVersion != 2 {
			t.Fatal(sq.Code, sq.Body.String())
		}
		w = request("POST", "/api/projects/"+p.ID+"/source", tokenB, map[string]any{"revision": baseRevision, "fileId": fileB.FileID, "billingBasis": sourceQuote.Basis})
		if w.Code != 200 {
			t.Fatal("quoted source refused", w.Code, w.Body.String())
		}
		if e = host.WaitUntilIdle(ctx); e != nil {
			t.Fatal(e)
		}
		replaced, _ := store.Snapshot(ctx, p.ID)
		if replaced.ScriptSource.Version != 2 || replaced.LatestRun().ErrorCode != "CREDIT_INSUFFICIENT" || len(replaced.LatestRun().Calls) != 0 {
			t.Fatal("replacement accounting")
		}

	})

	t.Run("zero-balance-promotion-retains-real-cost-no-zero-ledger", func(t *testing.T) {
		start, end := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
		prices.set(c.PriceRule{UnitPrice: 2, FreeEnabled: true, FreeStart: &start, FreeEnd: &end})
		defer prices.set(c.PriceRule{UnitPrice: 2})
		q := request("POST", "/api/projects/quote", tokenB, map[string]any{"fileId": fileB.FileID, "settings": settings})
		var quote app.ChargeQuote
		if q.Code != 200 || json.Unmarshal(q.Body.Bytes(), &quote) != nil || quote.Amount != 0 || !quote.Free || quote.OriginalAmount == 0 {
			t.Fatal(q.Code, q.Body.String())
		}
		w := request("POST", "/api/projects", tokenB, map[string]any{"fileId": fileB.FileID, "title": "private promotion fixture", "settings": settings, "billingBasis": quote.Basis})
		var out struct {
			ID string `json:"id"`
		}
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		if e = host.WaitUntilIdle(ctx); e != nil {
			t.Fatal(e)
		}
		p, e := store.Snapshot(ctx, out.ID)
		if e != nil || p.LatestRun().Status != d.RunCompleted {
			t.Fatal(p.LatestRun(), e)
		}
		_, count, e := store.CreditEntries(ctx, b.ID, 1, 100)
		if e != nil || count != 0 {
			t.Fatal("zero ledger", count, e)
		}
		cost := int64(0)
		for _, r := range p.Runs {
			for _, call := range r.Calls {
				cost += call.CostMicroCNY
			}
		}
		if cost <= 0 {
			t.Fatal("promotion erased provider costs")
		}
	})

	t.Run("session-replacement-does-not-cancel-completed-work", func(t *testing.T) {
		fresh := login(a.Email, "fixture-password-1234", "workspace")
		if w := request("GET", "/api/projects/"+project.ID, tokenA, nil); w.Code != 401 {
			t.Fatal(w.Code)
		}
		if w := request("POST", "/api/auth/logout", tokenA, map[string]any{}); w.Code != 401 {
			t.Fatal(w.Code)
		}
		if w := request("GET", "/api/projects/"+project.ID, fresh, nil); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	})
	// Keep the isolated evidence, including the known fake costs, for repeatable audits.
	fmt.Fprintln(io.Discard, "isolated evidence retained")
}

type delayedFixtureModel struct {
	delegate         ports.Model
	entered, release chan struct{}
}

func (m *delayedFixtureModel) Capacity() (int, int) { return m.delegate.Capacity() }
func (m *delayedFixtureModel) ContainsToolReceipt(raw json.RawMessage, a d.Action, origin d.Call) bool {
	return m.delegate.ContainsToolReceipt(raw, a, origin)
}
func (m *delayedFixtureModel) Generate(ctx context.Context, in ports.Request, observe func(json.RawMessage) error) (ports.Response, error) {
	out, e := m.delegate.Generate(ctx, in, observe)
	close(m.entered)
	<-m.release
	return out, e
}

type failedFixtureModel struct{}

func (*failedFixtureModel) Capacity() (int, int)                                       { return 1000000, 393216 }
func (*failedFixtureModel) ContainsToolReceipt(json.RawMessage, d.Action, d.Call) bool { return false }
func (*failedFixtureModel) Generate(ctx context.Context, in ports.Request, observe func(json.RawMessage) error) (ports.Response, error) {
	raw, _ := json.Marshal(in)
	if e := observe(raw); e != nil {
		return ports.Response{}, e
	}
	return ports.Response{Raw: json.RawMessage(`{"error":"PRIVATE-SUPPLIER-ERROR"}`)}, errors.New("PRIVATE-SUPPLIER-ERROR")
}

type uncertainFixtureLedger struct {
	ports.CreditLedger
	debitUnknown       atomic.Bool
	refundNotSubmitted atomic.Bool
}

func (l *uncertainFixtureLedger) Deduct(ctx context.Context, in c.Change) (c.Entry, error) {
	v, e := l.CreditLedger.Deduct(ctx, in)
	if e == nil && l.debitUnknown.CompareAndSwap(true, false) {
		return c.Entry{}, fault.New("CREDIT_COMMIT_UNKNOWN", "fixture lost commit response")
	}
	return v, e
}
func (l *uncertainFixtureLedger) Refund(ctx context.Context, in c.Change) (c.Entry, error) {
	if l.refundNotSubmitted.CompareAndSwap(true, false) {
		return c.Entry{}, fault.New("CREDIT_COMMIT_UNKNOWN", "fixture refund not yet submitted")
	}
	return l.CreditLedger.Refund(ctx, in)
}
