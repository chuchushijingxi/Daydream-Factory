package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
	"drama-agent/internal/testsupport/redisruntime"
)

func fixtureEditingSession(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func configurePostgresFixtureEditing(t *testing.T, s *app.Service) {
	t.Helper()
	if s == nil {
		return
	}
	if s.Editing == nil {
		s.Editing = redisruntime.Connect(t)
	}
	if s.EditorNames == nil {
		names, ok := s.Store.(ports.ProjectEditorNames)
		if !ok {
			t.Fatal("PG fixture store must provide real project editor names")
		}
		s.EditorNames = names
	}
}
func configureMemoryFixtureEditing(s *app.Service) {
	if s == nil {
		return
	}
	if s.Editing == nil {
		s.Editing = &fixtureProjectEditor{owners: map[string]ports.ProjectEditor{}}
	}
	if s.EditorNames == nil {
		if names, ok := s.Store.(ports.ProjectEditorNames); ok {
			s.EditorNames = names
		} else {
			s.EditorNames = fixtureEditorNames{}
		}
	}
}

type fixtureEditorNames struct{}

func (fixtureEditorNames) ProjectEditorName(_ context.Context, id string) (string, error) {
	return id, nil
}

// Only legacy single-user wrappers simulate entering the project page. Requests
// still traverse the production auth/scope/editing route; the raw security
// handler never auto-acquires a lease. Failed acquisition is left to the original
// command to report, preserving its payload, revision and authorization checks.
func acquireLegacyFixtureEditing(next http.Handler, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return
	}
	suffix, ok := strings.CutPrefix(r.URL.Path, "/api/projects/")
	if !ok {
		return
	}
	parts := strings.Split(suffix, "/")
	if parts[0] == "" || parts[0] == "quote" || len(parts) > 1 && parts[1] == "editing" {
		return
	}
	acquire := r.Clone(r.Context())
	acquire.Method = http.MethodPost
	target := *r.URL
	target.Path = "/api/projects/" + parts[0] + "/editing"
	target.RawPath = ""
	acquire.URL = &target
	acquire.RequestURI = target.RequestURI()
	acquire.Body = http.NoBody
	acquire.GetBody = nil
	acquire.ContentLength = 0
	acquire.Header = r.Header.Clone()
	acquire.Header.Del("Content-Type")
	acquire.Header.Del("Content-Length")
	next.ServeHTTP(httptest.NewRecorder(), acquire)
}

type fixtureProjectEditor struct {
	mu     sync.Mutex
	owners map[string]ports.ProjectEditor
}

func (s *fixtureProjectEditor) update(ctx context.Context, action, project, user, session string) (ports.ProjectEditor, error) {
	if err := ctx.Err(); err != nil {
		return ports.ProjectEditor{}, err
	}
	if project == "" || user == "" || session == "" {
		return ports.ProjectEditor{}, fault.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owners == nil {
		s.owners = map[string]ports.ProjectEditor{}
	}
	now := time.Now().UTC()
	owner := s.owners[project]
	if !owner.ExpiresAt.After(now) {
		delete(s.owners, project)
		owner = ports.ProjectEditor{}
	}
	same := owner.UserID == user && owner.SessionID == session
	if action == "acquire" && (owner.UserID == "" || same) || action == "renew" && same {
		owner = ports.ProjectEditor{UserID: user, SessionID: session, ExpiresAt: now.Add(90 * time.Second)}
		s.owners[project] = owner
	}
	if action == "release" && same {
		delete(s.owners, project)
		return ports.ProjectEditor{}, nil
	}
	if action == "check" && !same {
		return ports.ProjectEditor{}, fault.New("PROJECT_EDITING_BY_OTHER", "fixture editing lease missing or owned by another session")
	}
	return owner, nil
}
func (s *fixtureProjectEditor) AcquireProjectEditing(ctx context.Context, p, u, session string) (ports.ProjectEditor, error) {
	return s.update(ctx, "acquire", p, u, session)
}
func (s *fixtureProjectEditor) RenewProjectEditing(ctx context.Context, p, u, session string) (ports.ProjectEditor, error) {
	return s.update(ctx, "renew", p, u, session)
}
func (s *fixtureProjectEditor) ReleaseProjectEditing(ctx context.Context, p, u, session string) error {
	_, err := s.update(ctx, "release", p, u, session)
	return err
}
func (s *fixtureProjectEditor) CheckProjectEditing(ctx context.Context, p, u, session string) error {
	_, err := s.update(ctx, "check", p, u, session)
	return err
}

func TestFixtureEditingRequiresRealAcquisitionAndMatchingSession(t *testing.T) {
	ctx := context.Background()
	editor := &fixtureProjectEditor{}
	if err := editor.CheckProjectEditing(ctx, "project", "user", "session"); err == nil {
		t.Fatal("fixture silently allowed an unacquired lease")
	}
	first, err := editor.AcquireProjectEditing(ctx, "project", "user", "session")
	if err != nil || first.SessionID != "session" {
		t.Fatal(first, err)
	}
	other, err := editor.AcquireProjectEditing(ctx, "project", "user", "different-session")
	if err != nil || other.SessionID != "session" {
		t.Fatal("fixture stole another session's lease", other, err)
	}
	if err = editor.CheckProjectEditing(ctx, "project", "user", "different-session"); err == nil {
		t.Fatal("fixture allowed wrong session")
	}
	if err = editor.ReleaseProjectEditing(ctx, "project", "user", "different-session"); err != nil {
		t.Fatal(err)
	}
	if err = editor.CheckProjectEditing(ctx, "project", "user", "session"); err != nil {
		t.Fatal("wrong session released the lease", err)
	}
	if err = editor.ReleaseProjectEditing(ctx, "project", "user", "session"); err != nil {
		t.Fatal(err)
	}
	if err = editor.CheckProjectEditing(ctx, "project", "user", "session"); err == nil {
		t.Fatal("fixture retained released ownership")
	}
}
func TestLegacyFixtureWrapperEntersEditingButRawHandlerDoesNot(t *testing.T) {
	store := &fileProjectStore{project: d.Project{ID: "project_fixture", SpaceID: "http-fixture", OwnerID: "http-fixture", CreatorID: "http-fixture", Revision: 1}}
	service := &app.Service{Store: store}
	host, raw := httpapi.New(configuredFixture(service), nil, "", 0)
	defer host.Shutdown(context.Background())
	loggedIn := authenticatedFixture(host, raw)
	request := func(handler http.Handler) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/projects/project_fixture/delete", bytes.NewBufferString(`{"revision":1}`))
		r.Header.Set("Content-Type", "application/json")
		if handler == nil {
			addSessionCookie(r, authenticatedFixtureToken)
			handler = raw
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	denied := request(nil)
	if denied.Code != 409 || !strings.Contains(denied.Body.String(), "PROJECT_EDITING_BY_OTHER") {
		t.Fatal("raw handler automatically acquired editing", denied.Code, denied.Body.String())
	}
	accepted := request(loggedIn)
	if accepted.Code != 200 || store.project.DeletedAt == nil {
		t.Fatal("legacy wrapper did not enter through editing route", accepted.Code, accepted.Body.String())
	}
}
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
