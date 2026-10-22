package upload_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/upload"
	"drama-agent/internal/testsupport/uploadfiles"
)

type uploadSelector struct {
	ports.UploadedFileStore
	scope ports.SpaceScope
	query ports.UploadedFileQuery
	rows  []ports.UploadedFileItem
	calls int
}

func (s *uploadSelector) ListUploadedFiles(_ context.Context, scope ports.SpaceScope, q ports.UploadedFileQuery) ([]ports.UploadedFileItem, error) {
	s.calls++
	s.scope = scope
	s.query = q
	return s.rows, nil
}
func TestUploadSelectorCursorBindsSpaceActorAndFilterBeforeSQL(t *testing.T) {
	ctx := context.Background()
	access := &fileAccess{allowed: true}
	at := time.Now().UTC()
	store := &uploadSelector{rows: []ports.UploadedFileItem{{ID: "file-a", CreatedAt: at}, {ID: "file-b", CreatedAt: at}, {ID: "file-c", CreatedAt: at}}}
	s := &app.Service{Store: store, Resolver: access}
	scope := ports.SpaceScope{SpaceID: "team", UserID: "member"}
	page, err := s.ForScope(scope).List(ctx, "", "  素材  ", "", 2)
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil || store.query.Filter != ports.ListAll || store.query.Limit != 3 || store.query.Search != "素材" || store.scope.UserID != "member" {
		t.Fatal(page, store.query, err)
	}
	cursor := *page.NextCursor
	store.rows = nil
	page, err = s.ForScope(scope).List(ctx, ports.ListAll, "素材", cursor, 2)
	if err != nil || page.NextCursor != nil || page.Items == nil || store.query.After.ID != "file-b" {
		t.Fatal(page, store.query, err)
	}
	for _, tc := range []struct {
		scope          ports.SpaceScope
		filter         ports.ListFilter
		search, cursor string
	}{
		{scope, ports.ListMine, "素材", cursor}, {scope, ports.ListShared, "素材", cursor}, {ports.SpaceScope{SpaceID: "other", UserID: "member"}, ports.ListAll, "素材", cursor}, {ports.SpaceScope{SpaceID: "team", UserID: "another"}, ports.ListAll, "素材", cursor}, {scope, ports.ListAll, "changed", cursor}, {scope, "unknown", "素材", ""}, {scope, ports.ListAll, "素材", "malformed"},
	} {
		calls := store.calls
		if _, err = s.ForScope(tc.scope).List(ctx, tc.filter, tc.search, tc.cursor, 24); !errors.Is(err, fault.ErrInvalid) || store.calls != calls {
			t.Fatal("invalid cursor/filter reached SQL", tc, err)
		}
	}
	if _, err = s.List(ctx, ports.ListAll, "", "", 24); err == nil {
		t.Fatal("anonymous selector")
	}
	if _, err = s.ForScope(scope).List(ctx, ports.ListAll, strings.Repeat("文", 201), "", 24); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal(err)
	}
}

type countedObjects struct {
	ports.FileObjects
	reads int
}

func (s *countedObjects) ReadFile(ctx context.Context, id string, blob d.Blob) ([]byte, error) {
	s.reads++
	return s.FileObjects.ReadFile(ctx, id, blob)
}
func TestVerifiedContainerFileReadKeepsSpaceBoundaryAndSelectorPolicy(t *testing.T) {
	ctx := context.Background()
	service := uploadfiles.New()
	access := &fileAccess{allowed: true}
	service.Resolver, service.Access = access, access
	file, err := service.ForScope(ports.SpaceScope{SpaceID: "team", UserID: "owner"}).Upload(ctx, app.Input{Filename: "reference.txt", Data: []byte("private reference")})
	if err != nil {
		t.Fatal(err)
	}
	objects := &countedObjects{FileObjects: service.Objects}
	service.Objects = objects
	access.allowed = false
	reader := service.ForScope(ports.SpaceScope{SpaceID: "team", UserID: "shared-reader"})
	if _, _, err = reader.ReadOriginal(ctx, file.ID); !errors.Is(err, fault.ErrNotFound) || objects.reads != 0 {
		t.Fatal("normal asset policy bypassed", err)
	}
	got, raw, err := reader.ReadContainerFile(ctx, "team", file.ID, false)
	if err != nil || got.ID != file.ID || string(raw) != "private reference" || objects.reads != 1 {
		t.Fatal(got, string(raw), err)
	}
	for _, space := range []string{"", "another-team"} {
		before := objects.reads
		got, raw, err = reader.ReadContainerFile(ctx, space, file.ID, false)
		if !errors.Is(err, fault.ErrNotFound) || got.ID != "" || raw != nil || objects.reads != before {
			t.Fatal("container crossed space", got, err)
		}
	}
	if _, _, err = reader.ReadOriginal(ctx, file.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("container read permanently granted file access", err)
	}
}
