package httpapi_test

import (
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	d "drama-agent/internal/domain/project"
	"github.com/jackc/pgx/v5"
)

var legacyHTTPActor = ports.Actor{UserID: "legacy-http-fixture", Role: ports.RoleOperator}

func legacyFileQuote(t *testing.T, service *app.Service, fileID string, settings d.ProjectSettings) string {
	t.Helper()
	quote, err := service.ForActor(legacyHTTPActor).QuoteFile(context.Background(), fileID, settings)
	if err != nil {
		t.Fatal("fixture upload quotation", err)
	}
	return quote.Basis
}

func legacyQuote(t *testing.T, service *app.Service, pID string, input app.QuoteInput) string {
	t.Helper()
	quote, err := service.ForActor(legacyHTTPActor).Quote(context.Background(), pID, input)
	if err != nil {
		t.Fatal("fixture quotation", err)
	}
	return quote.Basis
}

func quotedLegacyService(t *testing.T, service *app.Service, pID string, input app.QuoteInput) *app.Service {
	t.Helper()
	return service.ForActor(legacyHTTPActor).WithBillingBasis(legacyQuote(t, service, pID, input))
}

func quoteProjectContent(t *testing.T, serverURL, projectID string, input app.QuoteInput) string {
	t.Helper()
	status, raw := postProjectContent(t, serverURL, "/api/projects/"+projectID+"/quote", input)
	var quote app.ChargeQuote
	if status != 200 || json.Unmarshal(raw, &quote) != nil || quote.Basis == "" {
		t.Fatalf("fixture HTTP quotation: %d %s", status, raw)
	}
	return quote.Basis
}

func quoteUploadedContent(t *testing.T, serverURL, fileID string, settings any) string {
	t.Helper()
	status, raw := postProjectContent(t, serverURL, "/api/projects/quote", map[string]any{"fileId": fileID, "settings": settings})
	var quote app.ChargeQuote
	if status != 200 || json.Unmarshal(raw, &quote) != nil || quote.Basis == "" {
		t.Fatalf("fixture upload quotation: %d %s", status, raw)
	}
	return quote.Basis
}

// 只在本轮隔离库使用的公开测试凭证；认证仍由真实 Redis 会话验证完成。
func postgresFixtureToken(userID, audience string) string {
	return userID + "." + audience + "." + strings.Repeat("A", 43)
}

func seedPostgresFixtureUser(t *testing.T, userID string, role ports.Role) {
	t.Helper()
	address := os.Getenv("DRAMA_TEST_DATABASE_URL")
	u, err := url.Parse(address)
	if err != nil || u.Hostname() != "127.0.0.1" || !strings.HasPrefix(u.Path, "/drama_accounts_test_") {
		t.Fatal("HTTP fixture requires an explicit isolated loopback accounts database")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	secret := strings.Repeat("A", 43)
	_, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,'HTTP fixture',$3,$4) ON CONFLICT(id) DO UPDATE SET enabled=true,role=EXCLUDED.role`, userID, userID+"@example.test", role, []byte("unused-fixture-password-hash"))
	if err != nil {
		t.Fatal(err)
	}
	for _, audience := range []string{accounts.Workspace, accounts.Operations} {
		redisruntime.SeedDigest(t, userID, audience, accounts.Digest(audience+":"+secret), role, time.Now().Add(time.Hour))
	}

}

// withPostgresSession 模拟一个明确已登录的客户端，不绕过生产中间件。
// 固定测试身份让重开连接和 SIGKILL 子进程使用同一归属；原始 handler 仍可验证拒绝行为。
func withPostgresSession(t *testing.T, host *httpapi.Handler, next http.Handler, audience string) http.Handler {
	t.Helper()
	seedPostgresFixtureUser(t, legacyHTTPActor.UserID, legacyHTTPActor.Role)
	store, err := pgfixture.Open(t, context.Background(), os.Getenv("DRAMA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	service := &accounts.Service{Store: store}
	host.Accounts, host.Authenticator = service, service
	host.StreamHeartbeat = appconfig.StreamHeartbeat
	host.StreamWriteTimeout = appconfig.StreamWriteTimeout
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addSessionCookie(r, postgresFixtureToken(legacyHTTPActor.UserID, audience))
		next.ServeHTTP(w, r)
	})
}

func TestLegacyPostgresHTTPFixtureKeepsProductionAccessBoundaries(t *testing.T) {
	store := openProjectContentPG(t)
	service := &app.Service{Store: store, Importer: scriptimport.Importer{}}
	p, err := service.ForActor(legacyHTTPActor).Create(context.Background(), "Owner fixture", "fixture.txt", []byte("完整的虚构原文"), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	host, raw := httpapi.New(service, nil, t.TempDir(), 0)
	defer host.Shutdown(context.Background())
	loggedIn := withPostgresSession(t, host, raw, accounts.Workspace)
	seedPostgresFixtureUser(t, "legacy-http-other", ports.RoleUser)
	request := func(handler http.Handler, path, userID, audience string, want int) {
		t.Helper()
		r := httptest.NewRequest("GET", "http://127.0.0.1:8787"+path, nil)
		if userID != "" {
			addSessionCookie(r, postgresFixtureToken(userID, audience))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: got %d want %d: %s", path, w.Code, want, w.Body.String())
		}
	}
	path := "/api/projects/" + p.ID
	request(raw, path, "", "", 401)
	request(loggedIn, path, "", "", 200)
	request(raw, path, "legacy-http-other", accounts.Workspace, 404)
	request(raw, "/api/model-config", "legacy-http-other", accounts.Operations, 403)
	request(raw, "/api/model-config", legacyHTTPActor.UserID, accounts.Workspace, 403)
	foreignActor := ports.Actor{UserID: "legacy-http-other", Role: ports.RoleUser}
	foreign, err := service.ForActor(foreignActor).Create(context.Background(), "Foreign fixture", "other.txt", []byte("另一用户的完整原文"), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, foreign)
	request(loggedIn, "/api/projects/"+foreign.ID, "", "", 404)
}
