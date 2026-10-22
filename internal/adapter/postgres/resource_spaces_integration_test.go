package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	project "drama-agent/internal/domain/project"
	task "drama-agent/internal/domain/task"
	upload "drama-agent/internal/domain/upload"
)

func TestResourceSpaceColumnsAndCASPreserveOwnership(t *testing.T) {
	store, _, _ := runtimeFixture(t)
	ctx := context.Background()
	space, other := identity.New("team"), identity.New("team")
	owner, creator := identity.New("owner"), identity.New("creator")
	for _, id := range []string{space, other} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.spaces(id,kind) VALUES($1,'team')`, id); err != nil {
			t.Fatal(err)
		}
		defer store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.spaces WHERE id=$1`, id)
	}
	p := project.Project{ID: identity.New("project"), SpaceID: space, OwnerID: owner, CreatorID: creator, Revision: 1, Title: "空间内项目", Original: []byte("完整原件")}
	if err := store.ForProjectSpace(space).Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.projects WHERE id=$1`, p.ID)
	for _, reader := range []ports.ProjectStore{store.ForProjectSpace(other), store.ForProjectSpace(owner)} {
		if _, err := reader.Snapshot(ctx, p.ID); !errors.Is(err, fault.ErrNotFound) {
			t.Fatal("foreign space read", err)
		}
	}
	for _, field := range []string{"space", "owner", "creator"} {
		changed := project.Clone(p)
		changed.Revision++
		switch field {
		case "space":
			changed.SpaceID = other
		case "owner":
			changed.OwnerID = creator
		case "creator":
			changed.CreatorID = owner
		}
		if err := store.Commit(ctx, changed, p.Revision); !errors.Is(err, fault.ErrConflict) {
			t.Fatal("CAS accepted forged "+field, err)
		}
	}
	p.Title = "保存后的标题"
	p.Revision++
	if err := store.ForProjectSpace(space).Commit(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	got, err := store.Snapshot(ctx, p.ID)
	if err != nil || got.SpaceID != space || got.OwnerID != owner || got.CreatorID != creator || string(got.Original) != "完整原件" || got.Title != p.Title {
		t.Fatal(got, err)
	}
	rows, err := store.ForProjectSpace(space).ListSummaries(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != p.ID {
		t.Fatal("space list depended on owner", rows, err)
	}
	fileID := identity.New("file")
	file := upload.File{ID: fileID, SpaceID: space, OwnerID: owner, CreatorID: creator, Filename: "原始.txt", CreatedAt: time.Now().UTC(), Original: upload.Blob{Bucket: "isolated-test", ObjectKey: upload.ObjectKey(fileID, "original"), ContentType: "text/plain", SizeBytes: 3, SHA256: strings.Repeat("a", 64)}}
	if err = store.ForUploadedFileSpace(space).CreateUploadedFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	defer store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.uploaded_files WHERE id=$1`, file.ID)
	read, err := store.UploadedFile(ctx, file.ID)
	if err != nil || read.SpaceID != space || read.OwnerID != owner || read.CreatorID != creator {
		t.Fatal(read, err)
	}
	if _, err = store.ForUploadedFileSpace(other).UploadedFile(ctx, file.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("foreign file", err)
	}
}

func TestTaskSpaceCommandOwnerAndActiveTargetConstraints(t *testing.T) {
	store, _, _ := runtimeFixture(t)
	ctx := context.Background()
	space, other := identity.New("team"), identity.New("team")
	for _, id := range []string{space, other} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.spaces(id,kind) VALUES($1,'team')`, id); err != nil {
			t.Fatal(err)
		}
		defer store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.spaces WHERE id=$1`, id)
	}
	tasks := NewTaskStore(store)
	ids := []string{}
	defer func() {
		for _, id := range ids {
			store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.tasks WHERE id=$1`, id)
		}
	}()
	makeTask := func(team, user, key string) task.Task {
		id := identity.New("task")
		ids = append(ids, id)
		return task.Task{ID: id, SpaceID: team, OwnerID: user, CreatorID: user, ScopeID: "project:shared", CommandID: "same-command", Status: task.Queued, State: task.State{Operation: "image.generate", ActiveKey: key, CommandDigest: "same"}}
	}
	first, err := tasks.ForTaskScope(ports.SpaceScope{SpaceID: space, UserID: "alice"}).CreateTask(ctx, makeTask(space, "alice", "asset:a"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := tasks.ForTaskScope(ports.SpaceScope{SpaceID: space, UserID: "bob"}).CreateTask(ctx, makeTask(space, "bob", "asset:b"))
	if err != nil || first.ID == second.ID {
		t.Fatal("per-member commands collided", err)
	}
	duplicate := makeTask(space, "bob", "asset:b")
	again, err := tasks.ForTaskScope(ports.SpaceScope{SpaceID: space, UserID: "bob"}).CreateTask(ctx, duplicate)
	if err != nil || again.ID != second.ID {
		t.Fatal("command retry was not idempotent", err)
	}
	for _, v := range []task.Task{first, second} {
		got, e := tasks.ForTaskScope(ports.SpaceScope{SpaceID: space, UserID: v.OwnerID}).TaskByCommand(ctx, v.ScopeID, v.CommandID)
		if e != nil || got.ID != v.ID {
			t.Fatal("command read crossed member", got, e)
		}
	}
	blocked := makeTask(space, "carol", "asset:a")
	if _, err = tasks.ForTaskSpace(space).CreateTask(ctx, blocked); !hasFaultCode(err, "TASK_ACTIVE") {
		t.Fatal("two members generated same active target", err)
	}
	if _, err = tasks.ForTaskSpace(other).Task(ctx, first.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("foreign task", err)
	}
	separate := makeTask(other, "alice", "asset:a")
	if _, err = tasks.ForTaskSpace(other).CreateTask(ctx, separate); err != nil {
		t.Fatal("target leaked across spaces", err)
	}
	gotSpace, gotOwner, gotCreator, err := tasks.TaskSpace(ctx, first.ID)
	if err != nil || gotSpace != space || gotOwner != "alice" || gotCreator != "alice" {
		t.Fatal("worker ownership was not durable", err)
	}
	claimed, ok, err := tasks.ClaimTask(ctx, first.ID, "worker", time.Minute)
	if err != nil || !ok {
		t.Fatal("claim", err)
	}
	changed := claimed
	changed.SpaceID = other
	if _, err = tasks.CommitTask(ctx, changed, claimed.Revision, claimed.LeaseToken, true); !errors.Is(err, fault.ErrConflict) {
		t.Fatal("worker changed task space", err)
	}
	changed = claimed
	changed.CreatorID = "bob"
	if _, err = tasks.CommitTask(ctx, changed, claimed.Revision, claimed.LeaseToken, true); !errors.Is(err, fault.ErrConflict) {
		t.Fatal("worker changed task creator", err)
	}
	claimed.Status = task.Succeeded
	claimed.NextRunAt = nil
	if _, err = tasks.CommitTask(ctx, claimed, claimed.Revision, claimed.LeaseToken, true); err != nil {
		t.Fatal("valid worker commit", err)
	}
}
func hasFaultCode(err error, code string) bool {
	var v *fault.Error
	return errors.As(err, &v) && v.Code == code
}
