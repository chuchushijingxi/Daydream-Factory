package task_test

import (
	"context"
	"errors"
	"testing"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
	d "drama-agent/internal/domain/task"
)

type spaceTaskStore struct {
	ports.TaskStore
	task        d.Task
	space       string
	cancelCalls *int
}

func (s *spaceTaskStore) Task(context.Context, string) (d.Task, error) {
	if s.space != "" && s.space != s.task.SpaceID {
		return d.Task{}, fault.ErrNotFound
	}
	return s.task, nil
}
func (s *spaceTaskStore) ForTaskSpace(space string) ports.TaskStore {
	copy := *s
	copy.space = space
	return &copy
}
func (s *spaceTaskStore) CancelTask(context.Context, string, string, uint64) (d.Task, error) {
	*s.cancelCalls++
	return s.task, nil
}

type taskSpaceAccess struct {
	permission spaces.Access
	last       spaces.Resource
	calls      int
}

func (a *taskSpaceAccess) Access(_ context.Context, actor ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	a.calls++
	a.last = r
	return a.permission, ports.SpaceScope{SpaceID: r.SpaceID, UserID: actor.UserID, Kind: spaces.TeamKind, Role: spaces.Member}, nil
}
func TestTaskUsesContainerPermissionAndRejectsStaleSpaceWrite(t *testing.T) {
	ctx := context.Background()
	cancelCalls := 0
	store := &spaceTaskStore{task: d.Task{ID: "task", ScopeID: "project:container", SpaceID: "team", OwnerID: "submitter", CreatorID: "submitter"}, cancelCalls: &cancelCalls}
	access := &taskSpaceAccess{permission: spaces.Edit}
	service := (&app.Service{Store: store, SpaceAccess: access}).ForScope(ports.SpaceScope{SpaceID: "other-space", UserID: "reader"}).ForResource(spaces.Resource{SpaceID: "team", OwnerID: "project-owner", Kind: spaces.ProjectResource, ID: "container"})
	view, err := service.Get(ctx, store.task.ScopeID, store.task.ID)
	if err != nil || view.SpaceID != "team" || view.CreatorID != "submitter" || access.last.ID != "container" || access.last.OwnerID != "project-owner" {
		t.Fatal(view, access.last, err)
	}
	_, err = service.Cancel(ctx, store.task.ScopeID, store.task.ID, 1)
	var problem *fault.Error
	if !errors.As(err, &problem) || problem.Code != "SPACE_CHANGED" || cancelCalls != 0 {
		t.Fatal("stale task cancellation reached storage", err, cancelCalls)
	}
	access.permission = spaces.None
	view, err = service.Get(ctx, store.task.ScopeID, store.task.ID)
	if !errors.Is(err, fault.ErrNotFound) || view.ID != "" {
		t.Fatal("forbidden task leaked", view, err)
	}
	calls := access.calls
	trusted := (&app.Service{Store: store, SpaceAccess: access}).ForSpace("team")
	if _, err = trusted.Get(ctx, store.task.ScopeID, store.task.ID); err != nil || access.calls != calls {
		t.Fatal("accepted worker job depended on browser permissions", err)
	}
}

func TestTaskContainerCannotAuthorizeAnotherProjectsTask(t *testing.T) {
	ctx := context.Background()
	store := &spaceTaskStore{task: d.Task{ID: "task", SpaceID: "team", ScopeID: "project:private", OwnerID: "owner"}}
	access := &taskSpaceAccess{permission: spaces.Edit}
	service := (&app.Service{Store: store, SpaceAccess: access}).ForScope(ports.SpaceScope{SpaceID: "team", UserID: "reader"}).ForResource(spaces.Resource{SpaceID: "team", OwnerID: "owner", Kind: spaces.ProjectResource, ID: "shared"})
	if _, err := service.Get(ctx, "project:private", "task"); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("visible project authorized unrelated task", err)
	}
	if access.calls != 0 {
		t.Fatal("forged binding reached access lookup")
	}
}
