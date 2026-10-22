package medialibrary

import (
	"context"
	"errors"
	"testing"

	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
	td "drama-agent/internal/domain/task"
	ud "drama-agent/internal/domain/upload"
)

type mediaFileStore struct {
	Store
	item Item
	task td.Task
}

func (s *mediaFileStore) GeneratedMedia(_ context.Context, _ ports.SpaceScope, task, output string) (Item, error) {
	if task != s.item.TaskID || output != s.item.OutputID {
		return Item{}, fault.ErrNotFound
	}
	return s.item, nil
}

type outputTasks struct {
	ports.TaskStore
	store *mediaFileStore
}

func (s outputTasks) Task(_ context.Context, id string) (td.Task, error) {
	if id != s.store.task.ID {
		return td.Task{}, fault.ErrNotFound
	}
	return s.store.task, nil
}

type outputReader struct {
	ports.TaskResultStore
	reads int
}

func (r *outputReader) ReadTaskOutput(_ context.Context, task string, out td.Output) ([]byte, error) {
	r.reads++
	return []byte(task + ":" + out.ID), nil
}

type assetAccess struct {
	allowed      bool
	projectReads int
}

func (a *assetAccess) Access(_ context.Context, actor ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	scope := ports.SpaceScope{SpaceID: r.SpaceID, UserID: actor.UserID}
	if r.Kind == spaces.ProjectResource {
		a.projectReads++
		return spaces.None, scope, nil
	}
	if a.allowed && r.OwnerID == actor.UserID {
		return spaces.Edit, scope, nil
	}
	return spaces.None, scope, nil
}

type imageThumbnailReader struct {
	calls     int
	space, id string
}

func (r *imageThumbnailReader) ContainerFile(context.Context, string, string) (ud.File, error) {
	return ud.File{}, nil
}
func (r *imageThumbnailReader) ReadContainerFile(_ context.Context, space, id string, _ bool) (ud.File, []byte, error) {
	r.calls++
	r.space, r.id = space, id
	return ud.File{}, []byte("thumbnail"), nil
}
func TestMediaFileReadsOwnVisibleOutputWithoutSourceProjectPermission(t *testing.T) {
	ctx := context.Background()
	store := &mediaFileStore{item: Item{SpaceID: "team", OwnerID: "member", CreatorID: "original", ProjectID: "project-other", TaskID: "task", OutputID: "output", Kind: "image", FileID: "file-id"}, task: td.Task{SpaceID: "team", OwnerID: "member", CreatorID: "original", ID: "task", ScopeID: "project:project-other", Status: td.Succeeded, State: td.State{Result: &td.Result{Files: []td.Output{{ID: "output", ContentType: "image/webp"}}}}}}
	access := &assetAccess{allowed: true}
	results := &outputReader{}
	thumbnails := &imageThumbnailReader{}
	service := (&Service{Store: store, Access: access, Files: thumbnails, Tasks: &taskapp.Service{Store: outputTasks{store: store}, SpaceAccess: access, Results: results}}).ForScope(ports.SpaceScope{SpaceID: "team", UserID: "member"})
	raw, view, err := service.File(ctx, "task", "output")
	if err != nil || string(raw) != "task:output" || view.ID != "output" || results.reads != 1 || access.projectReads != 0 {
		t.Fatal(view, string(raw), err)
	}
	raw, err = service.Thumbnail(ctx, "task", "output")
	if err != nil || string(raw) != "thumbnail" || thumbnails.space != "team" || thumbnails.id != "file-id" {
		t.Fatal(string(raw), err)
	}
	// A projection cannot substitute a different task container or output.
	store.task.ScopeID = "project:foreign"
	if _, _, err = service.File(ctx, "task", "output"); !errors.Is(err, fault.ErrNotFound) || results.reads != 1 {
		t.Fatal("unrelated task output leaked", err)
	}
	store.task.ScopeID = "project:project-other"
	access.allowed = false
	if _, _, err = service.File(ctx, "task", "output"); !errors.Is(err, fault.ErrNotFound) || results.reads != 1 {
		t.Fatal("revoked asset content leaked", err)
	}
	if _, err = service.Thumbnail(ctx, "task", "output"); !errors.Is(err, fault.ErrNotFound) || thumbnails.calls != 1 {
		t.Fatal("revoked asset thumbnail leaked", err)
	}
}
