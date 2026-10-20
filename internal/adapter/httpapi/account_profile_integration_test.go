package httpapi_test

import (
	"bytes"
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/imagethumbnail"
	"drama-agent/internal/adapter/passwordhash"
	a "drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
)

type countingAccounts struct {
	*a.Service
	count atomic.Int32
}

func (c *countingAccounts) Authenticate(ctx context.Context, token string) (ports.Actor, error) {
	c.count.Add(1)
	return c.Service.Authenticate(ctx, token)
}

func TestProfileRealPGHTTP(t *testing.T) {
	path := os.Getenv("DRAMA_TEST_CONFIG_FILE")
	if path == "" {
		t.Skip("dedicated PG required")
	}
	cfg, err := appconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(cfg.Database.URL)
	if !strings.HasPrefix(u.Path, "/drama_accounts_test_") {
		t.Fatal("dedicated account database required")
	}
	ctx := context.Background()
	store, err := pgfixture.Open(t, ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := &a.Service{Store: store, Hasher: passwordhash.Bcrypt{}, Thumbnails: imagethumbnail.FFmpeg{Settings: cfg.Media}}
	counted := &countingAccounts{Service: service}
	h, handler := httpapi.New(&projectapp.Service{}, nil, "../../../demo", 0)
	h.Accounts = service
	h.Authenticator = counted
	defer h.Shutdown(ctx)
	server := httptest.NewServer(handler)
	defer server.Close()
	request := func(method, path, token string, body []byte, contentType string) (int, []byte, http.Header) {
		t.Helper()
		r, e := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		if token != "" {
			addSessionCookie(r, token)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		resp, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		raw, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, raw, resp.Header
	}
	post := func(path, token string, input any) (int, []byte) {
		raw, _ := json.Marshal(input)
		status, raw, _ := request("POST", path, token, raw, "application/json")
		return status, raw
	}
	newUser := func(name string) a.User {
		email := identity.New("profile") + "@example.test"
		challenge := a.RandomToken()
		if e := store.PutVerification(ctx, challenge, email, "register", a.Digest(challenge+":482739"), time.Now().Add(time.Minute)); e != nil {
			t.Fatal(e)
		}
		if e := store.ActivateVerification(ctx, challenge, true); e != nil {
			t.Fatal(e)
		}
		_, code, e := service.CreateCode(ctx, ports.Actor{UserID: "profile-fixture", Role: ports.RoleOperator}, time.Now().Add(time.Hour))
		if e != nil {
			t.Fatal(e)
		}
		input := map[string]string{"email": email, "username": "\n", "password": "profile-fixture-password", "code": "482739", "authorizationCode": code}
		status, raw := post("/api/auth/register", "", input)
		if status != 400 || !bytes.Contains(raw, []byte("INVALID_USERNAME")) {
			t.Fatal("registration name validation", status, string(raw))
		}
		input["username"] = "  " + name + "  "
		status, raw = post("/api/auth/register", "", input)
		var user a.User
		if status != 201 || json.Unmarshal(raw, &user) != nil || user.Username != name {
			t.Fatal("registration transaction", status, string(raw))
		}
		return user
	}
	owner, other := newUser("测试昵称甲"), newUser("测试昵称乙")
	login := func(user a.User, password string) string {
		v, e := service.Login(ctx, user.Email, password, "", "workspace")
		if e != nil {
			t.Fatal(e)
		}
		return v.Token
	}
	token, otherToken := login(owner, "profile-fixture-password"), login(other, "profile-fixture-password")
	for _, r := range h.Routes() {
		if strings.HasPrefix(r.Pattern, "POST /api/auth/profile") || strings.Contains(r.Pattern, "/api/auth/avatar") || strings.Contains(r.Pattern, "/api/auth/password") {
			if r.Access != "user" {
				t.Fatal("unprotected profile route", r)
			}
		}
	}
	status, raw, _ := request("GET", "/api/auth/me", token, nil, "")
	var profile a.Profile
	if status != 200 || json.Unmarshal(raw, &profile) != nil || profile.AvatarURL != nil || profile.User.Username != owner.Username {
		t.Fatal("initial profile", status, string(raw))
	}
	counted.count.Store(0)
	status, raw = post("/api/auth/profile", token, map[string]string{"username": "  新昵称🌿  "})
	if status != 200 || counted.count.Load() != 1 {
		t.Fatal("single authentication", status, string(raw), counted.count.Load())
	}
	if json.Unmarshal(raw, &profile) != nil || profile.User.Username != "新昵称🌿" || profile.User.ID != owner.ID || profile.User.Email != owner.Email || profile.User.Role != ports.RoleUser {
		t.Fatal("profile identity changed", string(raw))
	}
	for _, name := range []string{"", strings.Repeat("界", 41), "hello\nworld"} {
		if status, _ := post("/api/auth/profile", token, map[string]string{"username": name}); status != 400 {
			t.Fatal("invalid username accepted")
		}
	}
	if status, _ := post("/api/auth/profile", token, map[string]string{"username": "x", "userId": other.ID}); status != 400 {
		t.Fatal("identity input accepted")
	}
	status, raw, _ = request("GET", "/api/auth/me", otherToken, nil, "")
	if status != 200 || !bytes.Contains(raw, []byte(other.Username)) {
		t.Fatal("other profile changed")
	}
	// A real generated image exercises the shared decoder/encoder and PG bytes.
	img := image.NewNRGBA(image.Rect(0, 0, 600, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 600; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 80, G: 130, B: 200, A: 255})
		}
	}
	var source bytes.Buffer
	if err = png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	upload := func(data []byte) (int, []byte) {
		var form bytes.Buffer
		writer := multipart.NewWriter(&form)
		part, _ := writer.CreateFormFile("file", "avatar.png")
		part.Write(data)
		writer.Close()
		status, raw, _ := request("POST", "/api/auth/avatar", token, form.Bytes(), writer.FormDataContentType())
		return status, raw
	}
	if status, _ := upload([]byte("<svg>bad</svg>")); status != 400 {
		t.Fatal("active content accepted")
	}
	if status, _ := upload([]byte("\x89PNG\r\n\x1a\ncorrupt")); status != 400 {
		t.Fatal("corrupt image accepted")
	}
	if status, _ := upload(make([]byte, a.MaxAvatarBytes+1)); status != 400 {
		t.Fatal("oversized image accepted")
	}
	status, raw = upload(source.Bytes())
	if status != 200 || json.Unmarshal(raw, &profile) != nil || profile.AvatarURL == nil {
		t.Fatal("avatar upload", status, string(raw))
	}

	op, _, err := service.InitializeOperator(ctx, "fixture-operator-password-123")
	if err != nil {
		t.Fatal(err)
	}
	opLogin, err := service.Login(ctx, op.Email, "fixture-operator-password-123", "", "operations")
	if err != nil {
		t.Fatal(err)
	}
	if status, _, _ := request("GET", *profile.AvatarURL, opLogin.Token, nil, ""); status != 404 {
		t.Fatal("operator content exemption")
	}
	status, opsRaw, _ := request("GET", "/api/operations/users?q="+url.QueryEscape(owner.Email), opLogin.Token, nil, "")
	if status != 200 {
		t.Fatal("operator projection", status, string(opsRaw))
	}
	for _, private := range []string{"avatarUrl", "avatar_data", "avatar_digest", "password_hash", "session_hash", *profile.AvatarURL} {
		if bytes.Contains(opsRaw, []byte(private)) {
			t.Fatal("private profile data in operator list")
		}
	}
	avatarURL := *profile.AvatarURL
	status, raw, headers := request("GET", avatarURL, token, nil, "")
	if status != 200 || http.DetectContentType(raw) != "image/webp" || !strings.Contains(headers.Get("Cache-Control"), "no-store") {
		t.Fatal("avatar binary", status, headers)
	}
	if status, _, _ := request("GET", avatarURL, "", nil, ""); status != 401 {
		t.Fatal("anonymous avatar")
	}
	if status, _, _ := request("GET", avatarURL+"&userId="+owner.ID, otherToken, nil, ""); status != 404 {
		t.Fatal("cross owner avatar")
	}
	if status, raw, _ := request("HEAD", avatarURL, token, nil, ""); status != 200 || len(raw) != 0 {
		t.Fatal("avatar head")
	}
	rangeReq, _ := http.NewRequest("GET", server.URL+avatarURL, nil)
	addSessionCookie(rangeReq, token)
	rangeReq.Header.Set("Range", "bytes=0-9")
	rangeResp, e := server.Client().Do(rangeReq)
	if e != nil {
		t.Fatal(e)
	}
	rangeRaw, _ := io.ReadAll(rangeResp.Body)
	rangeResp.Body.Close()
	if rangeResp.StatusCode != 206 || len(rangeRaw) != 10 {
		t.Fatal("avatar range")
	}
	if status, raw, _ := request("DELETE", "/api/auth/avatar", token, nil, ""); status != 200 || !bytes.Contains(raw, []byte(`"avatarUrl":null`)) {
		t.Fatal("remove avatar", status, string(raw))
	}
	if status, _, _ := request("GET", avatarURL, token, nil, ""); status != 404 {
		t.Fatal("deleted avatar served")
	}
	status, raw = post("/api/auth/password", token, map[string]string{"currentPassword": "wrong", "password": "new-profile-fixture-password"})
	if status != 400 || !bytes.Contains(raw, []byte("CURRENT_PASSWORD_INVALID")) {
		t.Fatal("wrong current password", status, string(raw))
	}
	if _, err = service.Authenticate(ctx, token); err != nil {
		t.Fatal("failed change revoked session")
	}
	// A request authenticated before a concurrent replacement cannot change the new session.
	oldToken := token
	token = login(owner, "profile-fixture-password")
	if err = service.ChangePassword(ctx, ports.Actor{UserID: owner.ID, Role: ports.RoleUser}, oldToken, "profile-fixture-password", "stale-profile-fixture-password"); err == nil {
		t.Fatal("stale password change")
	}
	var committed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := service.ChangePassword(ctx, ports.Actor{UserID: owner.ID, Role: ports.RoleUser}, token, "profile-fixture-password", "new-profile-fixture-password"); e == nil {
				committed.Add(1)
			}
		}()
	}
	wg.Wait()
	if committed.Load() != 1 {
		t.Fatal("concurrent password changes", committed.Load())
	}
	if _, err = service.Authenticate(ctx, token); err == nil {
		t.Fatal("old session survived password change")
	}
	if _, err = service.Login(ctx, owner.Email, "profile-fixture-password", "", "workspace"); err == nil {
		t.Fatal("old password survived")
	}
	token = login(owner, "new-profile-fixture-password")
	status, raw, _ = request("GET", "/api/auth/me", token, nil, "")
	if status != 200 || !bytes.Contains(raw, []byte("新昵称🌿")) {
		t.Fatal("profile not persisted across login")
	}
	status, raw = post("/api/auth/password", token, map[string]string{"currentPassword": "new-profile-fixture-password", "password": "final-profile-fixture-password"})
	if status != 200 {
		t.Fatal("HTTP password change", status, string(raw))
	}
	if status, _, _ := request("GET", "/api/auth/me", token, nil, ""); status != 401 {
		t.Fatal("HTTP change did not revoke")
	}
	login(owner, "final-profile-fixture-password")
	t.Log("Real PG + TCP HTTP: registration/profile persistence, single authentication, private re-encoded avatar GET/HEAD/Range/delete, wrong/stale/concurrent and successful password changes verified; no model calls")
}
