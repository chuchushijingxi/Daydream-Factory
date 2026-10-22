package httpapi_test

import (
	"context"
	"drama-agent/internal/domain/spaces"
	"testing"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
	"drama-agent/internal/testsupport/spacefixture"
)

func configureFixtureProject(s *app.Service) {
	if s == nil {
		return
	}
	if s.Resolver == nil {
		if resolver, ok := s.Store.(ports.SpaceResolver); ok {
			s.Resolver = resolver
		} else {
			s.Resolver = spacefixture.Personal{}
		}
	}
	if s.Access == nil {
		if access, ok := s.Store.(ports.SpaceAccess); ok {
			s.Access = access
		} else {
			s.Access = spacefixture.Personal{}
		}
	}
}
func configuredFixture(s *app.Service) *app.Service {
	configureFixtureProject(s)
	if s != nil {
		if files, ok := s.Files.(*uploadapp.Service); ok {
			configureFixtureFiles(files)
		}
	}
	return s
}
func configureFixtureFiles(s *uploadapp.Service) {
	if s.Resolver == nil {
		if resolver, ok := s.Store.(ports.SpaceResolver); ok {
			s.Resolver = resolver
		} else {
			s.Resolver = spacefixture.Personal{}
		}
	}
	if s.Access == nil {
		if access, ok := s.Store.(ports.SpaceAccess); ok {
			s.Access = access
		} else {
			s.Access = spacefixture.Personal{}
		}
	}
}
func fixtureProject(t *testing.T, s *app.Service, a ports.Actor) *app.Service {
	t.Helper()
	configureFixtureProject(s)
	if _, ok := s.Resolver.(spacefixture.Personal); !ok {
		seedPostgresFixtureUser(t, a.UserID, a.Role)
		configurePostgresFixtureEditing(t, s)
	} else {
		configureMemoryFixtureEditing(s)
	}
	return s.ForScope(ports.SpaceScope{SpaceID: a.UserID, UserID: a.UserID}).WithEditingSession(fixtureEditingSession(postgresFixtureToken(a.UserID, "workspace")))
}
func fixtureFiles(t *testing.T, s *uploadapp.Service, a ports.Actor) *uploadapp.Service {
	t.Helper()
	configureFixtureFiles(s)
	if _, ok := s.Resolver.(spacefixture.Personal); !ok {
		seedPostgresFixtureUser(t, a.UserID, a.Role)
	}
	return s.ForScope(ports.SpaceScope{SpaceID: a.UserID, UserID: a.UserID})
}

func (s *fileProjectStore) ForProjectScope(scope ports.SpaceScope) ports.ProjectStore {
	return s.ForProjectSpace(scope.SpaceID)
}

// This fake serves the existing single-user upload tests only. Production lists
// continue to use PostgreSQL visibility predicates and immutable creator columns.
func (s *fileProjectStore) ListVisibleSummaries(ctx context.Context, scope ports.SpaceScope, filter ports.ListFilter) ([]ports.ProjectSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filter, err := filter.Normalize()
	if err != nil {
		return nil, err
	}
	out := []ports.ProjectSummary{}
	p := s.project
	if p.ID == "" || p.DeletedAt != nil || scope.Kind != spaces.Personal || scope.SpaceID != scope.UserID || p.SpaceID != scope.SpaceID || filter == ports.ListShared || filter == ports.ListMine && p.CreatorID != scope.UserID {
		return out, nil
	}
	access := spaces.Decide(scope, spaces.Resource{SpaceID: p.SpaceID, OwnerID: p.OwnerID, Kind: spaces.ProjectResource, ID: p.ID}, nil)
	if access == spaces.None {
		return out, nil
	}
	return append(out, ports.ProjectSummary{ListMetadata: ports.ListMetadata{CreatorID: p.CreatorID, CreatorName: p.CreatorID, EffectiveAccess: access}, SpaceID: p.SpaceID, OwnerID: p.OwnerID, ID: p.ID, Title: p.Title, Filename: p.ScriptSource.Filename, UploadedAt: p.UploadedAt, Revision: p.Revision, PreviewImageFileIDs: []string{}}), nil
}
