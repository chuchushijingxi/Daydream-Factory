package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	spaceapp "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/identity"
	project "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/redisruntime"
)

func TestSpacesHTTPMembershipAndOperationsBoundaries(t *testing.T) {
	store := openProjectContentPG(t)
	runtime := redisruntime.Connect(t)
	ctx := context.Background()
	owner, admin, member, outsider, operator := identity.New("team_owner"), identity.New("team_admin"), identity.New("team_member"), identity.New("team_other"), identity.New("team_operator")
	for _, id := range []string{owner, admin, member, outsider} {
		seedPostgresFixtureUser(t, id, ports.RoleUser)
	}
	seedPostgresFixtureUser(t, operator, ports.RoleOperator)
	host, handler := httpapi.New(&projectapp.Service{Store: store, Resolver: store, Access: store}, nil, t.TempDir(), 0)
	host.Authenticator = &accounts.Service{Store: store}
	host.Spaces = &spaceapp.Service{Store: store, Resolver: store, Limiter: runtime}
	defer host.Shutdown(ctx)
	requestInSpace := func(method, path, user, selected string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if body != nil {
			var err error
			raw, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, "http://127.0.0.1:8792"+path, bytes.NewReader(raw))
		if user != "" {
			audience := accounts.Workspace
			if user == operator {
				audience = accounts.Operations
			}
			addSessionCookie(req, postgresFixtureToken(user, audience))
		}
		if selected != "" {
			req.Header.Set("X-Drama-Space", selected)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, out.Code, want, out.Body.String())
		}
		return out
	}
	request := func(method, path, user string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		return requestInSpace(method, path, user, "", body, want)
	}
	decode := func(out *httptest.ResponseRecorder, v any) {
		t.Helper()
		if err := json.Unmarshal(out.Body.Bytes(), v); err != nil {
			t.Fatal(err)
		}
	}
	request("GET", "/api/spaces", "", nil, 401)
	request("POST", "/api/operations/teams", owner, map[string]any{}, 403)
	var team spaceapp.TeamView
	decode(request("POST", "/api/operations/teams", operator, spaceapp.TeamInput{OwnerID: owner, Name: "HTTP 团队边界", MemberLimit: 3, ExpiresAt: time.Now().Add(90 * 24 * time.Hour), GrantAmount: 100, ModelDiscounts: []spaceapp.ModelDiscount{}}, 201), &team)
	base := "/api/spaces/" + team.ID
	// N1：管理写入按路径团队及角色鉴权，当前空间选择不限制也不授予管理权。
	decode(requestInSpace("PATCH", base, owner, outsider, spaceapp.SettingsInput{Revision: team.Revision, Name: "按路径管理的团队", Policy: "read_all"}, 200), &team)
	if team.Name != "按路径管理的团队" {
		t.Fatal("path-authorized management was not persisted")
	}
	requestInSpace("PATCH", base, outsider, team.ID, spaceapp.SettingsInput{Revision: team.Revision, Name: "未经授权的改名", Policy: "read_all"}, 404)
	request("GET", base, outsider, nil, 404)
	request("GET", base, operator, nil, 404)
	request("GET", "/api/operations/teams/"+team.ID, operator, nil, 200)
	for _, suffix := range []string{"/members", "/invites", "/content", "/credits"} {
		request("POST", "/api/operations/teams/"+team.ID+suffix, operator, map[string]any{}, 404)
	}
	var link spaceapp.InviteLink
	decode(request("POST", base+"/invite-links", owner, nil, 201), &link)
	if len(link.Token) < 40 {
		t.Fatal("invitation token missing")
	}
	request("GET", "/api/invite-links/"+link.Token, outsider, nil, 200)
	// 加入从个人空间发起，路径团队不会被当前选择头误拦为 SPACE_CHANGED。
	request("POST", "/api/invite-links/"+link.Token+"/accept", admin, nil, 200)
	request("POST", base+"/members/"+admin+"/role", owner, map[string]string{"role": "admin"}, 200)
	request("POST", "/api/invite-links/"+link.Token+"/accept", member, nil, 200)
	request("POST", base+"/invite-links", owner, nil, 409)
	request("POST", base+"/members/"+member+"/role", admin, map[string]string{"role": "admin"}, 403)
	request("POST", base+"/ownership-transfers", admin, map[string]string{"userId": member}, 403)
	request("DELETE", base+"/members/"+owner, admin, nil, 403)
	request("POST", base+"/leave", owner, nil, 403)
	for _, path := range []string{base + "/overview", base + "/audit", base + "/invites", base + "/join-requests"} {
		request("GET", path, member, nil, 403)
	}
	var visible []map[string]json.RawMessage
	decode(request("GET", base+"/members", member, nil, 200), &visible)
	if len(visible) != 3 {
		t.Fatal("member list count", len(visible))
	}
	for _, item := range visible {
		if item["email"] != nil || item["joinedAt"] != nil {
			t.Fatal("member list leaked management fields")
		}
	}
	// 运营有团队管理元信息，不能借运营身份读取团队正文。
	p := project.Project{ID: identity.New("team_project"), SpaceID: team.ID, OwnerID: owner, CreatorID: owner, Revision: 1, Title: "PRIVATE TEAM CONTENT"}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	request("GET", "/api/projects/"+p.ID, operator, nil, 404)
	request("GET", "/api/projects/"+p.ID, outsider, nil, 404)
	request("GET", "/api/projects/"+p.ID, admin, nil, 200)
	// 降低上限不会移出现有成员；申请可排队，审批时必须重新核对容量。
	decode(request("GET", "/api/operations/teams/"+team.ID, operator, nil, 200), &team)
	request("PATCH", "/api/operations/teams/"+team.ID, operator, map[string]any{"revision": team.Revision, "memberLimit": 2}, 200)
	var join spaceapp.JoinRequest
	decode(request("POST", base+"/join-requests", outsider, map[string]string{"message": "申请加入"}, 201), &join)
	request("POST", base+"/join-requests/"+join.ID+"/approve", admin, nil, 409)
	request("POST", base+"/join-requests/"+join.ID+"/reject", admin, nil, 200)
	// 所有权必须由接收人确认，旧所有者转换为管理员。
	var transfer spaceapp.OwnershipTransfer
	decode(request("POST", base+"/ownership-transfers", owner, map[string]string{"userId": admin}, 201), &transfer)
	request("POST", base+"/ownership-transfers/"+transfer.ID+"/accept", member, nil, 404)
	// N2：重复发起也会替换；旧链接立即失效，接收人只能确认新提议。
	var replacement spaceapp.OwnershipTransfer
	decode(request("POST", base+"/ownership-transfers", owner, map[string]string{"userId": admin}, 201), &replacement)
	if replacement.ID == transfer.ID {
		t.Fatal("ownership proposal was not replaced")
	}
	request("POST", base+"/ownership-transfers/"+transfer.ID+"/accept", admin, nil, 409)
	request("POST", base+"/ownership-transfers/"+replacement.ID+"/accept", admin, nil, 200)
	request("POST", base+"/members/"+member+"/role", owner, map[string]string{"role": "admin"}, 403)
	// 停用不影响运营管理元信息，但立即拒绝空间与内容入口。
	decode(request("GET", "/api/operations/teams/"+team.ID, operator, nil, 200), &team)
	request("PATCH", "/api/operations/teams/"+team.ID, operator, map[string]any{"revision": team.Revision, "disabled": true}, 200)
	request("GET", base, admin, nil, 404)
	request("GET", "/api/projects/"+p.ID, admin, nil, 404)
	decode(request("GET", "/api/operations/teams/"+team.ID, operator, nil, 200), &team)
	request("PATCH", "/api/operations/teams/"+team.ID, operator, map[string]any{"revision": team.Revision, "disabled": false}, 200)
	request("GET", base, admin, nil, 200)
	audit := request("GET", base+"/audit", admin, nil, 200)
	if !strings.Contains(audit.Body.String(), "ownership_transferred") || !strings.Contains(audit.Body.String(), "operator") {
		t.Fatal("management audit missing", audit.Body.String())
	}
}
