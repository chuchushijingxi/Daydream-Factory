package project

import (
	"context"
	"errors"
	"testing"

	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
	td "drama-agent/internal/domain/task"
	u "drama-agent/internal/domain/upload"
)

func TestSharedProjectUnadoptedImageThumbnailUsesActualTaskOutputReference(t *testing.T) {
	ctx := context.Background()
	store := scopeProjects{memory: newMemory()}
	p := d.Project{ID: "shared-project", SpaceID: "team", OwnerID: "another-member", CreatorID: "creator", Revision: 1}
	other := d.Project{ID: "other-project", SpaceID: "team", OwnerID: "another-member", CreatorID: "creator", Revision: 1}
	for _, project := range []d.Project{p, other} {
		if err := store.Create(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	access := &projectAccess{permission: spaces.Read}
	task := td.Task{ID: "image-task", SpaceID: "team", ScopeID: mediaScope(p.ID), OwnerID: "another-member", CreatorID: "creator", Status: td.Succeeded, State: td.State{Operation: "image.generate", Result: &td.Result{Files: []td.Output{{ID: "output", FileID: "unadopted-image", ContentType: "image/png"}}}}}
	taskStore := &previewTaskStore{rows: []td.Task{task}, inputs: map[string]td.Snapshot{task.ID: {Input: []byte(`{}`), CallerContext: mustJSON(MediaContext{Request: MediaCommand{Target: MediaTarget{AssetID: "original-target"}}})}}}
	published := u.File{ID: "unadopted-image", SpaceID: "team", OwnerID: "another-member", Original: u.Blob{ContentType: "image/png"}, Thumbnail: &u.Thumbnail{Width: 1, Height: 1}}
	files := &containerFileFixture{files: map[string]u.File{published.ID: published}}
	service := (&Service{Store: store, Access: access, Files: files, MediaTasks: &taskapp.Service{Store: taskStore, SpaceAccess: access}}).ForScope(ports.SpaceScope{SpaceID: "personal-selected", UserID: "shared-reader"})
	if _, err := service.Files.File(ctx, published.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("isolated upload selector must remain private", err)
	}
	view, err := service.Media(ctx, p.ID, task.ID)
	if err != nil || view.AdoptedOutputID != "" || len(p.Assets) != 0 {
		t.Fatal("fixture must represent an unadopted historical image", view, err)
	}
	raw, err := service.MediaThumbnail(ctx, p.ID, task.ID, "output")
	if err != nil || string(raw) != "published bytes" || files.calls != 1 {
		t.Fatal("shared project history image thumbnail failed", string(raw), err, files.calls)
	}
	for _, wrong := range []struct{ project, task, output string }{
		{other.ID, task.ID, "output"},
		{p.ID, "missing-task", "output"},
		{p.ID, task.ID, "missing-output"},
		{p.ID, task.ID, published.ID}, // fileId不能伪装成outputId。
	} {
		calls := files.calls
		if _, err := service.MediaThumbnail(ctx, wrong.project, wrong.task, wrong.output); !errors.Is(err, fault.ErrNotFound) || files.calls != calls {
			t.Fatal("forged task/output read a file", wrong, err, files.calls)
		}
	}
	files.files[published.ID] = u.File{ID: published.ID, SpaceID: "other-team", Original: u.Blob{ContentType: "image/png"}, Thumbnail: published.Thumbnail}
	if _, err = service.MediaThumbnail(ctx, p.ID, task.ID, "output"); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("cross-space output file exposed", err)
	}
	files.files[published.ID] = published
	access.permission = spaces.None
	calls := files.calls
	if _, err = service.MediaThumbnail(ctx, p.ID, task.ID, "output"); !errors.Is(err, fault.ErrNotFound) || files.calls != calls {
		t.Fatal("revoked container kept thumbnail capability", err)
	}
}

type videoThumbnailFixture struct {
	ports.TaskResultStore
	calls int
}

func (f *videoThumbnailFixture) ReadTaskThumbnail(_ context.Context, task string, output td.Output) ([]byte, error) {
	f.calls++
	return []byte("video cover"), nil
}
func TestProjectVideoThumbnailStillUsesTaskThumbnail(t *testing.T) {
	ctx := context.Background()
	store := newMemory()
	p := d.Project{ID: "video-project", Revision: 1}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	tasks := &previewTaskStore{rows: []td.Task{{ID: "video-task", ScopeID: mediaScope(p.ID), Status: td.Succeeded, State: td.State{Operation: "video.generate", Result: &td.Result{Files: []td.Output{{ID: "video-output", ContentType: "video/mp4"}}}}}}, inputs: map[string]td.Snapshot{"video-task": {Input: []byte(`{}`), CallerContext: mustJSON(MediaContext{})}}}
	results := &videoThumbnailFixture{}
	service := &Service{Store: store, MediaTasks: &taskapp.Service{Store: tasks, Results: results}}
	raw, err := service.MediaThumbnail(ctx, p.ID, "video-task", "video-output")
	if err != nil || string(raw) != "video cover" || results.calls != 1 {
		t.Fatal("video thumbnail changed owners", string(raw), err, results.calls)
	}
}
