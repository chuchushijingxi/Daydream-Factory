package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/application/accounts"
	creditsapp "drama-agent/internal/application/credits"
	projectapp "drama-agent/internal/application/project"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	"drama-agent/internal/testsupport/pgfixture"
	"github.com/jackc/pgx/v5"
)

func TestRecentCreditEntriesRealPGHTTP(t *testing.T) {
	address := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if address == "" {
		t.Skip("isolated recent-credit PG and Redis required")
	}
	parsed, err := url.Parse(address)
	if err != nil || !strings.HasPrefix(parsed.Path, "/drama_credit_recent_test_") {
		t.Skip("requires this query's dedicated test database")
	}
	ctx := context.Background()
	store, err := pgfixture.Open(t, ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	password := "isolated-credit-preview-password"
	hash, err := (passwordhash.Bcrypt{}).Hash([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	auth := &accounts.Service{Store: store, Hasher: passwordhash.Bcrypt{}}
	ids, tokens := []string{identity.New("recent_a"), identity.New("recent_b"), identity.New("recent_empty")}, []string{}
	for _, id := range ids {
		if _, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,$1,'user',$3)`, id, id+"@example.invalid", hash); err != nil {
			t.Fatal(err)
		}
		login, err := auth.Login(ctx, id+"@example.invalid", password, "", "workspace")
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, login.Token)
	}
	var entries []d.Entry
	for i := 0; i < 8; i++ {
		entry, err := store.Add(ctx, d.Change{AccountID: ids[0], Amount: int64(i + 1), Category: d.Gift, OperationID: identity.New("recent_op"), Source: "fixture", Subject: ids[0], Reason: fmt.Sprintf("record %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	foreign, err := store.Add(ctx, d.Change{AccountID: ids[1], Amount: 99, Category: d.Recharge, OperationID: identity.New("foreign_recent"), Source: "fixture", Subject: ids[1], Reason: "other account"})
	if err != nil {
		t.Fatal(err)
	}
	host, handler := httpapi.New(&projectapp.Service{}, nil, t.TempDir(), 0)
	host.Authenticator = auth
	host.Credits = &creditsapp.Service{Queries: store}
	defer host.Shutdown(ctx)
	request := func(token, query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8787/api/credits/ledger/recent"+query, nil)
		if token != "" {
			addSessionCookie(r, token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for i, token := range tokens {
		response := request(token, "")
		var fields map[string]json.RawMessage
		var out struct {
			Items []d.Entry `json:"items"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &fields) != nil || len(fields) != 1 || json.Unmarshal(response.Body.Bytes(), &out) != nil || out.Items == nil {
			t.Fatal("recent response must contain only a real array", response.Code, response.Body.String())
		}
		want := 5
		if i == 1 {
			want = 1
		}
		if i == 2 {
			want = 0
		}
		if len(out.Items) != want {
			t.Fatal("query is not bounded to this account's newest five", i, len(out.Items))
		}
		for index, entry := range out.Items {
			if entry.AccountID != ids[i] {
				t.Fatal("foreign ledger receipt exposed")
			}
			if i == 0 && (entry.ID != entries[7-index].ID || entry.Amount != entries[7-index].Amount) {
				t.Fatal("newest ordering or immutable amount changed")
			}
		}
		if i == 1 && out.Items[0].ID != foreign.ID {
			t.Fatal("other account lost its own record")
		}
	}
	if response := request("", ""); response.Code != 401 {
		t.Fatal("anonymous recent records allowed", response.Code)
	}
	for _, query := range []string{"?userId=" + ids[1], "?pageSize=50"} {
		if response := request(tokens[0], query); response.Code != 400 {
			t.Fatal("recent query accepted a target or pagination", response.Code)
		}
	}
}
