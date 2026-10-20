package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/passwordhash"
	a "drama-agent/internal/application/accounts"
	c "drama-agent/internal/application/credits"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	"drama-agent/internal/testsupport/redisruntime"
)

func TestGiftPolicyRegistrationDailyPGHTTP(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	initial, err := s.CreditGiftPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Only the guarded isolated identity database is used; leave subsequent
	// regression cases the same operational amounts they had before this test.
	t.Cleanup(func() {
		_, e := conn.Exec(context.Background(), `UPDATE drama_catalog_v1.credit_gift_policy SET registration_gift=$1,daily_gift=$2 WHERE id`, initial.RegistrationGift, initial.DailyGift)
		if e != nil {
			t.Error(e)
		}
	})
	policy, err := s.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: initial.Revision, RegistrationGift: 25, DailyGift: 7})
	if err != nil {
		t.Fatal(err)
	}
	service := &a.Service{Store: s, Hasher: passwordhash.Bcrypt{}}
	prefix := identity.New("gifts")
	user := createUser(t, s, prefix+"@example.test")
	wallet, err := s.CreditWallet(ctx, user.ID)
	if err != nil || wallet.Balance != 25 || wallet.GiftBalance != 25 || wallet.RechargeBalance != 0 {
		t.Fatal("registration wallet", wallet, err)
	}
	var claims, ledgers int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_gift_claims WHERE user_id=$1 AND kind='registration' AND amount=25 AND policy_revision=$2`, user.ID, policy.Revision).Scan(&claims); err != nil || claims != 1 {
		t.Fatal(claims, err)
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND source='registration-gift' AND category='gift'`, user.ID).Scan(&ledgers); err != nil || ledgers != 1 {
		t.Fatal(ledgers, err)
	}

	// Different accounts competing for one code: only the committed account is granted.
	_, auth, err := service.CreateCode(ctx, ports.Actor{UserID: "fixture-op", Role: ports.RoleOperator}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	emails := []string{prefix + "-race-a@example.test", prefix + "-race-b@example.test"}
	for _, email := range emails {
		codeFor(t, s, email, "register", "372915", time.Now().Add(time.Minute))
	}
	var registrations atomic.Int32
	var wg sync.WaitGroup
	for _, email := range emails {
		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			if _, e := service.Register(ctx, email, "并发注册用户", "fixture-password-1234", "372915", auth); e == nil {
				registrations.Add(1)
			}
		}(email)
	}
	wg.Wait()
	if registrations.Load() != 1 {
		t.Fatal("one code registered", registrations.Load())
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_gift_claims c JOIN drama_catalog_v1.users u ON u.id=c.user_id WHERE u.email=ANY($1) AND c.kind='registration'`, emails).Scan(&claims); err != nil || claims != 1 {
		t.Fatal("gift followed failed registration", claims, err)
	}

	// A failed Add rolls back user creation, email verification and authorization.
	blockedID := identity.New("gift_overflow")
	if _, err = s.Add(ctx, d.Change{AccountID: blockedID, Amount: d.MaxAmount, Category: d.Recharge, OperationID: prefix + "-overflow-seed", Source: "fixture", Subject: "fixture", Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	email := prefix + "-rollback@example.test"
	codeFor(t, s, email, "register", "587421", time.Now().Add(time.Minute))
	_, auth, err = service.CreateCode(ctx, ports.Actor{UserID: "fixture-op", Role: ports.RoleOperator}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := (passwordhash.Bcrypt{}).Hash([]byte("fixture-password-1234"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Register(ctx, s, a.Registration{ID: blockedID, Email: email, Username: "回滚用户", Code: "587421", AuthorizationCode: auth, PasswordHash: hash}); !codeIs(err, "CREDIT_INVALID_AMOUNT") {
		t.Fatal("expected overflow", err)
	}
	if _, err = s.CurrentUser(ctx, blockedID); err == nil {
		t.Fatal("failed grant created user")
	}
	current, _ := s.CreditGiftPolicy(ctx)
	policy, err = s.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: current.Revision, RegistrationGift: 0, DailyGift: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Register(ctx, s, a.Registration{ID: blockedID, Email: email, Username: "回滚用户", Code: "587421", AuthorizationCode: auth, PasswordHash: hash}); err != nil {
		t.Fatal("failed grant consumed eligibility", err)
	}

	// The production routing registry, actual PG authenticator and real TCP HTTP
	// exercise owner selection and role checks without logging in a formal user.
	host, handler := httpapi.New(&app.Service{}, nil, t.TempDir(), 0)
	host.Accounts = service
	host.Authenticator = service
	host.Credits = &c.Service{Ledger: s, Queries: s, Users: s}
	host.Gifts = &c.Gifts{Store: s, Cache: redisruntime.ConnectDatabase(t, conn.Config().ConnString())}
	server := httptest.NewServer(handler)
	defer server.Close()
	login, err := service.Login(ctx, user.Email, "test-password-at-least-12", "", "workspace")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, token string, body any) (int, []byte) {
		raw, _ := json.Marshal(body)
		req, e := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		if token != "" {
			addSessionCookie(req, token)
		}
		// 客户端时间及地区均不可信，整个HTTP领取流程在伪造头下仍须按DB日期执行。
		req.Header.Set("Date", time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
		req.Header.Set("X-Timezone", "Pacific/Kiritimati")
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, b
	}
	if status, _ := request("GET", "/api/operations/credit-policy", login.Token, nil); status != 403 {
		t.Fatal("ordinary read policy", status)
	}
	if status, _ := request("POST", "/api/credits/daily-gift", "", map[string]any{}); status != 401 {
		t.Fatal("anonymous claim", status)
	}
	for _, path := range []string{"/api/credits/balance", "/api/credits/ledger"} {
		if status, b := request("GET", path, login.Token, nil); status != 200 {
			t.Fatal(status, string(b))
		}
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_gift_claims WHERE user_id=$1 AND kind='daily'`, user.ID).Scan(&claims); err != nil || claims != 0 {
		t.Fatal("read/auth granted gift", claims, err)
	}
	if status, _ := request("POST", "/api/credits/daily-gift", login.Token, map[string]any{"userId": blockedID}); status != 400 {
		t.Fatal("client account accepted", status)
	}
	for _, body := range []map[string]any{
		{"date": "2099-01-01"},
		{"timezone": "Pacific/Kiritimati"},
		{"expiresAt": "2099-01-02T00:00:00Z"},
	} {
		if status, _ := request("POST", "/api/credits/daily-gift", login.Token, body); status != 400 {
			t.Fatal("client calendar or expiry accepted", body, status)
		}
	}
	var first atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, b := request("POST", "/api/credits/daily-gift", login.Token, map[string]any{})
			var v c.DailyGift
			if e := json.Unmarshal(b, &v); e != nil || status != 200 || v.Amount != 7 {
				t.Error(status, string(b), e)
				return
			}
			if !v.AlreadyClaimed {
				first.Add(1)
			}
		}()
	}
	wg.Wait()
	wallet, err = s.CreditWallet(ctx, user.ID)
	if err != nil || wallet.GiftBalance != 32 {
		t.Fatal("concurrent grant wallet", wallet, err)
	}
	if first.Load() != 1 {
		t.Fatal("daily concurrent grants", first.Load())
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND source='daily-gift'`, user.ID).Scan(&ledgers); err != nil || ledgers != 1 {
		t.Fatal("duplicate daily ledger", ledgers, err)
	}
	if _, err = s.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: policy.Revision, RegistrationGift: 0, DailyGift: 9}); err != nil {
		t.Fatal(err)
	}
	status, raw := request("POST", "/api/credits/daily-gift", login.Token, map[string]any{})
	var original c.DailyGift
	if json.Unmarshal(raw, &original) != nil || status != 200 || original.Amount != 7 || !original.AlreadyClaimed {
		t.Fatal("policy rewrote daily fact", status, string(raw))
	}
	var receiptFields map[string]json.RawMessage
	if json.Unmarshal(raw, &receiptFields) != nil || len(receiptFields) != 4 {
		t.Fatal("daily receipt leaked wallet or internal cache metadata", string(raw))
	}
	for _, name := range []string{"date", "amount", "claimed", "alreadyClaimed"} {
		if _, ok := receiptFields[name]; !ok {
			t.Fatal("missing daily receipt fact", name, string(raw))
		}
	}
	var beforeForged, afterForged time.Time
	if err = conn.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&beforeForged); err != nil {
		t.Fatal(err)
	}
	status, forgedRaw := request("POST", "/api/credits/daily-gift?date=2099-01-01&timezone=Pacific%2FKiritimati", login.Token, map[string]any{})
	if err = conn.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&afterForged); err != nil {
		t.Fatal(err)
	}
	var forged c.DailyGift
	if json.Unmarshal(forgedRaw, &forged) != nil || status != 200 || (forged.Date != c.GiftDate(beforeForged) && forged.Date != c.GiftDate(afterForged)) || (forged.Date == original.Date && (!forged.AlreadyClaimed || forged.Amount != original.Amount)) {
		t.Fatal("query changed authoritative gift date or awarded a second gift", status, string(forgedRaw))
	}
	grant, err := s.CreditOperation(ctx, "gift:daily:"+user.ID+":"+original.Date)
	expectedEnd, parseErr := time.Parse(time.RFC3339, original.Date+"T00:00:00+08:00")
	if err != nil || parseErr != nil || grant.DailyAmount != 7 || grant.DailyExpiresAt == nil || !grant.DailyExpiresAt.Equal(expectedEnd.AddDate(0, 0, 1)) {
		t.Fatal("daily grant did not freeze next Shanghai midnight", grant, err, parseErr)
	}
	if _, err = s.SetUserEnabled(ctx, user.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimDailyCreditGift(ctx, user.ID); !codeIs(err, "ACCOUNT_DISABLED") {
		t.Fatal("disabled application claim", err)
	}
	if status, _ = request("POST", "/api/credits/daily-gift", login.Token, map[string]any{}); status != 401 {
		t.Fatal("disabled HTTP claim", status)
	}

	t.Run("policy-CAS-zero-claim-and-input", func(t *testing.T) {
		current, _ := s.CreditGiftPolicy(ctx)
		var successes atomic.Int32
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, e := s.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: current.Revision, RegistrationGift: 0, DailyGift: 0}); e == nil {
					successes.Add(1)
				} else if !codeIs(e, "CREDIT_POLICY_CONFLICT") {
					t.Error(e)
				}
			}()
		}
		wg.Wait()
		if successes.Load() != 1 {
			t.Fatal("policy CAS", successes.Load())
		}
		zeroUser := createUser(t, s, prefix+"-zero@example.test")
		zeroLogin, e := service.Login(ctx, zeroUser.Email, "test-password-at-least-12", "", "workspace")
		if e != nil {
			t.Fatal(e)
		}
		zeroStatus, zeroRaw := request("POST", "/api/credits/daily-gift", zeroLogin.Token, map[string]any{})
		var zero c.DailyGift
		if json.Unmarshal(zeroRaw, &zero) != nil || zeroStatus != 200 || zero.Amount != 0 || zero.Claimed || zero.AlreadyClaimed {
			t.Fatal(zeroStatus, string(zeroRaw))
		}
		current, _ = s.CreditGiftPolicy(ctx)
		if _, e = s.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: current.Revision, DailyGift: 13}); e != nil {
			t.Fatal(e)
		}
		zeroStatus, zeroRaw = request("POST", "/api/credits/daily-gift", zeroLogin.Token, map[string]any{})
		var again c.DailyGift
		if json.Unmarshal(zeroRaw, &again) != nil || zeroStatus != 200 || again.Amount != 13 || !again.Claimed || again.AlreadyClaimed {
			t.Fatal("zero prevented same-day HTTP grant", zeroStatus, string(zeroRaw))
		}
		if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1`, zeroUser.ID).Scan(&ledgers); err != nil || ledgers != 1 {
			t.Fatal("same-day positive ledger", ledgers, err)
		}
		// Create a unique isolated operator; never replace the formal operator session.
		opID := identity.New("gift_op")
		_, e = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,'fixture operator','operator',$3)`, opID, opID+"@example.test", hash)
		if e != nil {
			t.Fatal(e)
		}
		opLogin, e := service.Login(ctx, opID+"@example.test", "fixture-password-1234", "", "operations")
		if e != nil {
			t.Fatal(e)
		}
		for _, body := range []any{map[string]any{"revision": current.Revision}, map[string]any{"revision": current.Revision, "registrationGift": 0.5, "dailyGift": 0}, map[string]any{"revision": current.Revision, "registrationGift": -1, "dailyGift": 0}, map[string]any{"revision": current.Revision, "registrationGift": 0, "dailyGift": 0, "timezone": "UTC"}} {
			status, raw = request("POST", "/api/operations/credit-policy", opLogin.Token, body)
			if status != 400 {
				t.Fatal("invalid policy", status, string(raw))
			}
		}
		status, raw = request("GET", "/api/credits/ledger?direction=acquired&category=gift", login.Token, nil)
		if status != 401 {
			t.Fatal("disabled query", status, string(raw))
		}
		if status, raw = request("POST", "/api/operations/users/"+zeroUser.ID+"/credits", opLogin.Token, map[string]any{"kind": "add", "category": "recharge", "amount": 10, "reason": "fixture", "operationId": prefix + "-op-recharge"}); status != 200 {
			t.Fatal(status, string(raw))
		}
		if status, raw = request("POST", "/api/operations/users/"+zeroUser.ID+"/credits", opLogin.Token, map[string]any{"kind": "add", "category": "gift", "amount": 10, "reason": "fixture", "operationId": prefix + "-op-recharge"}); status != 409 {
			t.Fatal("category idempotency", status, string(raw))
		}
		if status, raw = request("POST", "/api/operations/users/"+zeroUser.ID+"/credits", opLogin.Token, map[string]any{"kind": "deduct", "amount": 3, "reason": "fixture", "operationId": prefix + "-op-deduct"}); status != 200 {
			t.Fatal("deduct omits category", status, string(raw))
		}
		for _, body := range []map[string]any{
			{"kind": "add", "amount": 1, "reason": "fixture", "operationId": prefix + "-missing-category"},
			{"kind": "add", "category": "legacy", "amount": 1, "reason": "fixture", "operationId": prefix + "-invalid-category"},
			{"kind": "deduct", "category": "gift", "amount": 1, "reason": "fixture", "operationId": prefix + "-deduct-category"},
		} {
			if status, raw = request("POST", "/api/operations/users/"+zeroUser.ID+"/credits", opLogin.Token, body); status != 400 {
				t.Fatal("invalid category status", status, string(raw))
			}
		}
		// Lose an actual HTTP response after the production handler has committed.
		// Retrying the same daily command must read the original 13-point grant.
		lostUser := createUser(t, s, prefix+"-lost-response@example.test")
		lostLogin, e := service.Login(ctx, lostUser.Email, "test-password-at-least-12", "", "workspace")
		if e != nil {
			t.Fatal(e)
		}
		lostServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			if recorder.Code != 200 {
				t.Errorf("lost-response handler %d %s", recorder.Code, recorder.Body.String())
			}
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
		}))
		defer lostServer.Close()
		lostRequest, e := http.NewRequest("POST", lostServer.URL+"/api/credits/daily-gift", bytes.NewBufferString(`{}`))
		if e != nil {
			t.Fatal(e)
		}
		addSessionCookie(lostRequest, lostLogin.Token)
		if response, e := lostServer.Client().Do(lostRequest); e == nil {
			response.Body.Close()
			t.Fatal("response was not lost")
		}
		status, raw = request("POST", "/api/credits/daily-gift", lostLogin.Token, map[string]any{})
		var recovered c.DailyGift
		if json.Unmarshal(raw, &recovered) != nil || status != 200 || recovered.Amount != 13 || !recovered.AlreadyClaimed {
			t.Fatal("lost response reissued grant", status, string(raw))
		}
		lostWallet, e := s.CreditWallet(ctx, lostUser.ID)
		if e != nil || lostWallet.GiftBalance != 13 {
			t.Fatal("lost response wallet", lostWallet, e)
		}
		if e = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND source='daily-gift'`, lostUser.ID).Scan(&ledgers); e != nil || ledgers != 1 {
			t.Fatal("lost response duplicated", ledgers, e)
		}
		t.Logf("isolated PG/HTTP: registration gift25, daily7, 12 concurrent claims exactly1, zero remains eligible then funded13 once, policy CAS1/4; %s", fmt.Sprint(original.Date))
	})
}
