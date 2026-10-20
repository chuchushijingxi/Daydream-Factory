package httpapi

import (
	"bytes"
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	app "drama-agent/internal/application/platform"
	"drama-agent/internal/application/ports"
	project "drama-agent/internal/application/project"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/logoimage"
	d "drama-agent/internal/domain/platform"

	"github.com/jackc/pgx/v5"
)

type platformUnknownStore struct {
	app.Store
	commits atomic.Int32
}

func (s *platformUnknownStore) CommitPlatformSettings(ctx context.Context, revision uint64, next d.Settings) (app.Snapshot, error) {
	s.commits.Add(1)
	v, err := s.Store.CommitPlatformSettings(ctx, revision, next)
	if err == nil {
		return app.Snapshot{}, fault.New("PLATFORM_SETTINGS_SAVE_UNKNOWN", "fixture lost commit response")
	}
	return v, err
}

func TestPlatformSettingsPGHTTPAtomicCASRestartAndPublicLogo(t *testing.T) {
	address := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if address == "" {
		t.Skip("isolated PG opt-in")
	}
	u, err := url.Parse(address)
	if err != nil || !strings.Contains(u.Path, "_test_") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("requires an isolated local test database")
	}
	ctx := context.Background()
	store, err := pgfixture.Open(t, ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := store.PlatformSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var createdPlatformIDs, createdModelIDs []string
	defer func() {
		_, e := conn.Exec(ctx, `UPDATE drama_catalog_v1.platform_settings SET revision=$1,name=$2,sidebar_description=$3,logo_id=nullif($4,'') WHERE id`, before.Revision, before.Name, before.SidebarDescription, before.LogoID)
		if e != nil {
			t.Error(e)
		}
		for _, id := range createdPlatformIDs {
			if _, e = conn.Exec(ctx, `DELETE FROM drama_catalog_v1.platform_logos WHERE id=$1`, id); e != nil {
				t.Error(e)
			}
		}
		for _, id := range createdModelIDs {
			if _, e = conn.Exec(ctx, `DELETE FROM drama_catalog_v1.model_logos WHERE id=$1`, id); e != nil {
				t.Error(e)
			}
		}
	}()
	host, handler := New(&project.Service{}, nil, t.TempDir(), 0)
	host.Platform = &app.Service{Store: store}
	auth := &countingAuthenticator{role: ports.RoleOperator}
	host.Authenticator = auth
	server := httptest.NewServer(handler)
	defer server.Close()
	type reply struct {
		status int
		body   []byte
		header http.Header
	}
	request := func(method, path, body, audience string, headers map[string]string) reply {
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if audience != "" {
			req.Header.Set("X-Drama-Audience", audience)
			req.AddCookie(&http.Cookie{Name: cookieForAudience(audience), Value: "fixture-user." + audience + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Error(e)
			return reply{}
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return reply{res.StatusCode, raw, res.Header}
	}
	expect := func(res reply, status int, code string) {
		t.Helper()
		if res.status != status || code != "" && !bytes.Contains(res.body, []byte(`"code":"`+code+`"`)) {
			t.Fatalf("HTTP %d instead of %d: %s", res.status, status, res.body)
		}
	}
	get := func() app.View {
		res := request("GET", "/api/platform-settings", "", "", nil)
		expect(res, 200, "")
		var v app.View
		var fields map[string]any
		if json.Unmarshal(res.body, &v) != nil || json.Unmarshal(res.body, &fields) != nil || len(fields) != 5 || res.header.Get("Cache-Control") != "no-store" {
			t.Fatal("public projection leaked unrelated fields or was not current")
		}
		return v
	}
	encode := func(v app.Input) string { raw, _ := json.Marshal(v); return string(raw) }
	save := func(v app.Input) reply {
		return request("POST", "/api/operations/platform-settings", encode(v), "operations", nil)
	}
	initial := get()
	if initial.Snapshot != before || auth.count.Load() != 0 {
		t.Fatal("public read required session or changed settings")
	}
	expect(request("POST", "/api/operations/platform-settings", "{}", "", nil), 401, "AUTH_REQUIRED")
	expect(request("POST", "/api/operations/platform-settings", "{}", "workspace", nil), 403, "SESSION_AUDIENCE_MISMATCH")
	auth.role = ports.RoleUser
	expect(request("POST", "/api/operations/platform-logos", "bad", "operations", nil), 403, "ROLE_FORBIDDEN")
	auth.role = ports.RoleOperator
	expect(request("POST", "/api/operations/platform-settings", "{}", "operations", map[string]string{"Origin": "https://foreign.invalid"}), 403, "CROSS_ORIGIN")

	img := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	img.Set(20, 20, color.NRGBA{R: 183, A: 255})
	var raw bytes.Buffer
	png.Encode(&raw, img)
	id, _, _ := logoimage.Normalize(raw.Bytes())
	var exists bool
	if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.platform_logos WHERE id=$1)`, id).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		createdPlatformIDs = append(createdPlatformIDs, id)
	}
	res := request("POST", "/api/operations/platform-logos", raw.String(), "operations", map[string]string{"Content-Type": "image/png"})
	expect(res, 201, "")
	var logo app.LogoView
	json.Unmarshal(res.body, &logo)
	if logo.LogoID != id || get() != initial {
		t.Fatal("upload adopted settings or changed immutable identity")
	}
	publicLogo := request("GET", logo.LogoURL, "", "", nil)
	expect(publicLogo, 200, "")
	decoded, e := png.Decode(bytes.NewReader(publicLogo.body))
	if e != nil || decoded.Bounds() != image.Rect(0, 0, 96, 96) || publicLogo.header.Get("Content-Type") != "image/png" {
		t.Fatal("public logo not normalized", e)
	}
	expect(request("GET", logo.LogoURL, "", "", map[string]string{"If-None-Match": publicLogo.header.Get("ETag")}), 304, "")
	expect(request("POST", "/api/operations/platform-logos", "<svg/>", "operations", nil), 400, "INVALID_PLATFORM_LOGO")
	expect(request("POST", "/api/operations/platform-logos", strings.Repeat("x", (2<<20)+1), "operations", nil), 400, "INVALID_PLATFORM_LOGO")

	input := app.Input{Revision: initial.Revision, Settings: d.Settings{Name: "  星河😀剧场  ", SidebarDescription: " 创作空间 ", LogoID: id}}
	expect(save(input), 200, "")
	current := get()
	if current.Revision != initial.Revision+1 || current.Name != "星河😀剧场" || current.SidebarDescription != "创作空间" || current.LogoURL != logo.LogoURL {
		t.Fatal("settings were not adopted atomically")
	}
	expect(save(app.Input{Revision: initial.Revision, Settings: d.Settings{Name: "stale overwrite", SidebarDescription: "wrong", LogoID: ""}}), 409, "PLATFORM_SETTINGS_CONFLICT")
	if get() != current {
		t.Fatal("stale save partially overwrote the configuration")
	}
	for _, body := range []string{`{}`, `{"revision":1,"name":null,"sidebarDescription":"x","logoId":""}`, `{"revision":1,"name":"x","sidebarDescription":"x","logoId":"","logoUrl":"x"}`} {
		bad := request("POST", "/api/operations/platform-settings", body, "operations", nil)
		if bad.status != 400 {
			t.Fatal("accepted missing/null/unknown fields", string(bad.body))
		}
	}
	for _, fields := range []d.Settings{{Name: strings.Repeat("😀", 49)}, {Name: "valid", SidebarDescription: strings.Repeat("字", 121)}, {Name: "control\ninside"}, {Name: "valid", LogoID: strings.Repeat("a", 64)}} {
		if save(app.Input{Revision: current.Revision, Settings: fields}).status != 400 || get() != current {
			t.Fatal("invalid input was saved or partial changes escaped")
		}
	}
	for _, revision := range []uint64{ports.MaxSafeInteger, ports.MaxSafeInteger + 1} {
		expect(save(app.Input{Revision: revision, Settings: d.Settings{Name: "unsafe revision"}}), 400, "PLATFORM_SETTINGS_INVALID")
		if get() != current {
			t.Fatal("out-of-range revision changed persisted configuration")
		}
	}

	// Public platform URLs cannot expose or adopt a model-only logo.
	img.Set(0, 0, color.NRGBA{B: 131, A: 255})
	raw.Reset()
	png.Encode(&raw, img)
	modelID, modelPNG, _ := logoimage.Normalize(raw.Bytes())
	conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.model_logos WHERE id=$1)`, modelID).Scan(&exists)
	if !exists {
		createdModelIDs = append(createdModelIDs, modelID)
	}
	if err = store.SaveModelLogo(ctx, modelID, modelPNG); err != nil {
		t.Fatal(err)
	}
	expect(request("GET", "/api/platform-logos/"+modelID, "", "", nil), 404, "NOT_FOUND")
	expect(save(app.Input{Revision: current.Revision, Settings: d.Settings{Name: "bad bind", LogoID: modelID}}), 400, "INVALID_PLATFORM_LOGO")
	if get() != current {
		t.Fatal("model logo crossed storage boundary")
	}

	var wg sync.WaitGroup
	results := make(chan reply, 2)
	for _, name := range []string{"并发甲", "并发乙"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			results <- save(app.Input{Revision: current.Revision, Settings: d.Settings{Name: name, SidebarDescription: name, LogoID: id}})
		}(name)
	}
	wg.Wait()
	close(results)
	statuses := map[int]int{}
	for result := range results {
		statuses[result.status]++
	}
	current = get()
	if statuses[200] != 1 || statuses[409] != 1 || current.Name != current.SidebarDescription || current.Revision != initial.Revision+2 {
		t.Fatal("CAS allowed two writers or mixed fields", statuses, current)
	}

	unknown := &platformUnknownStore{Store: store}
	host.Platform.Store = unknown
	expect(save(app.Input{Revision: current.Revision, Settings: d.Settings{Name: strings.Repeat("😀", 48), SidebarDescription: "", LogoID: id}}), 503, "PLATFORM_SETTINGS_SAVE_UNKNOWN")
	current = get()
	if unknown.commits.Load() != 1 || current.Revision != initial.Revision+3 || current.SidebarDescription != "" || current.Name != strings.Repeat("😀", 48) {
		t.Fatal("unknown result was retried or could not be recovered by readback")
	}
	host.Platform.Store = store
	// Restoring the default is an ordinary atomic save, not deletion of old images.
	expect(save(app.Input{Revision: current.Revision, Settings: d.Settings{Name: current.Name, SidebarDescription: "", LogoID: ""}}), 200, "")
	current = get()
	if current.LogoURL != "" || current.LogoID != "" {
		t.Fatal("default logo not restored")
	}
	expect(request("GET", logo.LogoURL, "", "", nil), 200, "")
	host.ReadOnly = true
	expect(save(app.Input{Revision: current.Revision, Settings: d.Settings{Name: "no write"}}), 403, "SERVICE_READ_ONLY")
	expect(request("POST", "/api/operations/platform-logos", raw.String(), "operations", nil), 403, "SERVICE_READ_ONLY")
	if get() != current {
		t.Fatal("read-only request changed settings")
	}
	host.ReadOnly = false
	store.Close()
	store, err = pgfixture.Open(t, ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	host.Platform = &app.Service{Store: store}
	if restored := get(); !reflect.DeepEqual(restored, current) {
		t.Fatal("restart/migration reset persisted configuration", restored, current)
	}
	t.Log("real PG/HTTP: public default/read, upload/preview/adopt, CAS, strict DTO, Unicode, isolated logos, commit-unknown readback, restore-default, read-only and restart passed")
}
