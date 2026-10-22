package project

import (
	"context"
	"errors"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
)

func TestProjectAccessInfoDoesNotGiveEditGranteeSharingRights(t *testing.T) {
	ctx := context.Background()
	store := scopeProjects{memory: newMemory()}
	p := d.Project{ID: "project", SpaceID: "team", OwnerID: "assignee", CreatorID: "creator", Revision: 1}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		kind     spaces.Kind
		role     spaces.Role
		user     string
		canShare bool
	}{
		{spaces.TeamKind, spaces.Member, "editor", false},
		{spaces.TeamKind, spaces.Member, "assignee", true},
		{spaces.TeamKind, spaces.Admin, "admin", true},
		{spaces.TeamKind, spaces.Owner, "owner", true},
		{spaces.Personal, spaces.Owner, "assignee", false},
	} {
		identity := &teamCreditIdentity{scope: ports.SpaceScope{Kind: row.kind, Role: row.role, Status: spaces.Active}}
		service := (&Service{Store: store, Access: identity}).ForScope(ports.SpaceScope{SpaceID: "another-selected", UserID: row.user})
		info, err := service.AccessInfo(ctx, p.ID)
		if err != nil || info.Access != spaces.Edit || info.CanShare != row.canShare || info.CreatorID != "creator" || info.OwnerID != "assignee" || info.SpaceID != "team" {
			t.Fatal(row, info, err)
		}
	}
}

type visibleProjectsFixture struct {
	ports.ProjectStore
	gotScope  ports.SpaceScope
	gotFilter ports.ListFilter
}

func (s *visibleProjectsFixture) ListVisibleSummaries(_ context.Context, scope ports.SpaceScope, filter ports.ListFilter) ([]ports.ProjectSummary, error) {
	s.gotScope, s.gotFilter = scope, filter
	return []ports.ProjectSummary{{ID: "visible", ListMetadata: ports.ListMetadata{CreatorID: "creator", CreatorName: "name", SharedWithMe: true, EffectiveAccess: spaces.Read}}}, nil
}
func TestProjectListUsesOnlyResolvedVisibleProjectionAndPreservesMetadata(t *testing.T) {
	identity := &teamCreditIdentity{scope: ports.SpaceScope{Kind: spaces.TeamKind, Role: spaces.Member, Policy: spaces.Isolated, Status: spaces.Active}}
	store := &visibleProjectsFixture{}
	service := (&Service{Store: store, Resolver: identity}).ForScope(ports.SpaceScope{SpaceID: "team", UserID: "reader"})
	items, err := service.ListProjectsFiltered(context.Background(), ports.ListShared)
	if err != nil || len(items) != 1 || items[0].CreatorName != "name" || !items[0].SharedWithMe || items[0].EffectiveAccess != spaces.Read || store.gotScope.Policy != spaces.Isolated || store.gotScope.UserID != "reader" || store.gotFilter != ports.ListShared {
		t.Fatal(items, store.gotScope, store.gotFilter, err)
	}
	if _, err = service.Store.ListSummaries(context.Background()); err != nil || store.gotFilter != ports.ListAll {
		t.Fatal("scoped store retained another list visibility path", store.gotFilter, err)
	}
	_, err = service.ListProjectsFiltered(context.Background(), ports.ListFilter("invalid"))
	if err != fault.ErrInvalid {
		t.Fatal("unknown filter accepted", err)
	}
}

// 权限投影复用实际领域规则：只读限制内容，不能同时移除已确认的共享管理权。
type readOnlyProjectAccess struct{ scope ports.SpaceScope }

func (a readOnlyProjectAccess) Access(_ context.Context, actor ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	scope := a.scope
	scope.UserID = actor.UserID
	return spaces.Decide(scope, r, nil), scope, nil
}
func TestProjectAccessInfoReadOnlyTeamStillAllowsSharingManagers(t *testing.T) {
	ctx := context.Background()
	store := scopeProjects{memory: newMemory()}
	p := d.Project{ID: "read-only-project", SpaceID: "expired-team", OwnerID: "assignee", CreatorID: "creator", Revision: 1}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		role     spaces.Role
		user     string
		canShare bool
	}{
		{spaces.Member, "assignee", true},
		{spaces.Admin, "admin", true},
		{spaces.Owner, "owner", true},
		{spaces.Member, "reader", false},
	} {
		t.Run(string(row.role)+":"+row.user, func(t *testing.T) {
			access := readOnlyProjectAccess{scope: ports.SpaceScope{SpaceID: p.SpaceID, Kind: spaces.TeamKind, Role: row.role, Status: spaces.Expired, Policy: spaces.ReadAll, ReadOnly: true}}
			service := (&Service{Store: store, Access: access}).ForScope(ports.SpaceScope{SpaceID: p.SpaceID, UserID: row.user})
			info, err := service.AccessInfo(ctx, p.ID)
			if err != nil || info.Access != spaces.Read || info.CanShare != row.canShare {
				t.Fatal("read-only team lost management permission or gained content edit", row, info, err)
			}
			expectCode(t, service.requireProject(ctx, p, spaces.Edit), "TEAM_READ_ONLY")
		})
	}
	access := readOnlyProjectAccess{scope: ports.SpaceScope{SpaceID: p.SpaceID, Kind: spaces.TeamKind, Role: spaces.Owner, Status: spaces.Disabled, Policy: spaces.ReadAll, ReadOnly: true}}
	service := (&Service{Store: store, Access: access}).ForScope(ports.SpaceScope{SpaceID: p.SpaceID, UserID: "owner"})
	info, err := service.AccessInfo(ctx, p.ID)
	if !errors.Is(err, fault.ErrNotFound) || info.CanShare || info.SpaceID != "" {
		t.Fatal("disabled team exposed resource or management", info, err)
	}
}
