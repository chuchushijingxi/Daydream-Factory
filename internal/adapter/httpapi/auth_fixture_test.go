package httpapi_test

import (
	"context"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"net/http"
)

// Explicit authentication fixture for pre-existing HTTP behavior tests only.
// Security tests below use real PG users and the real production middleware.
const authenticatedFixtureToken = "http-fixture.operations.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type fixtureAuthenticator struct{}

func (fixtureAuthenticator) Authenticate(_ context.Context, token string) (ports.Actor, error) {
	if token != authenticatedFixtureToken {
		return ports.Actor{}, fault.New("AUTH_REQUIRED", "fixture session required")
	}
	return ports.Actor{UserID: "http-fixture", Role: ports.RoleOperator}, nil
}
func authenticatedFixture(h *httpapi.Handler, next http.Handler) http.Handler {
	h.Authenticator = fixtureAuthenticator{}
	configureFixtureProject(h.Service)
	configureMemoryFixtureEditing(h.Service)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addSessionCookie(r, authenticatedFixtureToken)
		acquireLegacyFixtureEditing(next, r)
		next.ServeHTTP(w, r)
	})
}
func (s *fileProjectStore) ForProjectSpace(owner string) ports.ProjectStore {
	return &ownedFixtureProject{fileProjectStore: s, owner: owner}
}

type ownedFixtureProject struct {
	*fileProjectStore
	owner string
}

func addSessionCookie(r *http.Request, token string) {
	_, audience, _, err := accounts.SessionParts(token)
	if err != nil {
		panic("invalid isolated test session")
	}
	name := "drama_workspace_session"
	if audience == accounts.Operations {
		name = "drama_operations_session"
	}
	r.Header.Set("X-Drama-Audience", audience)
	r.AddCookie(&http.Cookie{Name: name, Value: token})
}

func (s *ownedFixtureProject) Create(ctx context.Context, p d.Project) error {
	if s.owner == "" || p.SpaceID != s.owner {
		return fault.ErrInvalid
	}
	return s.fileProjectStore.Create(ctx, p)
}
func (s *ownedFixtureProject) Snapshot(ctx context.Context, id string) (d.Project, error) {
	p, err := s.fileProjectStore.Snapshot(ctx, id)
	if err == nil && (s.owner == "" || p.SpaceID != s.owner) {
		return d.Project{}, fault.ErrNotFound
	}
	return p, err
}
func (s *ownedFixtureProject) Commit(ctx context.Context, p d.Project, revision uint64) error {
	if _, err := s.Snapshot(ctx, p.ID); err != nil {
		return err
	}
	if p.SpaceID != s.owner {
		return fault.ErrConflict
	}
	return s.fileProjectStore.Commit(ctx, p, revision)
}
