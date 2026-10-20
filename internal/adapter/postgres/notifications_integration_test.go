package postgres_test

import (
	"bytes"
	"context"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/notificationmarkdown"
	"drama-agent/internal/adapter/passwordhash"
	accounts "drama-agent/internal/application/accounts"
	app "drama-agent/internal/application/notifications"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type notificationAuth struct {
	source ports.SessionAuthenticator
	calls  atomic.Int64
}

func (a *notificationAuth) Authenticate(ctx context.Context, token string) (ports.Actor, error) {
	a.calls.Add(1)
	return a.source.Authenticate(ctx, token)
}
func TestNotificationsPGHTTPIsolationPublishAndRead(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	// Earlier unreleased test-only migrations may have made an empty table before the digest field.
	if _, e := conn.Exec(ctx, `ALTER TABLE drama_catalog_v1.notification_messages ADD COLUMN IF NOT EXISTS creation_digest text`); e != nil {
		t.Fatal(e)
	}
	var missing int
	if e := conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.notification_messages WHERE creation_digest IS NULL`).Scan(&missing); e != nil || missing != 0 {
		t.Fatalf("refuse to invent historical notification hashes: %d %v", missing, e)
	}
	if _, e := conn.Exec(ctx, `ALTER TABLE drama_catalog_v1.notification_messages ALTER COLUMN creation_digest SET NOT NULL`); e != nil {
		t.Fatal(e)
	}
	account := &accounts.Service{Store: s, Hasher: passwordhash.Bcrypt{}}
	seed := func(role string, enabled bool) (ports.Actor, string) {
		t.Helper()
		id := identity.New("noticeuser")
		token := accounts.RandomToken()
		_, e := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,enabled,password_hash) VALUES($1,$2,'隔离站内信测试',$3,$4,$5)`, id, id+"@example.test", role, enabled, []byte("unusable-test-only-hash"))
		if e != nil {
			t.Fatal(e)
		}
		audience := "workspace"
		if role == "operator" {
			audience = "operations"

		}
		if enabled {
			redisruntime.SeedDigest(t, id, audience, accounts.Digest(audience+":"+token), ports.Role(role), time.Now().Add(time.Hour))
		}
		return ports.Actor{UserID: id, Role: ports.Role(role)}, id + "." + audience + "." + token
	}
	a, ta := seed("user", true)
	b, tb := seed("user", true)
	op, to := seed("operator", true)
	op2, to2 := seed("operator", true)
	disabled, _ := seed("user", false)
	service := &app.Service{Store: s, Markdown: notificationmarkdown.Parser{}}
	for _, actor := range []ports.Actor{a, b, op, op2} {
		if _, e := service.ReadAll(ctx, actor); e != nil {
			t.Fatal(e)
		}
	}
	h, handler := httpapi.New(&projectapp.Service{}, nil, "../../../demo", 0)
	auth := &notificationAuth{source: account}
	h.Authenticator = auth
	h.Accounts = account
	h.Notifications = service
	server := httptest.NewServer(handler)
	defer server.Close()
	request := func(method, path, token string, payload any) (int, []byte) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			raw, _ := json.Marshal(payload)
			body = bytes.NewReader(raw)
		}
		r, e := http.NewRequest(method, server.URL+path, body)
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			addSessionCookie(r, token)
		}
		before := auth.calls.Load()
		res, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(res.Body)
		if e != nil {
			t.Fatal(e)
		}
		if token != "" && auth.calls.Load() != before+1 {
			t.Fatal("request did not authenticate once")
		}
		return res.StatusCode, raw
	}
	for _, r := range h.Routes() {
		if strings.Contains(r.Pattern, "notifications") || strings.Contains(r.Pattern, "notification-commands") {
			if r.Access == "public" {
				t.Fatalf("public message route %s", r.Pattern)
			}
		}
	}
	for _, p := range []string{"/api/notifications", "/api/notifications/unread", "/api/operations/notifications"} {
		if status, _ := request("GET", p, "", nil); status != 401 {
			t.Fatalf("anonymous %s: %d", p, status)
		}
	}
	if status, _ := request("POST", "/api/operations/notifications", ta, map[string]any{}); status != 403 {
		t.Fatal("ordinary create", status)
	}
	in := app.Input{CommandID: identity.New("command"), Title: "测试通知", Body: "## 积分\n\n**测试正文**，不含任何用户业务内容。", Audience: "user", RecipientUserID: a.UserID}
	status, raw := request("POST", "/api/operations/notifications", to, in)
	if status != 201 {
		t.Fatalf("create %d %s", status, raw)
	}
	var draft app.Message
	if e := json.Unmarshal(raw, &draft); e != nil {
		t.Fatal(e)
	}
	if draft.RecipientName != "隔离站内信测试" || draft.RecipientEmail != a.UserID+"@example.test" {
		t.Fatal("recipient username missing", draft.RecipientName)
	}
	duplicate, e := service.Create(ctx, op, in)
	if e != nil || duplicate.ID != draft.ID {
		t.Fatal("same command did not reread", duplicate, e)
	}
	conflict := in
	conflict.Body = "different"
	if _, e = service.Create(ctx, op, conflict); !codeIs(e, "NOTIFICATION_COMMAND_CONFLICT") {
		t.Fatal("command conflict", e)
	}
	if n, e := service.Unread(ctx, a); e != nil || n != 0 {
		t.Fatal("draft delivered", n, e)
	}
	if status, _ = request("GET", "/api/notifications/"+draft.ID, ta, nil); status != 404 {
		t.Fatal("draft user readable", status)
	}
	if status, _ = request("GET", "/api/operations/notifications/"+draft.ID, to2, nil); status != 404 {
		t.Fatal("operator exemption", status)
	}
	if status, _ = request("POST", "/api/operations/notifications/"+draft.ID, to2, app.Input{Title: "stolen", Body: "body", Audience: "all", Revision: 1}); status != 404 {
		t.Fatal("operator write exemption", status)
	}
	edit := in
	edit.CommandID = ""
	edit.Revision = 1
	edit.Title = "更新后的标题"
	status, raw = request("POST", "/api/operations/notifications/"+draft.ID, to, map[string]any{"title": edit.Title, "body": edit.Body, "audience": edit.Audience, "recipientUserId": edit.RecipientUserID, "revision": edit.Revision})
	var saved app.Message
	if status != 200 {
		t.Fatalf("actual frontend update payload %d %s", status, raw)
	}
	if e = json.Unmarshal(raw, &saved); e != nil || saved.Revision != 2 {
		t.Fatal(saved, e)
	}
	// The display name is a live account projection, never a replacement identity.
	if _, e = conn.Exec(ctx, `UPDATE drama_catalog_v1.users SET username='接收人新名称🌙' WHERE id=$1`, a.UserID); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/operations/notifications/" + draft.ID, "/api/operations/notification-commands/" + in.CommandID} {
		var got app.Message
		status, raw = request("GET", path, to, nil)
		if e = json.Unmarshal(raw, &got); status != 200 || e != nil || got.RecipientName != "接收人新名称🌙" || got.RecipientUserID != a.UserID || got.RecipientEmail != a.UserID+"@example.test" {
			t.Fatal("recipient projection", status, string(raw), e)
		}
	}
	status, raw = request("GET", "/api/operations/notifications", to, nil)
	var operatorPage app.Page[app.Message]
	if e = json.Unmarshal(raw, &operatorPage); status != 200 || e != nil || len(operatorPage.Items) != 1 || operatorPage.Items[0].RecipientName != "接收人新名称🌙" || operatorPage.Items[0].RecipientEmail != a.UserID+"@example.test" {
		t.Fatal("recipient list projection", status, string(raw), e)
	}
	if _, e = service.Update(ctx, op, draft.ID, edit); !codeIs(e, "NOTIFICATION_CONFLICT") {
		t.Fatal("revision ignored", e)
	}
	if v, e := service.Create(ctx, op, in); e != nil || v.ID != draft.ID || v.Title != saved.Title {
		t.Fatal("created command digest was lost after edit", v, e)
	}
	if status, _ = request("POST", "/api/operations/notifications/"+draft.ID+"/publish", to, map[string]int{"revision": 1}); status != 409 {
		t.Fatal("stale publish", status)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := service.Publish(ctx, op, draft.ID, 2)
			if e != nil || v.Revision != 3 || v.RecipientCount != 1 || v.RecipientName != "接收人新名称🌙" {
				t.Errorf("publish race %+v %v", v, e)
			}
		}()
	}
	wg.Wait()
	if v, e := service.Publish(ctx, op, draft.ID, 2); e != nil || v.RecipientCount != 1 {
		t.Fatal("publish retry", v, e)
	}
	if _, e = service.Update(ctx, op, draft.ID, edit); !codeIs(e, "NOTIFICATION_CONFLICT") {
		t.Fatal("published edit", e)
	}
	if status, _ = request("GET", "/api/notifications/"+draft.ID, tb, nil); status != 404 {
		t.Fatal("B read A", status)
	}
	if status, _ = request("POST", "/api/notifications/"+draft.ID+"/read", tb, map[string]any{}); status != 404 {
		t.Fatal("B mutate A", status)
	}
	if status, _ = request("GET", "/api/notifications/"+draft.ID, to, nil); status != 404 {
		t.Fatal("operator content exemption", status)
	}
	if status, raw = request("GET", "/api/notifications?status=unread&page=1&pageSize=1", ta, nil); status != 200 {
		t.Fatal(status, string(raw))
	}
	var list app.Page[app.Item]
	if e := json.Unmarshal(raw, &list); e != nil || list.Unread != 1 || list.Total != 1 || len(list.Items) != 1 || list.Items[0].Summary != "积分 测试正文，不含任何用户业务内容。" || bytes.Contains(raw, []byte(`"body"`)) || bytes.Contains(raw, []byte("**")) {
		t.Fatal("summary privacy/counts", string(raw), e)
	}
	if status, raw = request("GET", "/api/notifications/"+draft.ID, ta, nil); status != 200 || !bytes.Contains(raw, []byte("测试正文")) {
		t.Fatal("own body", status, string(raw))
	}
	var detail app.Detail
	if e := json.Unmarshal(raw, &detail); e != nil || detail.Summary != list.Items[0].Summary || detail.Body != in.Body {
		t.Fatal("detail summary or source changed", e)
	}
	if n, _ := service.Unread(ctx, a); n != 1 {
		t.Fatal("GET detail changed read state")
	}
	status, raw = request("POST", "/api/notifications/"+draft.ID+"/read", ta, map[string]any{})
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	var first app.ReadReceipt
	json.Unmarshal(raw, &first)
	second, e := service.Read(ctx, a, draft.ID)
	if e != nil || !first.ReadAt.Equal(second.ReadAt) {
		t.Fatal("read receipt not idempotent", first, second, e)
	}
	allInput := app.Input{CommandID: identity.New("command"), Title: "全体测试", Body: "**仅隔离测试库**", Audience: "all"}
	all, e := service.Create(ctx, op, allInput)
	if e != nil {
		t.Fatal(e)
	}
	all, e = service.Publish(ctx, op, all.ID, all.Revision)
	if e != nil || all.RecipientCount < 2 {
		t.Fatal(all, e)
	}
	if _, e = service.Detail(ctx, disabled, all.ID); e == nil {
		t.Fatal("disabled user was recipient")
	}
	if _, e = service.Detail(ctx, op2, all.ID); e != nil {
		t.Fatal("enabled operator must receive all-account broadcast", e)
	}
	// Register after publication through the production API, without any
	// registration hook or receipt backfill. Historical all messages are unread.
	email := identity.New("new-notice-user") + "@example.test"
	codeFor(t, s, email, "register", "483726", time.Now().Add(time.Minute))
	_, authorization, e := account.CreateCode(ctx, op, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	status, raw = request("POST", "/api/auth/register", "", map[string]string{"email": email, "username": "新注册收件人", "password": "isolated-notice-password", "code": "483726", "authorizationCode": authorization})
	var registered accounts.User
	if status != 201 || json.Unmarshal(raw, &registered) != nil {
		t.Fatal("register", status, string(raw))
	}
	login, e := account.Login(ctx, email, "isolated-notice-password", "", "workspace")
	if e != nil {
		t.Fatal(e)
	}
	var receiptCount int
	if e = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.notification_receipts WHERE user_id=$1`, registered.ID).Scan(&receiptCount); e != nil || receiptCount != 0 {
		t.Fatal("registration backfilled notifications", receiptCount, e)
	}
	status, raw = request("GET", "/api/notifications/"+all.ID, login.Token, nil)
	var historical app.Detail
	if status != 200 || json.Unmarshal(raw, &historical) != nil || historical.ReadAt != nil || historical.Body != all.Body {
		t.Fatal("new user lost historical broadcast", status, string(raw))
	}
	status, raw = request("GET", "/api/notifications?status=unread&pageSize=1", login.Token, nil)
	var historicalPage app.Page[app.Item]
	if status != 200 || json.Unmarshal(raw, &historicalPage) != nil || historicalPage.Total < 1 || historicalPage.Unread < 1 || len(historicalPage.Items) != 1 || historicalPage.Items[0].ID != all.ID {
		t.Fatal("new user unread/list", status, string(raw))
	}
	if status, _ = request("GET", "/api/notifications/"+draft.ID, login.Token, nil); status != 404 {
		t.Fatal("new user read another user's target", status)
	}
	if status, _ = request("POST", "/api/notifications/"+draft.ID+"/read", login.Token, map[string]any{}); status != 404 {
		t.Fatal("new user marked foreign target", status)
	}
	concurrentReads := make(chan app.ReadReceipt, 5)
	newActor := ports.Actor{UserID: registered.ID, Role: ports.RoleUser}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := service.Read(ctx, newActor, all.ID)
			if e != nil {
				t.Error(e)
				return
			}
			concurrentReads <- v
		}()
	}
	wg.Wait()
	close(concurrentReads)
	status, raw = request("POST", "/api/notifications/"+all.ID+"/read", login.Token, map[string]any{})
	var historicalRead app.ReadReceipt
	if status != 200 || json.Unmarshal(raw, &historicalRead) != nil || historicalRead.ReadAt.IsZero() {
		t.Fatal("new user read", status, string(raw))
	}
	for receipt := range concurrentReads {
		if !receipt.ReadAt.Equal(historicalRead.ReadAt) {
			t.Fatal("first concurrent historical read timestamp changed")
		}
	}
	if e = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.notification_receipts WHERE user_id=$1 AND message_id=$2`, registered.ID, all.ID).Scan(&receiptCount); e != nil || receiptCount != 1 {
		t.Fatal("duplicate historical receipts", receiptCount, e)
	}
	status, raw = request("POST", "/api/notifications/"+all.ID+"/read", login.Token, map[string]any{})
	var repeatRead app.ReadReceipt
	if status != 200 || json.Unmarshal(raw, &repeatRead) != nil || !repeatRead.ReadAt.Equal(historicalRead.ReadAt) {
		t.Fatal("historical read time changed")
	}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := service.ReadAll(ctx, newActor)
			if e != nil || v.Unread != 0 {
				t.Errorf("concurrent historical read-all %+v %v", v, e)
			}
		}()
	}
	wg.Wait()
	if status, _ = request("POST", "/api/notifications/read-all", login.Token, map[string]any{}); status != 200 {
		t.Fatal("new user read-all", status)
	}
	status, raw = request("GET", "/api/notifications/unread", login.Token, nil)
	if status != 200 || string(bytes.TrimSpace(raw)) != `{"unread":0}` {
		t.Fatal("historical read-all count", status, string(raw))
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := service.Read(ctx, b, all.ID)
			if e != nil || v.ReadAt.IsZero() {
				t.Errorf("concurrent read %+v %v", v, e)
			}
		}()
	}
	wg.Wait()
	if n, e := service.Unread(ctx, b); e != nil || n != 0 {
		t.Fatal("concurrent read did not persist", n, e)
	}
	allResult, e := service.ReadAll(ctx, a)
	if e != nil || allResult.Updated != 1 || allResult.Unread != 0 {
		t.Fatal(allResult, e)
	}
	if n, e := service.Unread(ctx, b); e != nil || n != 0 {
		t.Fatal("A read-all changed B", n, e)
	}
	if status, _ = request("GET", "/api/operations/notification-commands/"+in.CommandID, to, nil); status != 200 {
		t.Fatal("command readback", status)
	}
	if status, _ = request("GET", "/api/operations/notifications?status=unread", to, nil); status != 400 {
		t.Fatal("ignored operator filter", status)
	}
	if status, _ = request("GET", "/api/notifications?page=-1", ta, nil); status != 400 {
		t.Fatal("invalid paging", status)
	}
	t.Log("PG + production HTTP: A/B/operator isolation, exact owner/revision, draft/create idempotency, concurrent publish-once, explicit read, read-all, historical broadcasts after real registration, targeted isolation and enabled qualification verified")
}
