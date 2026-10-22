package upload_test

import (
	"context"
	"errors"
	"testing"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
	d "drama-agent/internal/domain/upload"
	"drama-agent/internal/testsupport/uploadfiles"
)

type fileAccess struct{ allowed bool }

func (p *fileAccess) Resolve(_ context.Context, a ports.Actor, id string) (ports.SpaceScope, error) {
	if err := a.Require(); err != nil {
		return ports.SpaceScope{}, err
	}
	return ports.SpaceScope{SpaceID: id, UserID: a.UserID, Kind: spaces.TeamKind, Role: spaces.Member}, nil
}

type racedGeneratedFile struct {
	file   d.File
	mutate func(*d.File)
}

func (s *racedGeneratedFile) UploadedFile(context.Context, string) (d.File, error) {
	if s.file.ID == "" {
		return d.File{}, fault.ErrNotFound
	}
	return s.file, nil
}

func (s *racedGeneratedFile) CreateUploadedFile(_ context.Context, file d.File) error {
	s.file = file
	if s.mutate != nil {
		s.mutate(&s.file)
	}
	return fault.ErrConflict
}

func (s *racedGeneratedFile) ForUploadedFileSpace(string) ports.UploadedFileStore { return s }

func TestGeneratedFileConflictReadbackRequiresSameOwnership(t *testing.T) {
	for _, field := range []string{"same", "space", "owner", "creator"} {
		t.Run(field, func(t *testing.T) {
			service := uploadfiles.New()
			service.Thumbnails = thumbnailFixture{}
			service.Store = &racedGeneratedFile{mutate: func(file *d.File) {
				// 模拟首次读取后由并发写入者占用了同一确定性 ID。
				switch field {
				case "space":
					file.SpaceID = "other-space"
				case "owner":
					file.OwnerID = "other-owner"
				case "creator":
					file.CreatorID = "other-creator"
				}
			}}
			files := service.ForGeneratedResource("team", "assignee", "original")
			_, err := files.ImportGenerated(context.Background(), "raced-task-output", ports.MediaResult{
				ContentType: "image/webp", Data: []byte("RIFF\x10\x00\x00\x00WEBPVP8 fictional"),
			})
			if field == "same" {
				if err != nil {
					t.Fatal("same generated resource should remain idempotent", err)
				}
			} else if !errors.Is(err, fault.ErrConflict) {
				t.Fatal("adopted a generated file with different ownership", err)
			}
		})
	}
}
func (p *fileAccess) Access(ctx context.Context, a ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	scope, err := p.Resolve(ctx, a, r.SpaceID)
	if !p.allowed {
		return spaces.None, scope, nil
	}
	return spaces.Edit, scope, err
}
func TestUploadScopeAndResourceSpaceRead(t *testing.T) {
	ctx := context.Background()
	service := uploadfiles.New()
	access := &fileAccess{allowed: true}
	service.Resolver, service.Access = access, access
	first := service.ForScope(ports.SpaceScope{SpaceID: "team-first", UserID: "creator"})
	file, err := first.Upload(ctx, app.Input{Filename: "private.txt", Data: []byte("preserved")})
	if err != nil || file.SpaceID != "team-first" || file.OwnerID != "creator" || file.CreatorID != "creator" {
		t.Fatal(file, err)
	}
	other := service.ForScope(ports.SpaceScope{SpaceID: "team-current", UserID: "reader"})
	read, data, err := other.ReadOriginal(ctx, file.ID)
	if err != nil || read.SpaceID != file.SpaceID || string(data) != "preserved" {
		t.Fatal(read, string(data), err)
	}
	access.allowed = false
	denied, data, err := other.ReadOriginal(ctx, file.ID)
	if !errors.Is(err, fault.ErrNotFound) || denied.ID != "" || len(data) != 0 {
		t.Fatal("forbidden file leaked", denied, data, err)
	}
}
func TestGeneratedFileKeepsOriginalCreatorAfterTransfer(t *testing.T) {
	ctx := context.Background()
	service := uploadfiles.New()
	service.Thumbnails = thumbnailFixture{}
	files := service.ForGeneratedResource("team", "assignee", "original")
	file, err := files.ImportGenerated(ctx, "task-output", ports.MediaResult{ContentType: "image/webp", Data: []byte("RIFF\x10\x00\x00\x00WEBPVP8 fictional")})
	if err != nil || file.SpaceID != "team" || file.OwnerID != "assignee" || file.CreatorID != "original" {
		t.Fatal(file, err)
	}
	repeated, err := files.ImportGenerated(ctx, "task-output", ports.MediaResult{ContentType: "image/webp", Data: []byte("RIFF\x10\x00\x00\x00WEBPVP8 fictional")})
	if err != nil || repeated.ID != file.ID || repeated.CreatorID != "original" {
		t.Fatal(repeated, err)
	}
}
