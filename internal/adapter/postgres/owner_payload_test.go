package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	project "drama-agent/internal/domain/project"
	upload "drama-agent/internal/domain/upload"
)

func TestOwnerColumnRemainsAuthoritativeWithoutPayloadCopy(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	owner := identity.New("payload_owner")
	p := project.Project{ID: identity.New("payload_project"), OwnerID: owner, Revision: 1, Title: "完整原文", Original: []byte("保留原文")}
	store := s.ForProjectOwner(owner)
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	assertPayload := func(table, id string) {
		t.Helper()
		var raw []byte
		var storedOwner string
		if err := conn.QueryRow(ctx, `SELECT payload,owner_id FROM drama_catalog_v1.`+table+` WHERE id=$1`, id).Scan(&raw, &storedOwner); err != nil {
			t.Fatal(err)
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if _, duplicated := payload["ownerId"]; duplicated || storedOwner != owner {
			t.Fatal("owner was duplicated or lost", storedOwner, duplicated)
		}
	}
	assertPayload("projects", p.ID)
	// 既存 JSON 的旧副本不成为权限或归属的第二权威；读取不重写历史。
	if _, err := conn.Exec(ctx, `UPDATE drama_catalog_v1.projects SET payload=jsonb_set(payload,'{ownerId}','"stale-owner"') WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.Snapshot(ctx, p.ID)
	if err != nil || got.OwnerID != owner || project.Clone(got).OwnerID != owner || string(got.Original) != "保留原文" || got.Revision != 1 {
		t.Fatal(got, err)
	}
	if _, err := s.ForProjectOwner("stale-owner").Snapshot(ctx, p.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("JSON owner authorized another user", err)
	}
	got.Revision++
	if err := store.Commit(ctx, got, 1); err != nil {
		t.Fatal(err)
	}
	assertPayload("projects", p.ID)
	fileID := identity.New("file")
	file := upload.File{ID: fileID, OwnerID: owner, Filename: "原文.txt", CreatedAt: time.Now().UTC(), Original: upload.Blob{Bucket: "isolated-test", ObjectKey: upload.ObjectKey(fileID, "original"), ContentType: "text/plain", SizeBytes: 6, SHA256: strings.Repeat("a", 64)}}
	files := s.ForUploadedFileOwner(owner)
	if err := files.CreateUploadedFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	assertPayload("uploaded_files", fileID)
	if _, err := conn.Exec(ctx, `UPDATE drama_catalog_v1.uploaded_files SET payload=jsonb_set(payload,'{ownerId}','"stale-owner"') WHERE id=$1`, fileID); err != nil {
		t.Fatal(err)
	}
	read, err := files.UploadedFile(ctx, fileID)
	if err != nil || read.OwnerID != owner || read.Filename != file.Filename || read.Original != file.Original {
		t.Fatal(read, err)
	}
	if _, err := s.ForUploadedFileOwner("stale-owner").UploadedFile(ctx, fileID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("file JSON owner authorized another user", err)
	}
}
