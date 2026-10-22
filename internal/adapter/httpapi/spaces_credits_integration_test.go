package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/accounts"
	creditsapp "drama-agent/internal/application/credits"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	spaceapp "drama-agent/internal/application/spaces"
	credits "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	"drama-agent/internal/testsupport/redisruntime"
)

func TestTeamCreditsHTTPPrivacyAllocationsAndRelease(t *testing.T) {
	store := openProjectContentPG(t)
	runtime := redisruntime.Connect(t)
	store.UseRuntime(runtime)
	ctx := context.Background()
	owner, member, other, outsider, operator := identity.New("credit_owner"), identity.New("credit_member"), identity.New("credit_other"), identity.New("credit_outside"), identity.New("credit_operator")
	for _, user := range []string{owner, member, other, outsider} {
		seedPostgresFixtureUser(t, user, ports.RoleUser)
	}
	seedPostgresFixtureUser(t, operator, ports.RoleOperator)
	host, handler := httpapi.New(&projectapp.Service{Store: store, Resolver: store, Access: store}, nil, t.TempDir(), 0)
	host.StreamHeartbeat = 20 * time.Millisecond
	host.StreamWriteTimeout = time.Second
	host.Authenticator = &accounts.Service{Store: store}
	host.Credits = &creditsapp.Service{Ledger: store, Queries: store, Updates: store, Users: store}
	host.Gifts = &creditsapp.Gifts{Store: store, Cache: runtime}
	host.Spaces = &spaceapp.Service{Store: store, Resolver: store, Limiter: runtime, Credits: store, CreditQueries: store, AllocationQueries: store, CreditUpdates: store}
	defer host.Shutdown(ctx)
	request := func(method, path, user, selected string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "http://127.0.0.1:8792"+path, bytes.NewReader(raw))
		audience := accounts.Workspace
		if user == operator {
			audience = accounts.Operations
		}
		addSessionCookie(r, postgresFixtureToken(user, audience))
		if selected != "" {
			r.Header.Set("X-Drama-Space", selected)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	decode := func(w *httptest.ResponseRecorder, v any) {
		t.Helper()
		if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
			t.Fatal(err)
		}
	}
	var team spaceapp.TeamView
	decode(request("POST", "/api/operations/teams", operator, "", spaceapp.TeamInput{OwnerID: owner, Name: "隔离积分团队", MemberLimit: 3, ExpiresAt: time.Now().Add(90 * 24 * time.Hour), GrantAmount: 500}, 201), &team)
	base := "/api/spaces/" + team.ID
	// 运营的按用户积分查询不受工作台选择头影响，也没有团队流水入口。
	var personal credits.Wallet
	decode(request("GET", "/api/operations/users/"+owner+"/credits/balance", operator, team.ID, nil, 200), &personal)
	if personal.Balance != 0 {
		t.Fatal("selected team replaced operations user target", personal.Balance)
	}
	request("GET", "/api/operations/users/"+team.ID+"/credits/ledger", operator, team.ID, nil, 404)
	var link spaceapp.InviteLink
	decode(request("POST", base+"/invite-links", owner, owner, nil, 201), &link)
	for _, user := range []string{member, other} {
		request("POST", "/api/invite-links/"+link.Token+"/accept", user, user, nil, 200)
	}
	allocation := func(user, holder, kind, command string, amount int64, status int) {
		t.Helper()
		request("POST", base+"/members/"+holder+"/allocation", user, user, map[string]any{"kind": kind, "commandId": command, "amount": amount}, status)
	}
	command := identity.New("allocation")
	allocation(owner, member, "allocate", command, 120, 200)
	allocation(owner, member, "allocate", command, 120, 200)
	allocation(owner, member, "allocate", command, 121, 409)
	allocation(owner, other, "allocate", identity.New("allocation"), 70, 200)
	allocation(member, other, "allocate", identity.New("allocation"), 1, 403)
	allocation(outsider, member, "allocate", identity.New("allocation"), 1, 404)
	var debit credits.Entry
	for _, item := range []struct {
		user   string
		amount int64
	}{{member, 20}, {other, 10}} {
		entry, err := store.Deduct(ctx, credits.Change{AccountID: team.ID, HolderID: item.user, Amount: item.amount, OperationID: identity.New("team_debit"), Source: "http-credit-fixture", Subject: item.user, Reason: "generation"})
		if err != nil {
			t.Fatal(err)
		}
		if item.user == member {
			debit = entry
		}
	}
	var manager map[string]json.RawMessage
	decode(request("GET", "/api/credits/balance", owner, team.ID, nil, 200), &manager)
	for key, want := range map[string]int64{"balance": 470, "totalBalance": 470, "allocated": 160, "unallocated": 310} {
		var got int64
		if json.Unmarshal(manager[key], &got) != nil || got != want {
			t.Fatalf("manager %s: got %s want %d", key, manager[key], want)
		}
	}
	var mine map[string]json.RawMessage
	decode(request("GET", "/api/credits/balance", member, team.ID, nil, 200), &mine)
	var available int64
	if json.Unmarshal(mine["balance"], &available) != nil || available != 100 {
		t.Fatal("member balance is not own allocation", string(mine["balance"]))
	}
	for _, key := range []string{"totalBalance", "allocated", "unallocated"} {
		if mine[key] != nil {
			t.Fatal("member saw team aggregate", key)
		}
	}
	var history spaceapp.CreditHistoryView
	memberHistory := request("GET", "/api/credits/ledger", member, team.ID, nil, 200)
	decode(memberHistory, &history)
	for _, response := range []*httptest.ResponseRecorder{memberHistory, request("GET", "/api/credits/ledger/recent", member, team.ID, nil, 200)} {
		var raw struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		decode(response, &raw)
		if len(raw.Items) != 1 {
			t.Fatal("own debit disappeared from history", response.Body.String())
		}
		for _, item := range raw.Items {
			if item["balance"] != nil {
				t.Fatal("member history exposed account balance", response.Body.String())
			}
		}
	}
	if len(history.Items) != 1 || history.Items[0].ID != debit.ID || history.Items[0].HolderID != member || history.Allocations == nil || len(history.Allocations.Items) != 1 {
		t.Fatalf("member history leaked or lost own facts: %#v", history)
	}
	request("GET", "/api/credits/balance", outsider, team.ID, nil, 404)
	request("POST", "/api/credits/daily-gift", member, team.ID, map[string]any{}, 403)
	request("POST", base+"/credit-requests", member, member, map[string]int64{"needed": 200}, 200)
	request("POST", base+"/credit-requests", member, member, map[string]int64{"needed": 200}, 429)
	// 原生SSE通过query选择团队；长连接不能复用建立时的成员权限。
	server := httptest.NewServer(handler)
	defer server.Close()
	streamRequest, err := http.NewRequest("GET", server.URL+"/api/credits/events?space="+team.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	addSessionCookie(streamRequest, postgresFixtureToken(member, accounts.Workspace))
	stream, err := (&http.Client{Timeout: 2 * time.Second}).Do(streamRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != 200 {
		t.Fatal("team stream rejected", stream.StatusCode)
	}
	reader := bufio.NewReader(stream.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != "event: credits.ready\n" {
		t.Fatal("team stream was not ready", line, err)
	}
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		for {
			if _, err := reader.ReadString('\n'); err != nil {
				return
			}
		}
	}()
	select {
	case <-ended:
		t.Fatal("authorized member stream ended before removal")
	case <-time.After(3 * host.StreamHeartbeat):
	}
	request("DELETE", base+"/members/"+member, owner, owner, nil, 200)
	select {
	case <-ended:
	case <-time.After(750 * time.Millisecond):
		t.Fatal("removed member kept receiving team credit stream")
	}
	request("GET", "/api/credits/balance", member, team.ID, nil, 404)
	if _, err := store.Refund(ctx, credits.Change{HolderID: debit.HolderID, OriginalDeductionID: debit.ID, Amount: debit.Amount, OperationID: identity.New("team_refund"), Subject: member, Reason: "generation-incomplete"}); err != nil {
		t.Fatal(err)
	}
	decode(request("GET", "/api/credits/balance", owner, team.ID, nil, 200), &manager)
	for key, want := range map[string]int64{"totalBalance": 490, "allocated": 60, "unallocated": 430} {
		var got int64
		if json.Unmarshal(manager[key], &got) != nil || got != want {
			t.Fatalf("after release/refund %s: got %s want %d", key, manager[key], want)
		}
	}
	// 全额收回只需all，不要求无意义的amount；重试回读原60，不能再收回一次。
	reclaimCommand := identity.New("reclaim")
	for i := 0; i < 2; i++ {
		var event credits.AllocationEvent
		decode(request("POST", base+"/members/"+other+"/allocation", owner, owner, map[string]any{"kind": "reclaim", "commandId": reclaimCommand, "all": true}, 200), &event)
		if event.Amount != 60 {
			t.Fatal("all reclaim did not return original result", event.Amount)
		}
	}
	decode(request("GET", "/api/credits/balance", owner, team.ID, nil, 200), &manager)
	for key, want := range map[string]int64{"totalBalance": 490, "allocated": 0, "unallocated": 490} {
		var got int64
		if json.Unmarshal(manager[key], &got) != nil || got != want {
			t.Fatalf("after all reclaim %s: got %s want %d", key, manager[key], want)
		}
	}
}
