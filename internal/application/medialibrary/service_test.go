package medialibrary

import (
	"context"
	"errors"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
)

type captureStore struct {
	owner string
	user  string
	q     Query
	rows  []Item
	calls int
}

func (s *captureStore) ListGeneratedMedia(_ context.Context, scope ports.SpaceScope, q Query) ([]Item, error) {
	s.owner = scope.SpaceID
	s.q = q
	s.calls++
	return s.rows, nil
}
func (s *captureStore) GeneratedMedia(_ context.Context, scope ports.SpaceScope, task, output string) (Item, error) {
	s.owner = scope.SpaceID
	s.user = scope.UserID
	s.calls++
	return Item{SpaceID: "owner", OwnerID: "owner", CreatorID: "owner", TaskID: task, OutputID: output}, nil
}
func TestCursorScopesAndPageBoundary(t *testing.T) {
	ctx := context.Background()
	actor := ports.Actor{UserID: "a", Role: ports.RoleUser}
	at := time.Now().UTC()
	store := &captureStore{rows: []Item{{TaskID: "b", OutputID: "2", CreatedAt: at}, {TaskID: "b", OutputID: "1", CreatedAt: at}, {TaskID: "a", OutputID: "3", CreatedAt: at}}}
	s := &Service{Store: store, Resolver: personalAccess{}, Access: personalAccess{}}
	page, err := s.ForScope(scopeOf(actor)).List(ctx, "image", "  雨夜  ", ports.ListAll, "", 2)
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil || store.owner != "a" || store.q.Limit != 3 || store.q.Search != "雨夜" {
		t.Fatal(page, store.q, err)
	}
	cursor := *page.NextCursor
	store.rows = nil
	page, err = s.ForScope(scopeOf(actor)).List(ctx, "image", "雨夜", ports.ListAll, cursor, 2)
	if err != nil || page.NextCursor != nil || store.q.After.TaskID != "b" || store.q.After.OutputID != "1" || !store.q.After.At.Equal(at) {
		t.Fatal(page, store.q, err)
	}
	for _, change := range []struct {
		actor                ports.Actor
		kind, search, cursor string
	}{{actor, "video", "雨夜", cursor}, {actor, "image", "另一查询", cursor}, {ports.Actor{UserID: "other", Role: ports.RoleUser}, "image", "雨夜", cursor}, {actor, "image", "雨夜", "bad"}} {
		calls := store.calls
		_, err = s.ForScope(scopeOf(change.actor)).List(ctx, change.kind, change.search, ports.ListAll, change.cursor, 24)
		if !errors.Is(err, fault.ErrInvalid) || store.calls != calls {
			t.Fatal("cross-filter cursor reached storage", err)
		}
	}
}
func TestLibraryAuthenticationAndValidation(t *testing.T) {
	ctx := context.Background()
	store := &captureStore{}
	s := &Service{Store: store, Resolver: personalAccess{}, Access: personalAccess{}}
	actor := ports.Actor{UserID: "owner", Role: ports.RoleUser}
	if _, err := s.ForScope(ports.SpaceScope{}).List(ctx, "", "", ports.ListAll, "", 24); err == nil || store.calls != 0 {
		t.Fatal("anonymous reached storage")
	}
	for _, limit := range []int{-1, 101} {
		if _, err := s.ForScope(scopeOf(actor)).List(ctx, "", "", ports.ListAll, "", limit); !errors.Is(err, fault.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := s.ForScope(scopeOf(actor)).List(ctx, "audio", "", ports.ListAll, "", 24); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.ForScope(scopeOf(actor)).List(ctx, "", "\x00", ports.ListAll, "", 24); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.ForScope(scopeOf(actor)).Detail(ctx, "task", "output"); err != nil || store.owner != "" || store.user != actor.UserID {
		t.Fatal(err)
	}
	var unavailable *Service
	if _, err := unavailable.ForScope(scopeOf(actor)).List(ctx, "", "", ports.ListAll, "", 0); err == nil {
		t.Fatal("missing store reported success")
	}
}

func scopeOf(actor ports.Actor) ports.SpaceScope {
	return ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}
}

type personalAccess struct{}

func (personalAccess) Resolve(_ context.Context, actor ports.Actor, id string) (ports.SpaceScope, error) {
	if err := actor.Require(); err != nil {
		return ports.SpaceScope{}, err
	}
	if id == "" {
		id = actor.UserID
	}
	if id != actor.UserID {
		return ports.SpaceScope{}, fault.ErrNotFound
	}
	return ports.SpaceScope{SpaceID: id, UserID: actor.UserID, Kind: spaces.Personal}, nil
}
func (p personalAccess) Access(ctx context.Context, actor ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	scope, err := p.Resolve(ctx, actor, r.SpaceID)
	if err != nil {
		return spaces.None, scope, err
	}
	return spaces.Decide(scope, r, nil), scope, nil
}

func TestLibraryCursorBindsSelectionAndDetailReturnsRealAccess(t *testing.T) {
	ctx := context.Background()
	store := &captureStore{rows: []Item{{TaskID: "b", OutputID: "2", CreatedAt: time.Now().UTC()}, {TaskID: "a", OutputID: "1", CreatedAt: time.Now().UTC()}}}
	s := (&Service{Store: store, Resolver: personalAccess{}, Access: personalAccess{}}).ForScope(ports.SpaceScope{SpaceID: "owner", UserID: "owner"})
	page, err := s.List(ctx, "image", "", ports.ListMine, "", 1)
	if err != nil || page.NextCursor == nil || store.q.Filter != ports.ListMine {
		t.Fatal(page, err)
	}
	calls := store.calls
	if _, err = s.List(ctx, "image", "", ports.ListAll, *page.NextCursor, 1); !errors.Is(err, fault.ErrInvalid) || store.calls != calls {
		t.Fatal("selection cursor reused", err)
	}
	if _, err = s.List(ctx, "image", "", ports.ListFilter("bad"), "", 1); !errors.Is(err, fault.ErrInvalid) || store.calls != calls {
		t.Fatal("invalid selection reached SQL", err)
	}
	item, err := s.Detail(ctx, "task", "out")
	if err != nil || item.EffectiveAccess != spaces.Edit {
		t.Fatal("detail lost real access", item, err)
	}
}
