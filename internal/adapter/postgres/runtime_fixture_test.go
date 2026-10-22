package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/domain/identity"
	project "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/redisruntime"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func openRuntimePG(t *testing.T, ctx context.Context, url string, settings ...appconfig.Database) (*Store, error) {
	t.Helper()
	store, err := Open(ctx, url, settings...)
	if err == nil {
		redisruntime.AttachURL(t, store, url)
	}
	return store, err
}

func fixtureSpace(t *testing.T, s *Store, id string) string {
	t.Helper()
	if id == "" {
		id = identity.New("fixture_owner")
	}
	// Resolved actors must be real enabled users, not orphan personal spaces.
	tag, err := s.pool.Exec(context.Background(), `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,$1,'user','fixture') ON CONFLICT(id) DO NOTHING`, id, id+"@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	createdSpace := false
	if tag.RowsAffected() == 1 {
		// Some callers defer Store.Close, so cleanup needs its own short-lived
		// connection to this same opt-in test database. Never delete an existing user.
		database := os.Getenv("DRAMA_TEST_DATABASE_URL")
		t.Cleanup(func() { cleanupFixtureIdentity(t, database, id, createdSpace) })
	}
	tag, err = s.pool.Exec(context.Background(), `INSERT INTO drama_catalog_v1.spaces(id,kind) VALUES($1,'personal') ON CONFLICT DO NOTHING`, id)
	if err != nil {
		t.Fatal(err)
	}
	createdSpace = tag.RowsAffected() == 1
	return id
}

func cleanupFixtureIdentity(t *testing.T, database, id string, createdSpace bool) {
	t.Helper()
	if database == "" {
		t.Error("fixture identity cleanup requires DRAMA_TEST_DATABASE_URL")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, database)
	if err != nil {
		t.Error("fixture identity cleanup connection unavailable")
		return
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Error("fixture identity cleanup transaction unavailable")
		return
	}
	defer tx.Rollback(context.Background())
	if createdSpace {
		_, err = tx.Exec(ctx, `DELETE FROM drama_catalog_v1.spaces s WHERE s.id=$1 AND s.kind='personal'
 AND NOT EXISTS(SELECT 1 FROM drama_catalog_v1.projects WHERE space_id=s.id)
 AND NOT EXISTS(SELECT 1 FROM drama_catalog_v1.tasks WHERE space_id=s.id)
 AND NOT EXISTS(SELECT 1 FROM drama_catalog_v1.uploaded_files WHERE space_id=s.id)`, id)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `DELETE FROM drama_catalog_v1.users WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM drama_catalog_v1.spaces WHERE id=$1)`, id)
	}
	if err != nil {
		var constraint *pgconn.PgError
		if errors.As(err, &constraint) && constraint.Code == "23503" {
			return // Other retained facts still use this identity; preserve both rows.
		}
		t.Error("fixture identity cleanup failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		t.Error("fixture identity cleanup commit failed")
	}
}

func fixtureProject(t *testing.T, s *Store, p project.Project) project.Project {
	t.Helper()
	if p.OwnerID == "" {
		p.OwnerID = identity.New("fixture_owner")
	}
	p.SpaceID = fixtureSpace(t, s, p.OwnerID)
	p.CreatorID = p.OwnerID
	return p
}
