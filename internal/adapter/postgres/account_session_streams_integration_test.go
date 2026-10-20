package postgres_test

import (
	"bufio"
	"context"
	"drama-agent/internal/adapter/appconfig"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/passwordhash"
	a "drama-agent/internal/application/accounts"
	c "drama-agent/internal/application/credits"
	projectapp "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
)

func TestPlatformStreamsRecheckOnlyTheirCurrentPGSlot(t *testing.T) {
	s, conn := identityDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	op := createUser(t, s, identity.New("platformstream")+"@example.invalid")
	if _, err := conn.Exec(ctx, `UPDATE drama_catalog_v1.users SET role='operator' WHERE id=$1`, op.ID); err != nil {
		t.Fatal(err)
	}
	service := &a.Service{Store: s, Hasher: passwordhash.Bcrypt{}}
	login := func(audience string) a.LoginResult {
		t.Helper()
		v, err := service.Login(ctx, op.Email, "test-password-at-least-12", "", audience)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ws, ops := login(a.Workspace), login(a.Operations)
	h, handler := httpapi.New(&projectapp.Service{}, nil, t.TempDir(), 0)
	h.StreamHeartbeat = appconfig.StreamHeartbeat
	h.StreamWriteTimeout = appconfig.StreamWriteTimeout
	h.Accounts = service
	h.Authenticator = service
	h.Credits = &c.Service{Ledger: s, Queries: s, Updates: s, Users: s}
	defer h.Shutdown(context.Background())
	server := httptest.NewServer(handler)
	defer server.Close()
	open := func(audience string, session a.LoginResult) (<-chan string, <-chan struct{}) {
		t.Helper()
		r, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/credits/events?audience="+audience, nil)
		name := "drama_workspace_session"
		if audience == a.Operations {
			name = "drama_operations_session"
		}
		r.AddCookie(&http.Cookie{Name: name, Value: session.Token})
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		if response.StatusCode != 200 {
			t.Fatal("stream authentication", response.StatusCode)
		}
		lines, done := make(chan string, 16), make(chan struct{})
		go func() {
			defer close(done)
			scanner := bufio.NewScanner(response.Body)
			for scanner.Scan() {
				select {
				case lines <- scanner.Text():
				case <-ctx.Done():
					return
				}
			}
		}()
		select {
		case line := <-lines:
			if line != "event: credits.ready" {
				t.Error("stream ready", line)
			}
		case <-ctx.Done():
			t.Fatal("ready timeout")
		}
		return lines, done
	}
	_, workspaceDone := open(a.Workspace, ws)
	operationsLines, operationsDone := open(a.Operations, ops)
	_ = login(a.Workspace)
	select {
	case <-workspaceDone:
	case <-time.After(5 * time.Second):
		t.Fatal("replaced workspace stream stayed open")
	}
	select {
	case <-operationsDone:
		t.Fatal("workspace replacement closed operations stream")
	default:
	}
	for {
		select {
		case line := <-operationsLines:
			if line == ": keepalive" {
				goto alive
			}
		case <-ctx.Done():
			t.Fatal("operations heartbeat timeout")
		}
	}
alive:
	if _, err := service.Authenticate(ctx, ops.Token); err != nil {
		t.Fatal("other platform session lost", err)
	}
	if _, err := service.Authenticate(ctx, ws.Token); !codeIs(err, "SESSION_REPLACED") {
		t.Fatal("PG did not identify replacement", err)
	}
	if err := s.Logout(ctx, ops.Token); err != nil {
		t.Fatal(err)
	}
	select {
	case <-operationsDone:
	case <-time.After(5 * time.Second):
		t.Fatal("logout did not close operations stream")
	}
}
