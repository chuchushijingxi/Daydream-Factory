package medialibrary

import (
	"context"
	"errors"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

type captureStore struct {
	owner string
	q     Query
	rows  []Item
	calls int
}

func (s *captureStore) ListGeneratedMedia(_ context.Context, owner string, q Query) ([]Item, error) {
	s.owner = owner
	s.q = q
	s.calls++
	return s.rows, nil
}
func (s *captureStore) GeneratedMedia(_ context.Context, owner, task, output string) (Item, error) {
	s.owner = owner
	s.calls++
	return Item{TaskID: task, OutputID: output}, nil
}
func TestCursorScopesAndPageBoundary(t *testing.T) {
	ctx := context.Background()
	actor := ports.Actor{UserID: "a", Role: ports.RoleUser}
	at := time.Now().UTC()
	store := &captureStore{rows: []Item{{TaskID: "b", OutputID: "2", CreatedAt: at}, {TaskID: "b", OutputID: "1", CreatedAt: at}, {TaskID: "a", OutputID: "3", CreatedAt: at}}}
	s := &Service{Store: store}
	page, err := s.List(ctx, actor, "image", "  雨夜  ", "", 2)
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil || store.owner != "a" || store.q.Limit != 3 || store.q.Search != "雨夜" {
		t.Fatal(page, store.q, err)
	}
	cursor := *page.NextCursor
	store.rows = nil
	page, err = s.List(ctx, actor, "image", "雨夜", cursor, 2)
	if err != nil || page.NextCursor != nil || store.q.After.TaskID != "b" || store.q.After.OutputID != "1" || !store.q.After.At.Equal(at) {
		t.Fatal(page, store.q, err)
	}
	for _, change := range []struct {
		actor                ports.Actor
		kind, search, cursor string
	}{{actor, "video", "雨夜", cursor}, {actor, "image", "另一查询", cursor}, {ports.Actor{UserID: "other", Role: ports.RoleUser}, "image", "雨夜", cursor}, {actor, "image", "雨夜", "bad"}} {
		calls := store.calls
		_, err = s.List(ctx, change.actor, change.kind, change.search, change.cursor, 24)
		if !errors.Is(err, fault.ErrInvalid) || store.calls != calls {
			t.Fatal("cross-filter cursor reached storage", err)
		}
	}
}
func TestLibraryAuthenticationAndValidation(t *testing.T) {
	ctx := context.Background()
	store := &captureStore{}
	s := &Service{Store: store}
	actor := ports.Actor{UserID: "owner", Role: ports.RoleUser}
	if _, err := s.List(ctx, ports.Actor{}, "", "", "", 24); err == nil || store.calls != 0 {
		t.Fatal("anonymous reached storage")
	}
	for _, limit := range []int{-1, 101} {
		if _, err := s.List(ctx, actor, "", "", "", limit); !errors.Is(err, fault.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := s.List(ctx, actor, "audio", "", "", 24); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.List(ctx, actor, "", "\x00", "", 24); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Detail(ctx, actor, "task", "output"); err != nil || store.owner != "owner" {
		t.Fatal(err)
	}
	var unavailable *Service
	if _, err := unavailable.List(ctx, actor, "", "", "", 0); err == nil {
		t.Fatal("missing store reported success")
	}
}
