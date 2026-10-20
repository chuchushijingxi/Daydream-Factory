package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"drama-agent/internal/testsupport/pgfixture"
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
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/adapter/redisstate"
	"drama-agent/internal/application/accounts"
	creditsapp "drama-agent/internal/application/credits"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	project "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/redisruntime"
	"github.com/jackc/pgx/v5"
)

func TestCreditSSERealPGHTTP(t *testing.T) {
	path := os.Getenv("DRAMA_TEST_CONFIG_FILE")
	if path == "" {
		t.Skip("dedicated accounts PG required")
	}
	cfg, err := appconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	address, _ := url.Parse(cfg.Database.URL)
	if !strings.HasPrefix(address.Path, "/drama_accounts_test_") {
		t.Fatal("dedicated accounts database required")
	}
	ctx := context.Background()
	listenAddress := *address
	query := listenAddress.Query()
	listenName := identity.New("sse_listener")
	query.Set("application_name", listenName)
	listenAddress.RawQuery = query.Encode()
	store, err := pgfixture.Open(t, ctx, listenAddress.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	writer, err := pgfixture.Open(t, ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	conn, err := pgx.Connect(ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	password := "isolated-sse-fixture-password"
	hash, err := (passwordhash.Bcrypt{}).Hash([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{identity.New("sse_a"), identity.New("sse_b"), identity.New("sse_operator")}
	service := &accounts.Service{Store: store, Hasher: passwordhash.Bcrypt{}}
	tokens := make([]string, 3)
	for i, id := range ids {
		role := ports.RoleUser
		audience := "workspace"
		if i == 2 {
			role = ports.RoleOperator
			audience = "operations"
		}
		_, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,$1,$3,$4)`, id, id+"@example.invalid", role, hash)
		if err != nil {
			t.Fatal(err)
		}
		login, e := service.Login(ctx, id+"@example.invalid", password, "", audience)
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = login.Token
	}
	host, handler := httpapi.New(&projectapp.Service{}, nil, t.TempDir(), 0)
	host.StreamHeartbeat = appconfig.StreamHeartbeat
	host.StreamWriteTimeout = appconfig.StreamWriteTimeout
	host.Authenticator = service
	host.Accounts = service
	updates := &creditReconnectUpdates{}
	updates.state.Store(redisruntime.Connect(t))
	host.Credits = &creditsapp.Service{Ledger: store, Queries: store, Updates: updates, Users: store}
	defer host.Shutdown(ctx)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	request := func(method, path, token string, body any) *http.Response {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		if token != "" {
			addSessionCookie(r, token)
		}
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		resp, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return resp
	}
	expectStatus := func(path, token string, want int) {
		t.Helper()
		r := request("GET", path, token, nil)
		defer r.Body.Close()
		if r.StatusCode != want {
			raw, _ := io.ReadAll(r.Body)
			t.Fatal(path, r.StatusCode, string(raw))
		}
	}
	expectStatus("/api/credits/events", "", 401)
	expectStatus("/api/operations/users/"+ids[0]+"/credits/events", tokens[1], 403)
	expectStatus("/api/operations/users/missing/credits/events", tokens[2], 404)
	head := request("HEAD", "/api/credits/events", tokens[0], nil)
	head.Body.Close()
	if head.StatusCode != 405 {
		t.Fatal(head.StatusCode)
	}
	type stream struct {
		events chan string
		done   chan struct{}
		close  func()
	}
	open := func(path, token string) *stream {
		t.Helper()
		resp := request("GET", path, token, nil)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") || resp.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal(resp.StatusCode, resp.Header)
		}
		s := &stream{events: make(chan string, 32), done: make(chan struct{}), close: func() { resp.Body.Close() }}
		go func() {
			defer close(s.done)
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "event: ") {
					s.events <- strings.TrimPrefix(line, "event: ")
				}
				if strings.HasPrefix(line, "data: ") && line != "data: {}" {
					s.events <- "LEAK:" + line
				}
			}
		}()
		t.Cleanup(s.close)
		return s
	}
	event := func(s *stream, want string) {
		t.Helper()
		select {
		case got := <-s.events:
			if got != want {
				t.Fatal("unexpected event", got, want)
			}
		case <-s.done:
			t.Fatal("stream ended")
		case <-time.After(3 * time.Second):
			t.Fatal("missing event", want)
		}
	}
	quiet := func(s *stream) {
		t.Helper()
		select {
		case got := <-s.events:
			t.Fatal("unexpected event", got)
		case <-s.done:
			t.Fatal("unexpected disconnect")
		case <-time.After(150 * time.Millisecond):
		}
	}
	a := open("/api/credits/events?userId="+ids[1], tokens[0])
	b := open("/api/credits/events", tokens[1])
	op := open("/api/operations/users/"+ids[0]+"/credits/events", tokens[2])
	for _, s := range []*stream{a, b, op} {
		event(s, "credits.ready")
	}
	// Streams subscribe only; they never query a balance as authentication or mint money.
	var count int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=ANY($1)`, ids).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	change := d.Change{AccountID: ids[0], Amount: 100, Category: d.Gift, OperationID: identity.New("sse_add"), Source: "operations-adjustment", Subject: ids[2], Reason: "isolated SSE grant"}
	posted := request("POST", "/api/operations/users/"+ids[0]+"/credits", tokens[2], map[string]any{"kind": "add", "amount": change.Amount, "category": change.Category, "operationId": change.OperationID, "reason": change.Reason})
	posted.Body.Close()
	if posted.StatusCode != 200 {
		t.Fatal(posted.StatusCode)
	}
	event(a, "credits.changed")
	event(op, "credits.changed")
	quiet(b)
	assertBalance := func(want int64) {
		t.Helper()
		wallet, e := store.CreditWallet(ctx, ids[0])
		if e != nil || wallet.Balance != want {
			t.Fatal(wallet, e)
		}
	}
	assertBalance(100)
	if _, err = writer.Add(ctx, change); err != nil {
		t.Fatal(err)
	}
	quiet(a)
	quiet(op)
	// Rollback never delivers a credit change, even after the INSERT trigger ran.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_ledger(id,operation_id,account_id,kind,amount,balance,source,subject,reason,category,gift_amount) VALUES($1,$1,$2,'add',1,101,'rollback','fixture','rollback','gift',1)`, identity.New("sse_rollback"), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	quiet(a)
	assertBalance(100)
	debit := d.Change{AccountID: ids[0], Amount: 40, OperationID: identity.New("sse_debit"), Source: "worker-fixture", Subject: "worker-fixture", Reason: "pre-deduction"}
	receipt, err := writer.Deduct(ctx, debit)
	if err != nil {
		t.Fatal(err)
	}
	event(a, "credits.changed")
	event(op, "credits.changed")
	assertBalance(60)
	if _, err = writer.Refund(ctx, d.Change{OriginalDeductionID: receipt.ID, Amount: 40, OperationID: identity.New("sse_refund"), Subject: "worker-fixture", Reason: "confirmed refund"}); err != nil {
		t.Fatal(err)
	}
	event(a, "credits.changed")
	event(op, "credits.changed")
	assertBalance(100)
	// A completion-only commit must refresh analysis despite no new ledger entry.
	runID := identity.New("sse_run")
	p := project.Project{ID: identity.New("sse_project"), OwnerID: ids[0], Revision: 1, Runs: []project.Run{{ID: runID, Status: project.RunRunning, Credit: &d.Charge{ID: runID, Amount: 10}}}}
	if err = writer.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Deduct(ctx, d.Change{AccountID: ids[0], Amount: 10, OperationID: "generation:" + runID + ":deduct", Source: p.ID, Subject: ids[0], Reason: "generation"}); err != nil {
		t.Fatal(err)
	}
	event(a, "credits.changed")
	event(op, "credits.changed")
	filter := d.HistoryFilter{Page: 1, PageSize: 20, Direction: "consumed", Category: "all"}
	before, err := store.CreditHistory(ctx, ids[0], filter)
	if err != nil {
		t.Fatal(err)
	}
	p.Revision = 2
	p.Runs[0].Status = project.RunCompleted
	if err = writer.Commit(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	event(a, "credits.changed")
	event(op, "credits.changed")
	after, err := store.CreditHistory(ctx, ids[0], filter)
	if err != nil || after.Summary.SettledConsumed-before.Summary.SettledConsumed != 10 || after.Total != before.Total {
		t.Fatal("settlement projection", before.Summary, after.Summary, err)
	}
	// 历史 task-ID 与当前媒体冻结 charge-ID 都必须发送结算失效，二者不能混为一个身份。
	for _, frozenCharge := range []bool{false, true} {
		taskID := identity.New("sse_task")
		chargeID, snapshot := taskID, []byte(`{}`)
		if frozenCharge {
			chargeID = identity.New("media_charge")
			snapshot, _ = json.Marshal(map[string]any{"callerContext": map[string]any{"credit": map[string]any{"id": chargeID, "amount": 5}}})
		}
		_, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.tasks(id,scope_id,command_id,status,revision,input_snapshot,payload,owner_id) VALUES($1,$2,$1,'queued',1,$3,'{}',$4)`, taskID, "project:"+p.ID, snapshot, ids[0])
		if err != nil {
			t.Fatal(err)
		}
		_, err = conn.Exec(ctx, `UPDATE drama_catalog_v1.tasks SET status='running',revision=2 WHERE id=$1`, taskID)
		if err != nil {
			t.Fatal(err)
		}
		quiet(a) // 没有原扣款时不制造积分事件。
		_, err = writer.Deduct(ctx, d.Change{AccountID: ids[0], Amount: 5, OperationID: "generation:" + chargeID + ":deduct", Source: p.ID, Subject: ids[0], Reason: "generation"})
		if err != nil {
			t.Fatal(err)
		}
		event(a, "credits.changed")
		event(op, "credits.changed")
		before, err = store.CreditHistory(ctx, ids[0], filter)
		if err != nil {
			t.Fatal(err)
		}
		_, err = conn.Exec(ctx, `UPDATE drama_catalog_v1.tasks SET status='succeeded',revision=3 WHERE id=$1`, taskID)
		if err != nil {
			t.Fatal(err)
		}
		event(a, "credits.changed")
		event(op, "credits.changed")
		after, err = store.CreditHistory(ctx, ids[0], filter)
		if err != nil || after.Summary.SettledConsumed-before.Summary.SettledConsumed != 5 || after.Total != before.Total {
			t.Fatal("task settlement projection", frozenCharge, before.Summary, after.Summary, err)
		}
	}

	quiet(b)
	// Killing only this fixture's dedicated LISTEN connection closes every
	// subscriber. A fresh stream reestablishes LISTEN and rereads on ready.
	updates.state.Load().Close()
	for _, s := range []*stream{a, b, op} {
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			t.Fatal("failed listener did not close stream")
		}
	}
	updates.state.Store(redisruntime.Connect(t))
	a = open("/api/credits/events", tokens[0])
	b = open("/api/credits/events", tokens[1])
	op = open("/api/operations/users/"+ids[0]+"/credits/events", tokens[2])
	for _, s := range []*stream{a, b, op} {
		event(s, "credits.ready")
	}
	// Reconnect always sends ready: callers reread the latest wallet, not missed deltas.
	a.close()
	if _, err = writer.Add(ctx, d.Change{AccountID: ids[0], Amount: 3, Category: d.Recharge, OperationID: identity.New("sse_offline"), Source: "fixture", Subject: "fixture", Reason: "offline"}); err != nil {
		t.Fatal(err)
	}
	event(op, "credits.changed")
	a = open("/api/credits/events", tokens[0])
	event(a, "credits.ready")
	assertBalance(83)
	// Replace A and disable B; idle streams must stop at the common heartbeat check.
	if _, err = service.Login(ctx, ids[0]+"@example.invalid", password, "", "workspace"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetUserEnabled(ctx, ids[1], false); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*stream{a, b} {
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
			t.Fatal("revoked idle stream stayed open")
		}
	}
	op.close()
	t.Log("PG/HTTP: grants, pre-deduction, refund, rollback, idempotency, owner/operator isolation, completion analysis, reconnect and idle revocation verified")
}

type creditReconnectUpdates struct {
	state atomic.Pointer[redisstate.Store]
}

func (s *creditReconnectUpdates) WatchCreditAccount(ctx context.Context, user string) (<-chan struct{}, func(), error) {
	return s.state.Load().WatchCreditAccount(ctx, user)
}
