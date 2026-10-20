package httpapi_test

import (
	"bytes"
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	"github.com/jackc/pgx/v5"
)

func TestPricedMockCandidatesRealPGHTTP(t *testing.T) {
	path := os.Getenv("DRAMA_TEST_CONFIG_FILE")
	if path == "" {
		t.Skip("dedicated accounts PG required")
	}
	cfg, err := appconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	address, err := url.Parse(cfg.Database.URL)
	if err != nil || !strings.HasPrefix(address.Path, "/drama_accounts_test_") {
		t.Fatal("dedicated accounts database required")
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
	before, err := store.ModelConfigSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		current, e := store.ModelConfigSnapshot(ctx)
		if e == nil {
			before.Revision = current.Revision + 1
			e = store.ModelConfigCommit(ctx, before, current.Revision)
		}
		if e != nil {
			t.Error("restore test registry", e)
		}
	}()
	cipher, err := postgres.OpenModelCipher(filepath.Join(t.TempDir(), "mock-config.key"), true)
	if err != nil {
		t.Fatal(err)
	}
	factoryCalls := 0
	configuration := &modelconfig.Service{Store: store, Projects: store, Cipher: cipher, Tasks: &taskapp.Service{}, Factory: func(context.Context, string, string, string, md.ConfiguredModel) (ports.Model, error) {
		factoryCalls++
		return interfaceOnlyModel{}, nil
	}}
	preset, _ := md.OfficialPreset("mock")
	on := true
	input := modelconfig.ModelConfigInput{Revision: before.Revision, Connection: modelconfig.ConnectionInput{ID: preset.ID, Name: preset.Name, Kind: preset.Kind, Protocol: preset.Protocol}}
	for _, m := range preset.Models {
		input.Connection.Models = append(input.Connection.Models, modelconfig.ModelEnableInput{ID: m.ID, ModelID: m.ModelID, Description: "本地媒体测试", Category: m.Category, Enabled: &on, MockBehavior: &md.MockBehavior{}, CreditPrice: &credits.PriceRule{UnitPrice: 5}})
	}
	view, err := configuration.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	accountsService := &accounts.Service{Store: store, Hasher: passwordhash.Bcrypt{}}
	email := identity.New("candidate") + "@example.test"
	codeID := accounts.RandomToken()
	if err = store.PutVerification(ctx, codeID, email, "register", accounts.Digest(codeID+":316842"), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = store.ActivateVerification(ctx, codeID, true); err != nil {
		t.Fatal(err)
	}
	_, authorization, err := accountsService.CreateCode(ctx, ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = accountsService.Register(ctx, email, "候选测试用户", "candidate-fixture-password", "316842", authorization); err != nil {
		t.Fatal(err)
	}
	host, handler := httpapi.New(&app.Service{Store: store, Models: configuration}, configuration, t.TempDir(), 0)
	host.Accounts = accountsService
	host.Authenticator = accountsService
	defer host.Shutdown(ctx)
	request := func(method, path string, cookie *http.Cookie, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			addSessionCookie(r, cookie.Value)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	login := request("POST", "/api/auth/login", nil, map[string]string{"email": email, "password": "candidate-fixture-password", "audience": "workspace"})
	if login.Code != 200 || len(login.Result().Cookies()) != 1 {
		t.Fatal("fixture login failed", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	conn, err := pgx.Connect(ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	opID, token := identity.New("description_operator"), accounts.RandomToken()
	if _, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,enabled,password_hash) VALUES($1,$2,'隔离模型运营','operator',true,$3)`, opID, opID+"@example.invalid", []byte("unusable-fixture-hash")); err != nil {
		t.Fatal(err)
	}
	redisruntime.SeedDigest(t, opID, accounts.Operations, accounts.Digest("operations:"+token), ports.RoleOperator, time.Now().Add(time.Hour))
	opCookie := &http.Cookie{Name: "drama_operations_session", Value: opID + ".operations." + token}
	input.Revision = view.Revision
	input.Connection.Models[0].Description = "更新后的图片说明🌙"
	input.Connection.Models[1].Description = "本地视频测试"
	updated := request("POST", "/api/model-config", opCookie, input)
	if updated.Code != 200 || json.Unmarshal(updated.Body.Bytes(), &view) != nil {
		t.Fatal("description HTTP save", updated.Code, updated.Body.String())
	}
	if stale := request("POST", "/api/model-config", opCookie, input); stale.Code != 409 {
		t.Fatal("description edit bypassed CAS", stale.Code)
	}
	if w := request("GET", "/api/models", nil, nil); w.Code != 401 {
		t.Fatal("anonymous candidate access", w.Code)
	}
	if w := request("GET", "/api/model-config", cookie, nil); w.Code != 403 {
		t.Fatal("ordinary configuration access", w.Code)
	}
	w := request("GET", "/api/models", cookie, nil)
	var options ports.ModelOptions
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &options) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	var image, video *ports.ModelOption
	for i := range options.Image {
		if options.Image[i].ID == md.MockImageModelID {
			image = &options.Image[i]
		}
	}
	for i := range options.Video {
		if options.Video[i].ID == md.MockVideoModelID {
			video = &options.Video[i]
		}
	}
	if image == nil || video == nil || image.CreditPrice.UnitPrice != 5 || video.CreditPrice.UnitPrice != 5 || image.BillingUnit != "image" || video.BillingUnit != "second" || image.Free || video.Free {
		t.Fatal("priced mock missing or changed", options)
	}
	if image.Description != "更新后的图片说明🌙" || video.Description != "本地视频测试" {
		t.Fatal("description missing from ordinary candidates", options)
	}
	for _, forbidden := range []string{"credential", "secret", "apiKey", "baseUrl"} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatal("unsafe candidate field", forbidden)
		}
	}
	persisted, err := store.ModelConfigSnapshot(ctx)
	if err != nil || persisted.Revision != view.Revision || factoryCalls != 0 {
		t.Fatal("candidate read mutated configuration or created execution", err)
	}
	for _, conn := range persisted.Connections {
		if conn.ID == "mock" {
			for _, m := range conn.Models {
				if !reflect.DeepEqual(m.CreditPrice, &credits.PriceRule{UnitPrice: 5}) {
					t.Fatal("PG price changed")
				}
				want := "更新后的图片说明🌙"
				if m.Category == "video" {
					want = "本地视频测试"
				}
				if m.Description != want {
					t.Fatal("PG description lost", m.Description)
				}
			}
		}
	}
	input.Revision = view.Revision
	for i := range input.Connection.Models {
		input.Connection.Models[i].Description = ""
	}
	cleared := request("POST", "/api/model-config", opCookie, input)
	if cleared.Code != 200 {
		t.Fatal("clear description", cleared.Code, cleared.Body.String())
	}
	w = request("GET", "/api/models", cookie, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &options) != nil {
		t.Fatal("clear candidate read")
	}
	for _, m := range append(options.Image, options.Video...) {
		if m.Description != "" {
			t.Fatal("empty description replaced", m)
		}
	}
	t.Log("real PG configured prices match ordinary-session HTTP candidates; anonymous 401, management 403; no model construction or generation")
}
