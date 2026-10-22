package project

import (
	"context"
	"errors"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
	u "drama-agent/internal/domain/upload"
)

type containerFileFixture struct {
	ports.FileReader
	files map[string]u.File
	calls int
}

func (f *containerFileFixture) ForFileScope(ports.SpaceScope) ports.FileReader { return f }
func (f *containerFileFixture) File(context.Context, string) (u.File, error) {
	return u.File{}, fault.ErrNotFound
}
func (f *containerFileFixture) ContainerFile(_ context.Context, space, id string) (u.File, error) {
	f.calls++
	return f.files[id], nil
}
func (f *containerFileFixture) ReadContainerFile(ctx context.Context, space, id string, thumbnail bool) (u.File, []byte, error) {
	v, e := f.ContainerFile(ctx, space, id)
	return v, []byte("published bytes"), e
}
func TestProjectFilesRequireRealSameSpaceReferenceAndReadableContainer(t *testing.T) {
	ctx := context.Background()
	store := scopeProjects{memory: newMemory()}
	p := d.Project{ID: "container", SpaceID: "team", OwnerID: "owner", CreatorID: "creator", Revision: 1, SourceFileID: "source", Assets: []d.Asset{{ID: "asset", ImageFileID: "image", Looks: []d.Look{{ID: "look", ImageFileID: "look-image"}}}}}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	access := &projectAccess{permission: spaces.Read}
	files := &containerFileFixture{files: map[string]u.File{}}
	for _, id := range []string{"source", "image", "look-image", "unreferenced"} {
		files.files[id] = u.File{ID: id, SpaceID: "team", OwnerID: "another-member"}
	}
	service := (&Service{Store: store, Access: access, Files: files}).ForScope(ports.SpaceScope{SpaceID: "other-selected-space", UserID: "shared-reader"})
	for _, id := range []string{"source", "image", "look-image"} {
		if _, err := service.Files.File(ctx, id); !errors.Is(err, fault.ErrNotFound) {
			t.Fatal("fixture selector should deny foreign owner", err)
		}
		file, raw, err := service.ReferencedFileBytes(ctx, p.ID, id, true)
		if err != nil || file.ID != id || string(raw) != "published bytes" {
			t.Fatal(file, err)
		}
	}
	calls := files.calls
	_, err := service.ReferencedFile(ctx, p.ID, "unreferenced")
	if !errors.Is(err, fault.ErrNotFound) || files.calls != calls {
		t.Fatal("client forged container reference", err, files.calls)
	}
	files.files["image"] = u.File{ID: "image", SpaceID: "another-team"}
	_, _, err = service.ReferencedFileBytes(ctx, p.ID, "image", false)
	if !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("cross-space file bytes exposed", err)
	}
	access.permission = spaces.None
	calls = files.calls
	_, err = service.ReferencedFile(ctx, p.ID, "source")
	if !errors.Is(err, fault.ErrNotFound) || files.calls != calls {
		t.Fatal("revoked container kept file capability", err)
	}
}
