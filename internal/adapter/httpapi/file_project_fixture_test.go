package httpapi_test

import (
	"context"
	"testing"

	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
)

func TestFileProjectFixtureVisibleListPreservesPersonalIsolation(t *testing.T) {
	store := &fileProjectStore{project: d.Project{ID: "project", SpaceID: "user", OwnerID: "user", CreatorID: "creator", Revision: 1}}
	scope := ports.SpaceScope{SpaceID: "user", UserID: "user", Kind: spaces.Personal}
	for _, filter := range []ports.ListFilter{ports.ListMine, ports.ListShared} {
		rows, err := store.ListVisibleSummaries(context.Background(), scope, filter)
		if err != nil || len(rows) != 0 {
			t.Fatal("fixture ignored immutable creator/shared filter", rows, err)
		}
	}
	rows, err := store.ListVisibleSummaries(context.Background(), scope, ports.ListAll)
	if err != nil || len(rows) != 1 || rows[0].OwnerID != "user" || rows[0].CreatorID != "creator" {
		t.Fatal(rows, err)
	}
	scope.UserID = "other"
	rows, err = store.ListVisibleSummaries(context.Background(), scope, ports.ListAll)
	if err != nil || len(rows) != 0 {
		t.Fatal("fixture leaked a foreign personal list", rows, err)
	}
}
