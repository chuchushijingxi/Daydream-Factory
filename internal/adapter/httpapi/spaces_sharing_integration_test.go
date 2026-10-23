package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	spaceapp "drama-agent/internal/application/spaces"
	uploadapp "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/identity"
	project "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
	"drama-agent/internal/testsupport/redisruntime"
	"drama-agent/internal/testsupport/uploadfiles"
	"github.com/jackc/pgx/v5"
)

func TestSpaceSharingAndReferencedFilesHTTP(t *testing.T) {
	ctx := context.Background()
	store := openProjectContentPG(t)
	runtime := redisruntime.Connect(t)
	store.UseRuntime(runtime)
	manager, creator, recipient, outsider, operator := identity.New("sharing_manager"), identity.New("sharing_creator"), identity.New("sharing_recipient"), identity.New("sharing_outside"), identity.New("sharing_operator")
	for _, user := range []string{manager, creator, recipient, outsider} {
		seedPostgresFixtureUser(t, user, ports.RoleUser)
	}
	seedPostgresFixtureUser(t, operator, ports.RoleOperator)
	objects := uploadfiles.New().Objects
	files := &uploadapp.Service{Store: store, Objects: objects, Resolver: store, Access: store}
	host, handler := httpapi.New(&projectapp.Service{Store: store, Resolver: store, Access: store, Files: files}, nil, t.TempDir(), 0)
	host.Authenticator = &accounts.Service{Store: store}
	host.Spaces = &spaceapp.Service{Store: store, Resolver: store, Limiter: runtime, Credits: store, CreditQueries: store, AllocationQueries: store, CreditUpdates: store}
	host.Files, host.Runtime = files, runtime
	host.IPRequestsPerMinute, host.RequestsPerMinute = 10000, 10000
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
	decode(request("POST", "/api/operations/teams", operator, operator, spaceapp.TeamInput{OwnerID: manager, Name: "隔离共享与编辑", MemberLimit: 3, ExpiresAt: time.Now().Add(90 * 24 * time.Hour)}, 201), &team)
	base := "/api/spaces/" + team.ID
	var invite spaceapp.InviteLink
	decode(request("POST", base+"/invite-links", manager, manager, nil, 201), &invite)
	for _, user := range []string{creator, recipient} {
		request("POST", "/api/invite-links/"+invite.Token+"/accept", user, user, nil, 200)
	}
	decode(request("PATCH", base, manager, manager, spaceapp.SettingsInput{Revision: team.Revision, Name: team.Name, Policy: spaces.Isolated}, 200), &team)
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	fileService := files.ForScope(ports.SpaceScope{SpaceID: team.ID, UserID: creator})
	referenced, err := fileService.Upload(ctx, uploadapp.Input{Filename: "reference.png", Data: pixels.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := fileService.Upload(ctx, uploadapp.Input{Filename: "unreferenced.png", Data: pixels.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	p := project.Project{ID: identity.New("sharing_project"), SpaceID: team.ID, OwnerID: creator, CreatorID: creator, Revision: 1, Title: "真实引用的虚构项目", Assets: []project.Asset{{ID: "prop", Kind: "prop", Name: "道具", ImageFileID: referenced.ID}}}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	path := "/api/projects/" + p.ID
	share := base + "/shares/project/" + p.ID
	request("GET", path, recipient, recipient, nil, 404)
	request("GET", share, outsider, team.ID, nil, 404)
	request("PUT", share, creator, creator, spaceapp.ShareInput{Grantee: recipient, Permission: spaces.Read}, 200)
	request("GET", path, recipient, recipient, nil, 200)
	var access projectapp.AccessView
	decode(request("GET", path+"/permissions", recipient, recipient, nil, 200), &access)
	if access.Access != spaces.Read || access.CanShare {
		t.Fatal("readonly grantee permissions wrong", access)
	}
	request("GET", share, recipient, team.ID, nil, 403)
	request("GET", "/api/files/"+referenced.ID+"/original", recipient, team.ID, nil, 404)
	content := request("GET", path+"/files/"+referenced.ID+"/original", recipient, recipient, nil, 200)
	if !bytes.Equal(content.Body.Bytes(), pixels.Bytes()) {
		t.Fatal("container reference bytes changed")
	}
	request("GET", path+"/files/"+hidden.ID+"/original", recipient, team.ID, nil, 404)
	var list []httpapi.ProjectSummaryView
	decode(request("GET", "/api/projects?filter=shared", recipient, team.ID, nil, 200), &list)
	if len(list) != 1 || list[0].ID != p.ID || !list[0].SharedWithMe || list[0].CreatorID != creator || list[0].OwnerID != creator || list[0].SpaceID != team.ID || list[0].EffectiveAccess != spaces.Read {
		t.Fatalf("shared list metadata wrong: %#v", list)
	}
	decode(request("GET", "/api/projects?filter=mine", recipient, team.ID, nil, 200), &list)
	if len(list) != 0 {
		t.Fatal("shared project became creator's own", list)
	}
	var uploadList uploadapp.Listing
	decode(request("GET", "/api/uploads", recipient, team.ID, nil, 200), &uploadList)
	if len(uploadList.Items) != 0 {
		t.Fatal("container reference expanded global selector", uploadList)
	}
	request("PUT", share, creator, creator, spaceapp.ShareInput{Grantee: recipient, Permission: spaces.Edit}, 200)
	decode(request("GET", path+"/permissions", recipient, team.ID, nil, 200), &access)
	if access.Access != spaces.Edit || access.CanShare {
		t.Fatal("edit grant gave sharing management", access)
	}
	request("PUT", share, recipient, team.ID, spaceapp.ShareInput{Grantee: manager, Permission: spaces.Read}, 403)
	request("DELETE", share, creator, creator, map[string]string{"grantee": recipient}, 200)
	request("GET", path, recipient, team.ID, nil, 404)
	request("GET", path+"/files/"+referenced.ID+"/original", recipient, team.ID, nil, 404)
	request("POST", path+"/delete", recipient, team.ID, map[string]uint64{"revision": 1}, 404)
	request("PUT", share, creator, creator, spaceapp.ShareInput{Grantee: recipient, Permission: spaces.Edit}, 200)

	// N6：只改本fixture团队的到期事实；不替换全局时间，真实余额0使内容只读。
	balance, err := store.Balance(ctx, team.ID)
	if err != nil || balance != 0 {
		t.Fatal("N6 requires the fixture's real zero balance", balance, err)
	}
	// openProjectContentPG和seedPostgresFixtureUser已核实显式隔离loopback库。
	conn, err := pgx.Connect(ctx, os.Getenv("DRAMA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	originalExpiry := team.ExpiresAt
	restoreExpiry := true
	defer func() {
		if restoreExpiry {
			if _, err := conn.Exec(ctx, `UPDATE drama_catalog_v1.teams SET expires_at=$1 WHERE space_id=$2 AND owner_id=$3`, originalExpiry, team.ID, manager); err != nil {
				t.Errorf("restore fixture expiry: %v", err)
			}
		}
	}()
	updated, err := conn.Exec(ctx, `UPDATE drama_catalog_v1.teams SET expires_at=$1 WHERE space_id=$2 AND owner_id=$3`, time.Now().Add(-time.Hour), team.ID, manager)
	if err != nil || updated.RowsAffected() != 1 {
		t.Fatal("expire only the owned test fixture", updated, err)
	}
	for _, user := range []string{creator, manager} {
		decode(request("GET", path+"/permissions", user, user, nil, 200), &access)
		if access.Access != spaces.Read || !access.CanShare {
			t.Fatal("readonly team lost sharing management", user, access)
		}
	}
	request("PUT", share, creator, creator, spaceapp.ShareInput{Grantee: recipient, Permission: spaces.Read}, 200)
	var sharing spaceapp.SharingView
	decode(request("GET", share, creator, creator, nil, 200), &sharing)
	if !sharing.CanManage || len(sharing.Shares) != 1 || sharing.Shares[0].Grantee != recipient || sharing.Shares[0].Permission != spaces.Read {
		t.Fatal("readonly creator could not read updated sharing", sharing)
	}
	// 即便授予Edit，团队只读仍限制内容；当前选择头为个人空间不阻止团队管理。
	request("PUT", share, creator, creator, spaceapp.ShareInput{Grantee: recipient, Permission: spaces.Edit}, 200)
	decode(request("GET", path+"/permissions", recipient, recipient, nil, 200), &access)
	if access.Access != spaces.Read || access.CanShare {
		t.Fatal("readonly edit grantee gained content/sharing rights", access)
	}
	request("GET", share, recipient, recipient, nil, 403)
	request("PUT", share, recipient, recipient, spaceapp.ShareInput{Grantee: manager, Permission: spaces.Read}, 403)
	contentDenied := request("POST", path+"/delete", recipient, team.ID, map[string]uint64{"revision": 1}, 403)
	if !strings.Contains(contentDenied.Body.String(), "TEAM_READ_ONLY") {
		t.Fatal("readonly content mutation did not preserve readonly reason", contentDenied.Body.String())
	}
	request("DELETE", share, creator, creator, map[string]string{"grantee": recipient}, 200)
	decode(request("GET", share, creator, creator, nil, 200), &sharing)
	if !sharing.CanManage || len(sharing.Shares) != 0 {
		t.Fatal("readonly creator could not remove sharing", sharing)
	}
	request("GET", path, recipient, team.ID, nil, 404)
	// 恢复原夹具及最终删除路径，证明只读例外只作用于共享配置。
	request("PUT", share, creator, creator, spaceapp.ShareInput{Grantee: recipient, Permission: spaces.Edit}, 200)
	updated, err = conn.Exec(ctx, `UPDATE drama_catalog_v1.teams SET expires_at=$1 WHERE space_id=$2 AND owner_id=$3`, originalExpiry, team.ID, manager)
	if err != nil || updated.RowsAffected() != 1 {
		t.Fatal("restore fixture expiry", updated, err)
	}
	restoreExpiry = false
	decode(request("GET", path+"/permissions", recipient, team.ID, nil, 200), &access)
	if access.Access != spaces.Edit || access.CanShare {
		t.Fatal("restoring expiry changed content/shared roles", access)
	}
	request("POST", path+"/delete", recipient, team.ID, map[string]uint64{"revision": 1}, 200)
	request("GET", path, recipient, team.ID, nil, 404)
}
