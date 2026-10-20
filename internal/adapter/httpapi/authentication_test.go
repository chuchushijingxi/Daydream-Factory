package httpapi

import (
	"context"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/fault"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type countingAuthenticator struct {
	count   atomic.Int32
	role    ports.Role
	revoked atomic.Bool
}

func (a *countingAuthenticator) Authenticate(context.Context, string) (ports.Actor, error) {
	a.count.Add(1)
	if a.revoked.Load() {
		return ports.Actor{}, fault.New("SESSION_REPLACED", "session replaced")
	}
	return ports.Actor{UserID: "fixture-user", Role: a.role}, nil
}
func TestProductionRegistrationDefaultsAndSingleAuthentication(t *testing.T) {
	host, handler := New(&app.Service{}, nil, t.TempDir(), 0)
	auth := &countingAuthenticator{role: ports.RoleUser}
	host.Authenticator = auth
	// The same registry given to every production module gets a newly added route.
	host.registry.HandleFunc("GET /api/new-business-route", func(w http.ResponseWriter, r *http.Request) {
		if actor(r).UserID != "fixture-user" {
			t.Error("shared Actor missing")
		}
		write(w, 200, map[string]bool{"ok": true})
	})
	for _, route := range host.Routes() {
		method, path, _ := strings.Cut(route.Pattern, " ")
		path = strings.ReplaceAll(path, "{$}", "")
		for strings.Contains(path, "{") {
			a := strings.Index(path, "{")
			z := strings.Index(path[a:], "}") + a
			path = path[:a] + "fixture" + path[z+1:]
		}
		req := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if route.Access != "public" && rec.Code != 401 {
			t.Errorf("unprotected %s: %d", route.Pattern, rec.Code)
		}
		if route.Access == "public" && rec.Code == 401 {
			t.Errorf("declared public route challenged %s", route.Pattern)
		}
	}
	if auth.count.Load() != 0 {
		t.Fatal("anonymous request called authenticator without cookie")
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1:8787/api/new-business-route", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "fixture-user.workspace.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 || auth.count.Load() != 1 {
		t.Fatal("ordinary request did not authenticate exactly once", rec.Code, auth.count.Load())
	}
	req = httptest.NewRequest("GET", "http://127.0.0.1:8787/api/model-config", nil)
	req.Header.Set("X-Drama-Audience", "operations")
	req.AddCookie(&http.Cookie{Name: operationsSessionCookie, Value: "fixture-user.operations.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 403 || auth.count.Load() != 2 {
		t.Fatal("shared role check", rec.Code, auth.count.Load())
	}
}
func TestLongWriterRechecksRevocation(t *testing.T) {
	auth := &countingAuthenticator{role: ports.RoleUser}
	rec := httptest.NewRecorder()
	w := &sessionWriter{ResponseWriter: rec, auth: auth, token: "test", ctx: context.Background(), checked: time.Now().Add(-3 * time.Second)}
	auth.revoked.Store(true)
	if n, e := w.Write([]byte("private")); e == nil || n != 0 || rec.Body.Len() != 0 || auth.count.Load() != 1 {
		t.Fatal("revoked stream leaked content", n, e)
	}
}

func TestLongOperatorWriterRechecksRole(t *testing.T) {
	auth := &countingAuthenticator{role: ports.RoleUser}
	rec := httptest.NewRecorder()
	w := &sessionWriter{ResponseWriter: rec, auth: auth, token: "test", ctx: context.Background(), checked: time.Now().Add(-3 * time.Second), actor: ports.Actor{UserID: "fixture-user", Role: ports.RoleOperator}, operator: true}
	if n, e := w.Write([]byte("private")); e == nil || n != 0 || rec.Body.Len() != 0 {
		t.Fatal("demoted operator retained stream", n, e)
	}
}

func TestStaticQAFixturesAreNotProductionAnonymousEntrypoints(t *testing.T) {
	root := t.TempDir()
	if e := os.MkdirAll(filepath.Join(root, "__account-fixture"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "__account-fixture", "index.html"), []byte("private-fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	_, handler := New(&app.Service{}, nil, root, 0)
	for _, url := range []string{"/demo/__account-fixture/index.html", "/demo/proposals/media-generation.html", "/demo/README.md"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8787"+url, nil))
		if w.Code != 404 || strings.Contains(w.Body.String(), "private-fixture") {
			t.Fatal("non-shell static entry leaked", url, w.Code)
		}
	}
}

func TestFirstPrivateOutputUsesEntranceAuthenticationTime(t *testing.T) {
	for _, flush := range []bool{false, true} {
		for _, state := range []string{"revoked", "demoted", "replaced", "valid", "short"} {
			t.Run(fmt.Sprintf("flush=%t/%s", flush, state), func(t *testing.T) {
				auth := &countingAuthenticator{role: ports.RoleUser}
				rec := httptest.NewRecorder()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				checked := time.Now().Add(-3 * time.Second)
				if state == "short" {
					checked = time.Now()
				}
				w := &sessionWriter{ResponseWriter: rec, auth: auth, ctx: ctx, cancel: cancel, checked: checked, actor: ports.Actor{UserID: "fixture-user", Role: ports.RoleUser}}
				switch state {
				case "revoked":
					auth.revoked.Store(true)
				case "demoted":
					w.operator = true
					w.actor.Role = ports.RoleOperator
				case "replaced":
					w.actor.UserID = "previous-user"
				}
				if flush {
					w.Flush()
				} else {
					_, _ = w.Write([]byte("private"))
				}
				rejected := state == "revoked" || state == "demoted" || state == "replaced"
				if rejected {
					if rec.Body.Len() != 0 || rec.Flushed || ctx.Err() == nil {
						t.Fatal("unverified first output escaped or cancellation lost")
					}
				} else if flush && !rec.Flushed || !flush && rec.Body.String() != "private" {
					t.Fatal("valid output blocked")
				}
				want := int32(1)
				if state == "short" {
					want = 0
				}
				if auth.count.Load() != want {
					t.Fatal("unexpected authentication count", auth.count.Load(), want)
				}
			})
		}
	}
}
