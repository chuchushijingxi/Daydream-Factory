package postgres_test

import (
	"bufio"
	"bytes"
	"context"
	"drama-agent/internal/adapter/redisstate"
	"drama-agent/internal/testsupport/pgfixture"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/json"
	"image"
	"image/png"
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

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/notificationmarkdown"
	"drama-agent/internal/application/accounts"
	creditapp "drama-agent/internal/application/credits"
	app "drama-agent/internal/application/notifications"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
)

func TestNotificationRealtimeWithdrawalImagesRealPGHTTP(t *testing.T) {
	base, conn := identityDB(t)
	ctx := context.Background()
	cfg, e := appconfig.Load(os.Getenv("DRAMA_TEST_CONFIG_FILE"))
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(cfg.Database.URL)
	q := u.Query()
	name := identity.New("notice_listener")
	q.Set("application_name", name)
	u.RawQuery = q.Encode()
	listener, e := pgfixture.Open(t, ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(listener.Close)
	account := &accounts.Service{Store: listener}
	seed := func(role string, enabled bool) (ports.Actor, string) {
		id, token := identity.New("notice_rt"), accounts.RandomToken()
		_, e := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,enabled,password_hash) VALUES($1,$2,'隔离通知发布人',$3,$4,$5)`, id, id+"@example.invalid", role, enabled, []byte("unusable-test-only-hash"))
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
	updates := &reconnectableUpdates{}
	updates.state.Store(redisruntime.Connect(t))
	service := &app.Service{Store: listener, Updates: updates, Markdown: notificationmarkdown.Parser{}}
	writer := &app.Service{Store: base, Updates: base, Markdown: notificationmarkdown.Parser{}}
	for _, actor := range []ports.Actor{a, b, op, op2} {
		if _, e := service.ReadAll(ctx, actor); e != nil {
			t.Fatal(e)
		}
	}
	h, handler := httpapi.New(&projectapp.Service{}, nil, t.TempDir(), 0)
	h.StreamHeartbeat = appconfig.StreamHeartbeat
	h.StreamWriteTimeout = appconfig.StreamWriteTimeout
	h.Authenticator = account
	h.Accounts = account
	h.Notifications = service
	h.Credits = &creditapp.Service{Ledger: listener, Queries: listener, Updates: updates, Users: listener}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Cleanup(func() { h.Shutdown(ctx) })
	request := func(method, path, token string, payload any) *http.Response {
		t.Helper()
		var raw []byte
		if data, ok := payload.([]byte); ok {
			raw = data
		} else if payload != nil {
			raw, _ = json.Marshal(payload)
		}
		r, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		if token != "" {
			addSessionCookie(r, token)
		}
		r.Header.Set("Content-Type", "application/json")
		resp, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return resp
	}
	jsonRequest := func(method, path, token string, payload, out any, want int) {
		t.Helper()
		resp := request(method, path, token, payload)
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, raw)
		}
		if out != nil && json.Unmarshal(raw, out) != nil {
			t.Fatal("invalid response", string(raw))
		}
	}
	type stream struct {
		events chan string
		done   chan struct{}
		close  func()
	}
	open := func(path, token string) *stream {
		t.Helper()
		resp := request("GET", path, token, nil)
		if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal("stream", resp.StatusCode)
		}
		s := &stream{events: make(chan string, 128), done: make(chan struct{}), close: func() { resp.Body.Close() }}
		t.Cleanup(s.close)
		go func() {
			defer close(s.done)
			scan := bufio.NewScanner(resp.Body)
			for scan.Scan() {
				line := scan.Text()
				if strings.HasPrefix(line, "event: ") {
					s.events <- strings.TrimPrefix(line, "event: ")
				}
				if strings.HasPrefix(line, "data: ") && line != "data: {}" {
					s.events <- "LEAK:" + line
				}
			}
		}()
		return s
	}
	event := func(s *stream, want string) {
		t.Helper()
		select {
		case got := <-s.events:
			if got != want {
				t.Fatal("event", got, want)
			}
		case <-s.done:
			t.Fatal("stream ended")
		case <-time.After(4 * time.Second):
			t.Fatal("event missing", want)
		}
	}
	quiet := func(s *stream) {
		t.Helper()
		select {
		case got := <-s.events:
			t.Fatal("unexpected event", got)
		case <-s.done:
			t.Fatal("unexpected disconnect")
		case <-time.After(100 * time.Millisecond):
		}
	}
	flushRuntime := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			if err := base.FlushRuntimeEvents(ctx); err != nil {
				t.Fatal(err)
			}
			var pending int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.runtime_events`).Scan(&pending); err != nil {
				t.Fatal(err)
			}
			if pending == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("runtime delivery did not settle")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	flushRuntime()
	jsonRequest("GET", "/api/notifications/events", "", nil, nil, 401)
	jsonRequest("HEAD", "/api/notifications/events", ta, nil, nil, 405)
	sa, sb, sop, sc := open("/api/notifications/events", ta), open("/api/notifications/events", tb), open("/api/notifications/events", to), open("/api/credits/events", ta)
	for _, s := range []*stream{sa, sb, sop} {
		event(s, "notifications.ready")
	}
	event(sc, "credits.ready")
	var listens int
	if e = conn.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE application_name=$1 AND query LIKE 'LISTEN drama_agent_credit_updates%'`, name).Scan(&listens); e != nil || listens != 0 {
		t.Fatal("Redis streams opened a PG listener", listens, e)
	}
	var draft app.Message
	jsonRequest("POST", "/api/operations/notifications", to, app.Input{CommandID: identity.New("notice_cmd"), Title: "独立通知联调", Audience: "user", RecipientUserID: a.UserID}, &draft, 201)
	event(sop, "notifications.changed")
	if draft.AuthorID != op.UserID || draft.AuthorName != "隔离通知发布人" || draft.Status != "draft" {
		t.Fatal("publisher fields", draft)
	}
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID+"/publish", to, map[string]uint64{"revision": draft.Revision}, nil, 400)
	var imageBytes bytes.Buffer
	png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 4, 3)))
	var img, duplicate app.Image
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID+"/images", ta, imageBytes.Bytes(), nil, 403)
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID+"/images", to2, imageBytes.Bytes(), nil, 404)
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID+"/images", to, imageBytes.Bytes(), &img, 201)
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID+"/images", to, imageBytes.Bytes(), &duplicate, 201)
	if img.ID != duplicate.ID || img.Width != 4 || img.Height != 3 || img.URL == img.PreviewURL {
		t.Fatal("image receipt", img, duplicate)
	}
	jsonRequest("GET", img.URL, ta, nil, nil, 404)
	jsonRequest("GET", img.PreviewURL, to2, nil, nil, 404)
	jsonRequest("GET", img.PreviewURL, ta, nil, nil, 403)
	preview := request("HEAD", img.PreviewURL, to, nil)
	preview.Body.Close()
	if preview.StatusCode != 200 || preview.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatal("owner draft preview", preview.StatusCode)
	}
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID, to, app.Input{Revision: draft.Revision, Title: draft.Title, Body: "![invalid](/api/files/private/original)", Audience: "user", RecipientUserID: a.UserID}, nil, 400)
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID, to, app.Input{Revision: draft.Revision, Title: draft.Title, Body: "![forged](/api/notifications/" + draft.ID + "/images/another_message_image)", Audience: "user", RecipientUserID: a.UserID}, nil, 400)
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID, to, app.Input{Revision: draft.Revision, Title: draft.Title, Body: "第一行\n第二行\n\n![图](" + img.URL + ")", Audience: "user", RecipientUserID: a.UserID}, &draft, 200)
	event(sop, "notifications.changed")
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID+"/publish", to, map[string]uint64{"revision": draft.Revision}, &draft, 200)
	event(sa, "notifications.changed")
	event(sop, "notifications.changed")
	quiet(sb)
	quiet(sc)
	response := request("GET", img.URL, ta, nil)
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Equal(data, imageBytes.Bytes()) || response.Header.Get("Content-Type") != "image/png" {
		t.Fatal("recipient image", response.StatusCode)
	}
	jsonRequest("GET", img.URL, tb, nil, nil, 404)
	var detail app.Detail
	jsonRequest("GET", "/api/notifications/"+draft.ID, ta, nil, &detail, 200)
	if !strings.Contains(detail.Body, "第一行\n第二行") {
		t.Fatal("stored newlines changed")
	}
	var page app.Page[app.Item]
	jsonRequest("GET", "/api/notifications?status=unread", ta, nil, &page, 200)
	if len(page.Items) != 1 || page.Items[0].Summary != "第一行 第二行" || detail.Summary != page.Items[0].Summary {
		t.Fatal("summary leaked image or lost text", page)
	}
	var unread struct {
		Unread int64 `json:"unread"`
	}
	jsonRequest("GET", "/api/notifications/unread", ta, nil, &unread, 200)
	if unread.Unread != 1 {
		t.Fatal("unread", unread)
	}
	jsonRequest("POST", "/api/notifications/"+draft.ID+"/read", ta, map[string]any{}, nil, 200)
	event(sa, "notifications.changed")
	quiet(sb)
	flushRuntime()
	settled := time.NewTimer(200 * time.Millisecond)
drainRead:
	for {
		select {
		case <-sa.events:
		case <-settled.C:
			break drainRead
		}
	}
	jsonRequest("POST", "/api/notifications/"+draft.ID+"/read", ta, map[string]any{}, nil, 200)
	quiet(sa)
	// A different Store/connection writes: delivery is not an in-process callback.
	broadcast, e := writer.Create(ctx, op, app.Input{CommandID: identity.New("notice_cmd"), Title: "所有启用账号", Body: "隔离测试正文", Audience: "all"})
	if e != nil {
		t.Fatal(e)
	}
	event(sop, "notifications.changed")
	broadcastImage, e := writer.UploadImage(ctx, op, broadcast.ID, imageBytes.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	broadcast, e = writer.Update(ctx, op, broadcast.ID, app.Input{Revision: broadcast.Revision, Title: broadcast.Title, Body: "历史全体消息\n\n![通知图片](" + broadcastImage.URL + ")", Audience: "all"})
	if e != nil {
		t.Fatal(e)
	}
	event(sop, "notifications.changed")
	broadcast, e = writer.Publish(ctx, op, broadcast.ID, broadcast.Revision)
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []*stream{sa, sb, sop} {
		event(s, "notifications.changed")
	}
	if _, e = service.Detail(ctx, op2, broadcast.ID); e != nil {
		t.Fatal("operator not included", e)
	}
	if _, e = service.Detail(ctx, disabled, broadcast.ID); e == nil {
		t.Fatal("disabled included")
	}
	// A later account needs no materialized receipt to read an all-message or
	// its image. Withdrawal must also reach that account's already open stream.
	future, tfuture := seed("user", true)
	var materialized int
	if e = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.notification_receipts WHERE message_id=$1 AND user_id=$2`, broadcast.ID, future.UserID).Scan(&materialized); e != nil || materialized != 0 {
		t.Fatal("future user was backfilled", e)
	}
	jsonRequest("GET", "/api/notifications/"+broadcast.ID, tfuture, nil, &detail, 200)
	futureImage := request("GET", broadcastImage.URL, tfuture, nil)
	futureBytes, _ := io.ReadAll(futureImage.Body)
	futureImage.Body.Close()
	if futureImage.StatusCode != 200 || !bytes.Equal(futureBytes, imageBytes.Bytes()) {
		t.Fatal("future recipient image", futureImage.StatusCode)
	}
	futureStream := open("/api/notifications/events", tfuture)
	event(futureStream, "notifications.ready")
	jsonRequest("POST", "/api/notifications/read-all", ta, map[string]any{}, nil, 200)
	event(sa, "notifications.changed")
	// Withdrawing preserves history but removes every recipient read surface.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := writer.Withdraw(ctx, op, broadcast.ID, broadcast.Revision)
			if e != nil || v.Status != "withdrawn" || v.WithdrawnAt == nil {
				t.Errorf("withdraw race %+v %v", v, e)
			}
		}()
	}
	wg.Wait()
	for _, s := range []*stream{sa, sb, sop} {
		event(s, "notifications.changed")
	}
	event(futureStream, "notifications.changed")
	jsonRequest("GET", "/api/notifications/"+broadcast.ID, tfuture, nil, nil, 404)
	jsonRequest("GET", broadcastImage.URL, tfuture, nil, nil, 404)
	jsonRequest("POST", "/api/notifications/"+broadcast.ID+"/read", tfuture, map[string]any{}, nil, 404)
	for _, token := range []string{ta, tb, to, to2} {
		jsonRequest("GET", "/api/notifications/"+broadcast.ID, token, nil, nil, 404)
		jsonRequest("POST", "/api/notifications/"+broadcast.ID+"/read", token, map[string]any{}, nil, 404)
	}
	if _, e = writer.Withdraw(ctx, op, draft.ID, draft.Revision); e != nil {
		t.Fatal(e)
	}
	event(sa, "notifications.changed")
	event(sop, "notifications.changed")
	for _, method := range []string{"GET", "HEAD"} {
		jsonRequest(method, img.URL, ta, nil, nil, 404)
		jsonRequest(method, img.URL, to, nil, nil, 404)
	}
	jsonRequest("GET", "/api/notifications/unread", ta, nil, &unread, 200)
	if unread.Unread != 0 {
		t.Fatal("withdrawn counted", unread)
	}
	jsonRequest("GET", "/api/notifications", ta, nil, &page, 200)
	for _, item := range page.Items {
		if item.ID == broadcast.ID || item.ID == draft.ID {
			t.Fatal("withdrawn summary remained visible", item.ID)
		}
	}
	jsonRequest("POST", "/api/operations/notifications/"+draft.ID+"/publish", to, map[string]uint64{"revision": draft.Revision + 1}, nil, 409)
	var receipts int
	if e = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.notification_receipts WHERE message_id=$1`, broadcast.ID).Scan(&receipts); e != nil || int64(receipts) != broadcast.RecipientCount {
		t.Fatal("withdraw deleted receipts", receipts, e)
	}
	// Rolled-back DB facts do not emit refresh hints.
	flushRuntime()
	// Earlier committed facts may produce coalesced or repeated repair hints.
	// Drain those before observing a transaction that must produce no new work.
	for _, stream := range []*stream{sa, sop} {
		settled := time.NewTimer(200 * time.Millisecond)
	drain:
		for {
			select {
			case <-stream.events:
			case <-settled.C:
				break drain
			}
		}
	}
	tx, e := conn.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	tx.Exec(ctx, `UPDATE drama_catalog_v1.notification_messages SET title='rollback' WHERE id=$1`, draft.ID)
	tx.Rollback(ctx)
	quiet(sa)
	quiet(sop)
	// Listener loss closes subscribers; reconnect ready recovers authoritative state.
	updates.state.Load().Close()
	select {
	case <-sa.done:
	case <-time.After(4 * time.Second):
		t.Fatal("listener loss left stale stream")
	}
	updates.state.Store(redisruntime.Connect(t))
	reconnected := open("/api/notifications/events", ta)
	event(reconnected, "notifications.ready")
	jsonRequest("GET", "/api/notifications/unread", ta, nil, &unread, 200)
	if unread.Unread != 0 {
		t.Fatal("reconnect state")
	}
	redisruntime.SeedDigest(t, a.UserID, accounts.Workspace, accounts.Digest("workspace:"+accounts.RandomToken()), a.Role, time.Now().Add(time.Hour))

	select {
	case <-reconnected.done:
	case <-time.After(5 * time.Second):
		t.Fatal("replaced session did not close notification stream")
	}
	jsonRequest("GET", "/api/notifications/events", ta, nil, nil, 401)
	logoutStream := open("/api/notifications/events", tb)
	event(logoutStream, "notifications.ready")
	jsonRequest("POST", "/api/auth/logout", tb, map[string]any{}, nil, 200)
	select {
	case <-logoutStream.done:
	case <-time.After(5 * time.Second):
		t.Fatal("logout left old notification stream alive")
	}
	disabledStream := open("/api/notifications/events", to2)
	event(disabledStream, "notifications.ready")
	if _, e = base.SetUserEnabled(ctx, op2.UserID, false); e != nil {
		t.Fatal(e)
	}
	select {
	case <-disabledStream.done:
	case <-time.After(5 * time.Second):
		t.Fatal("disable left old notification stream alive")
	}
	t.Log("real PG + HTTP: author/list fields, newline preservation, image upload/dedupe/preview/recipient/HEAD isolation, all enabled roles, publish/read/read-all/withdraw invalidation, concurrent withdrawal, receipt retention, rollback, shared cross-process listener, reconnect and session revocation passed")
}

type reconnectableUpdates struct {
	state atomic.Pointer[redisstate.Store]
}

func (s *reconnectableUpdates) WatchCreditAccount(ctx context.Context, user string) (<-chan struct{}, func(), error) {
	return s.state.Load().WatchCreditAccount(ctx, user)
}
func (s *reconnectableUpdates) WatchNotifications(ctx context.Context, user string) (<-chan struct{}, func(), error) {
	return s.state.Load().WatchNotifications(ctx, user)
}
