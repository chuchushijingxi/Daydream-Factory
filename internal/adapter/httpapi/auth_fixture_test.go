package httpapi_test

import (
	"context"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"net/http"
)

// Explicit authentication fixture for pre-existing HTTP behavior tests only.
// Security tests below use real PG users and the real production middleware.
type fixtureAuthenticator struct{}

func (fixtureAuthenticator) Authenticate(_ context.Context, token string) (ports.Actor, error) {
	if token != "http-fixture.operations.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
		return ports.Actor{}, fault.New("AUTH_REQUIRED", "fixture session required")
	}
	return ports.Actor{UserID: "http-fixture", Role: ports.RoleOperator}, nil
}
func authenticatedFixture(h *httpapi.Handler, next http.Handler) http.Handler {
	h.Authenticator = fixtureAuthenticator{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addSessionCookie(r, "http-fixture.operations.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		next.ServeHTTP(w, r)
	})
}
func (s recordReadStore) ForProjectOwner(owner string) ports.ProjectStore { return s }
func (s *fileProjectStore) ForProjectOwner(owner string) ports.ProjectStore {
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
