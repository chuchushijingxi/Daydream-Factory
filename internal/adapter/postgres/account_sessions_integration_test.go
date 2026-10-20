package postgres_test

import (
	"context"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/passwordhash"
	a "drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
)

func TestPlatformSessionsRealPGHTTP(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	service := &a.Service{Store: s, Hasher: passwordhash.Bcrypt{}}
	user := createUser(t, s, identity.New("platformuser")+"@example.invalid")
	op := createUser(t, s, identity.New("platformoperator")+"@example.invalid")
	if _, err := conn.Exec(ctx, `UPDATE drama_catalog_v1.users SET role='operator' WHERE id=$1`, op.ID); err != nil {
		t.Fatal(err)
	}
	password := "test-password-at-least-12"
	login := func(email, audience string) a.LoginResult {
		t.Helper()
		v, err := service.Login(ctx, email, password, "", audience)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	valid := func(token string) {
		t.Helper()
		actor, err := service.Authenticate(ctx, token)
		if err != nil || actor.UserID != op.ID || actor.Role != ports.RoleOperator {
			t.Fatal("operator current session", err)
		}
	}
	ws, ops := login(op.Email, a.Workspace), login(op.Email, a.Operations)
	valid(ws.Token)
	valid(ops.Token)
	ordinary := login(user.Email, a.Workspace)
	if _, err := service.Login(ctx, user.Email, password, "", a.Operations); !codeIs(err, "ROLE_FORBIDDEN") {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, ordinary.Token); err != nil {
		t.Fatal("denied operations login revoked workspace", err)
	}
	t.Run("old-credential-cannot-be-repacked-for-a-platform", func(t *testing.T) {
		legacy := a.RandomToken()
		redisruntime.SeedDigest(t, user.ID, a.Workspace, a.Digest(legacy), ports.RoleUser, time.Now().Add(time.Hour))

		if _, err := service.Authenticate(ctx, user.ID+".workspace."+legacy); !codeIs(err, "SESSION_REPLACED") {
			t.Fatal("legacy digest accepted after repacking", err)
		}
		ordinary = login(user.Email, a.Workspace)
		if _, err := service.Authenticate(ctx, ordinary.Token); err != nil {
			t.Fatal("new bound login rejected", err)
		}
	})
	h, handler := httpapi.New(&projectapp.Service{}, nil, t.TempDir(), 0)
	h.Accounts = service
	h.Authenticator = service
	defer h.Shutdown(ctx)
	request := func(path, audience, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8787"+path, nil)
		if token != "" {
			addSessionCookie(r, token)
		}
		if audience != "" {
			r.Header.Set("X-Drama-Audience", audience)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, v := range []a.LoginResult{ws, ops} {
		if w := request("/api/auth/me", "", v.Token); w.Code != 200 {
			t.Fatal("profile", w.Code, w.Body.String())
		}
	}
	if w := request("/api/operations/users", a.Workspace, ws.Token); w.Code != 403 || !strings.Contains(w.Body.String(), "SESSION_AUDIENCE_MISMATCH") {
		t.Fatal("workspace reached management", w.Code)
	}
	if w := request("/api/operations/users", a.Operations, ops.Token); w.Code != 200 {
		t.Fatal("operations access", w.Code)
	}
	for _, path := range []string{"/api/auth/me?audience=operations", "/api/auth/me?audience=workspace&audience=workspace", "/api/auth/me?audience=", "/api/auth/me?audience=%"} {
		if w := request(path, a.Workspace, ws.Token); w.Code != 400 {
			t.Fatal("ambiguous selector accepted", path, w.Code)
		}
	}
	t.Run("native-selector-and-no-cookie-fallback", func(t *testing.T) {
		r := httptest.NewRequest("HEAD", "http://127.0.0.1:8787/api/auth/avatar?audience=operations", nil)
		r.AddCookie(&http.Cookie{Name: "drama_operations_session", Value: ops.Token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("native HEAD did not authenticate", w.Code)
		}
		r = httptest.NewRequest("GET", "http://127.0.0.1:8787/api/auth/me", nil)
		r.AddCookie(&http.Cookie{Name: "drama_operations_session", Value: ops.Token})
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("other Cookie fallback", w.Code)
		}
		r = httptest.NewRequest("GET", "http://127.0.0.1:8787/api/auth/me", nil)
		r.AddCookie(&http.Cookie{Name: "drama_workspace_session", Value: ops.Token})
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("credential platform swap", w.Code)
		}
		r = httptest.NewRequest("GET", "http://127.0.0.1:8787/api/auth/me", nil)
		r.AddCookie(&http.Cookie{Name: "drama_session", Value: ops.Token})
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("obsolete Cookie accepted", w.Code)
		}
	})
	t.Run("parallel-password-and-code-platform-login", func(t *testing.T) {
		var wg sync.WaitGroup
		tokens := make(chan string, 6)
		for i := 0; i < 3; i++ {
			for _, aud := range []string{a.Workspace, a.Operations} {
				wg.Add(1)
				go func(audience string) {
					defer wg.Done()
					v, err := service.Login(ctx, op.Email, password, "", audience)
					if err != nil {
						t.Error(err)
						return
					}
					tokens <- v.Token
				}(aud)
			}
		}
		wg.Wait()
		close(tokens)
		current := map[string]string{}
		for token := range tokens {
			if _, err := service.Authenticate(ctx, token); err == nil {
				_, aud, _, _ := a.SessionParts(token)
				if current[aud] != "" {
					t.Fatal("multiple current sessions", aud)
				}
				current[aud] = token
			} else if !codeIs(err, "SESSION_REPLACED") {
				t.Fatal(err)
			}
		}
		if len(current) != 2 {
			t.Fatal("two independent platform slots", len(current))
		}
		// A code login enters the exact same operations slot, retaining workspace.
		codeFor(t, s, op.Email, "login", "312456", time.Now().Add(time.Minute))
		coded, err := service.Login(ctx, op.Email, "", "312456", a.Operations)
		if err != nil {
			t.Fatal(err)
		}
		valid(current[a.Workspace])
		valid(coded.Token)
		if _, err = service.Authenticate(ctx, current[a.Operations]); !codeIs(err, "SESSION_REPLACED") {
			t.Fatal("code did not replace operations", err)
		}
		if err = s.Logout(ctx, ops.Token); err != nil {
			t.Fatal(err)
		}
		valid(coded.Token)
		valid(current[a.Workspace])
		ws.Token = current[a.Workspace]
		ops = coded
	})
	t.Run("failed-and-stale-writes-preserve-both-slots", func(t *testing.T) {
		var before, after int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.session_events WHERE user_id=$1`, op.ID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Login(ctx, op.Email, "wrong", "", a.Workspace); !codeIs(err, "INVALID_CREDENTIALS") {
			t.Fatal(err)
		}
		if err := service.ChangePassword(ctx, ports.Actor{UserID: op.ID, Role: ports.RoleOperator}, ws.Token, "wrong", password); !codeIs(err, "CURRENT_PASSWORD_INVALID") {
			t.Fatal(err)
		}
		old := ws.Token
		ws = login(op.Email, a.Workspace)
		if err := service.ChangePassword(ctx, ports.Actor{UserID: op.ID, Role: ports.RoleOperator}, old, password, "must-not-become-password"); !codeIs(err, "SESSION_REVOKED") {
			t.Fatal("stale password write", err)
		}
		if err := s.Logout(ctx, old); err != nil {
			t.Fatal(err)
		}
		valid(ws.Token)
		valid(ops.Token)
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.session_events WHERE user_id=$1`, op.ID).Scan(&after); err != nil || after != before+1 {
			t.Fatal("failed/stale request emitted revocation", before, after, err)
		}
	})
	t.Run("logout-one-platform-and-credential-bound-late-logout", func(t *testing.T) {
		old := ops.Token
		ops = login(op.Email, a.Operations)
		if err := s.Logout(ctx, old); err != nil {
			t.Fatal(err)
		}
		valid(ws.Token)
		valid(ops.Token)
		if err := s.Logout(ctx, ops.Token); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Authenticate(ctx, ops.Token); !codeIs(err, "SESSION_REVOKED") {
			t.Fatal(err)
		}
		valid(ws.Token)
		ops = login(op.Email, a.Operations)
	})
	for _, kind := range []string{"password_changed", "password_reset", "disabled"} {
		t.Run(kind+"-revokes-both", func(t *testing.T) {
			actor := ports.Actor{UserID: op.ID, Role: ports.RoleOperator}
			switch kind {
			case "password_changed":
				if err := service.ChangePassword(ctx, actor, ws.Token, password, password); err != nil {
					t.Fatal(err)
				}
			case "password_reset":
				codeFor(t, s, op.Email, "reset", "312456", time.Now().Add(time.Minute))
				if err := service.ResetPassword(ctx, op.Email, "312456", password); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if _, err := s.SetUserEnabled(ctx, op.ID, false); err != nil {
					t.Fatal(err)
				}
			}
			for _, v := range []a.LoginResult{ws, ops} {
				_, err := service.Authenticate(ctx, v.Token)
				want := "SESSION_REVOKED"
				if kind == "disabled" {
					want = "ACCOUNT_DISABLED"
				}
				if !codeIs(err, want) {
					t.Fatal(kind, err)
				}
			}
			var count int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.session_events WHERE user_id=$1 AND kind=$2`, op.ID, kind).Scan(&count); err != nil || count != 2 {
				t.Fatal("atomic audit for both slots", kind, count, err)
			}
			if kind == "disabled" {
				if _, err := s.SetUserEnabled(ctx, op.ID, true); err != nil {
					t.Fatal(err)
				}
				if _, err := service.Authenticate(ctx, ws.Token); !codeIs(err, "SESSION_REVOKED") {
					t.Fatal("enable revived old session", err)
				}
			}
			ws, ops = login(op.Email, a.Workspace), login(op.Email, a.Operations)
		})
	}
	t.Run("expiry-and-audit-have-no-credential-or-content", func(t *testing.T) {
		_, audience, digest, _ := a.SessionParts(ws.Token)
		redisruntime.SeedDigest(t, op.ID, audience, digest, ports.RoleOperator, time.Now().Add(10*time.Millisecond))
		time.Sleep(20 * time.Millisecond)

		if _, err := service.Authenticate(ctx, ws.Token); !codeIs(err, "SESSION_EXPIRED") {
			t.Fatal(err)
		}
		valid(ops.Token)
		var raw []byte
		if err := conn.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(e)),'[]'::jsonb) FROM drama_catalog_v1.session_events e WHERE user_id=$1`, op.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var records []map[string]any
		if err := json.Unmarshal(raw, &records); err != nil {
			t.Fatal(err)
		}
		for _, record := range records {
			if len(record) != 5 {
				t.Fatal("unexpected audit fields", record)
			}
		}
		for _, private := range []string{password, ws.Token, ops.Token, "hash", "Cookie", "body"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("private audit data", private)
			}
		}
	})
}
