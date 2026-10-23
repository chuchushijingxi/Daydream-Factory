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

type scopeProjects struct {
	*memory
	space string
}

func (s scopeProjects) ForProjectSpace(space string) ports.ProjectStore { s.space = space; return s }
func (s scopeProjects) ForProjectScope(scope ports.SpaceScope) ports.ProjectStore {
	return s.ForProjectSpace(scope.SpaceID)
}
func (s scopeProjects) Snapshot(ctx context.Context, id string) (d.Project, error) {
	p, err := s.memory.Snapshot(ctx, id)
	if err == nil && s.space != "" && p.SpaceID != s.space {
		return d.Project{}, fault.ErrNotFound
	}
	return p, err
}
func (s scopeProjects) Commit(ctx context.Context, p d.Project, rev uint64) error {
	if p.SpaceID != s.space {
		return fault.ErrNotFound
	}
	return s.memory.Commit(ctx, p, rev)
}

type projectAccess struct {
	permission spaces.Access
	calls      int
	resource   spaces.Resource
}

func (a *projectAccess) Access(_ context.Context, actor ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	a.calls++
	a.resource = r
	return a.permission, ports.SpaceScope{SpaceID: r.SpaceID, UserID: actor.UserID, Kind: spaces.TeamKind, Role: spaces.Member, Policy: spaces.ReadAll}, nil
}
func (a *projectAccess) Resolve(_ context.Context, actor ports.Actor, id string) (ports.SpaceScope, error) {
	if id == "" {
		id = actor.UserID
	}
	return ports.SpaceScope{SpaceID: id, UserID: actor.UserID, Kind: spaces.TeamKind, Role: spaces.Member}, nil
}
func TestProjectResourceSpaceReadAndStaleSpaceWrite(t *testing.T) {
	ctx := context.Background()
	store := scopeProjects{memory: newMemory()}
	p := d.Project{ID: "project-spaces", SpaceID: "team-actual", OwnerID: "creator", CreatorID: "creator", Revision: 1, Title: "private"}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	access := &projectAccess{permission: spaces.Edit}
	service := (&Service{Store: store, Resolver: access, Access: access}).ForScope(ports.SpaceScope{SpaceID: "team-selected", UserID: "editor"})
	read, err := service.ReadProject(ctx, p.ID)
	if err != nil || read.SpaceID != p.SpaceID || access.resource.SpaceID != p.SpaceID {
		t.Fatal(read, access.resource, err)
	}
	_, err = service.DeleteProject(ctx, p.ID, p.Revision)
	var problem *fault.Error
	if !errors.As(err, &problem) || problem.Code != "SPACE_CHANGED" {
		t.Fatal("stale tab wrote another space", err)
	}
	current, _ := store.Snapshot(ctx, p.ID)
	if current.Revision != p.Revision || current.DeletedAt != nil {
		t.Fatal("refusal changed resource", current)
	}
	access.permission = spaces.None
	denied, err := service.ReadProject(ctx, p.ID)
	if !errors.Is(err, fault.ErrNotFound) || denied.ID != "" || denied.Title != "" {
		t.Fatal("forbidden resource leaked", denied, err)
	}
}
func TestProjectCommitRechecksPermissionAndKeepsCreator(t *testing.T) {
	ctx := context.Background()
	store := scopeProjects{memory: newMemory()}
	p := d.Project{ID: "project-recheck", SpaceID: "team", OwnerID: "assignee", CreatorID: "original", Revision: 1}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	access := &projectAccess{permission: spaces.Edit}
	service := (&Service{Store: store, Resolver: access, Access: access}).ForScope(ports.SpaceScope{SpaceID: p.SpaceID, UserID: "editor"})
	read, err := service.Project(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	access.permission = spaces.Read
	_, err = service.commit(ctx, read, read.Revision)
	var problem *fault.Error
	if !errors.As(err, &problem) || problem.Code != "ROLE_FORBIDDEN" {
		t.Fatal("revoked edit committed", err)
	}
	access.permission = spaces.Edit
	saved, err := service.DeleteProject(ctx, p.ID, p.Revision)
	if err != nil || saved.CreatorID != "original" || saved.OwnerID != "assignee" {
		t.Fatal(saved, err)
	}
}

type noProjectExecution struct {
	ports.ProjectStore
	calls *int
}

func (s noProjectExecution) Acquire(context.Context, string) (ports.Lease, bool, error) {
	*s.calls++
	return ports.Lease{}, false, nil
}
func (s noProjectExecution) ForProjectSpace(string) ports.ProjectStore {
	panic("execution gate bypassed by space derivation")
}
func TestWorkerAcquiresBeforeSpaceDerivation(t *testing.T) {
	ctx := context.Background()
	store := newMemory()
	p := d.Project{ID: "project-worker", SpaceID: "team", OwnerID: "owner", CreatorID: "owner", Revision: 1}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	calls := 0
	service := &Service{Store: noProjectExecution{ProjectStore: store, calls: &calls}}
	if err := service.Execute(ctx, p.ID); !errors.Is(err, errExecutionBusy) || calls != 1 {
		t.Fatal("worker did not retain original execution gate", err, calls)
	}
}

func TestProjectScopeWithoutStoreReturnsConfigurationError(t *testing.T) {
	ctx := context.Background()
	service := (&Service{}).ForScope(ports.SpaceScope{SpaceID: "user", UserID: "user"})
	if _, err := service.Project(ctx, "project"); err == nil {
		t.Fatal("missing store reported a project")
	}
	if _, err := service.ListProjects(ctx); err == nil {
		t.Fatal("missing store reported a list")
	}
	if err := service.Store.Create(ctx, d.Project{}); err == nil {
		t.Fatal("missing store accepted create")
	}
}
