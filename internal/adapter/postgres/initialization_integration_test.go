package postgres_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/adapter/postgres"
	a "drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/identity"
	"drama-agent/internal/testsupport/redisruntime"
	"github.com/jackc/pgx/v5"
)

func bootstrapDB(t *testing.T) (*postgres.Store, *pgx.Conn) {
	t.Helper()
	base := os.Getenv("DRAMA_BOOTSTRAP_TEST_DATABASE_URL")
	if base == "" || os.Getenv("DRAMA_TEST_REDIS_ADDRESS") == "" {
		t.Skip("dedicated bootstrap PG and Redis required")
	}
	parsed, err := url.Parse(base)
	if err != nil || !strings.HasPrefix(parsed.Path, "/drama_release_accounts_test_") {
		t.Fatal("bootstrap test requires an explicitly isolated database")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	name := "drama_release_accounts_test_" + identity.New("seed")
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted); err != nil {
			t.Error(err)
		}
	})
	parsed.Path = "/" + name
	conn, err := pgx.Connect(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	store, err := postgres.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store, conn
}

func TestDistributableSQLCreatesOnlyBootstrapAccountAndPreservesExistingData(t *testing.T) {
	sql, err := os.ReadFile("../../../deploy/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("empty-database-login-and-repeat", func(t *testing.T) {
		store, conn := bootstrapDB(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
		redisruntime.AttachURL(t, store, conn.Config().ConnString())
		var users, business, settings int
		if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM drama_catalog_v1.users),
 (SELECT count(*) FROM drama_catalog_v1.projects)+(SELECT count(*) FROM drama_catalog_v1.tasks)+
 (SELECT count(*) FROM drama_catalog_v1.uploaded_files)+(SELECT count(*) FROM drama_catalog_v1.credit_ledger)+
 (SELECT count(*) FROM drama_catalog_v1.email_verifications)+(SELECT count(*) FROM drama_catalog_v1.session_events)+
 (SELECT count(*) FROM drama_catalog_v1.authorization_codes)+(SELECT count(*) FROM drama_catalog_v1.notification_messages),
 (SELECT count(*) FROM drama_catalog_v1.platform_settings)+(SELECT count(*) FROM drama_catalog_v1.registration_settings)+
 (SELECT count(*) FROM drama_catalog_v1.mail_configuration)+(SELECT count(*) FROM drama_catalog_v1.model_config)+
 (SELECT count(*) FROM drama_catalog_v1.credit_gift_policy)`).Scan(&users, &business, &settings); err != nil || users != 1 || business != 0 || settings != 5 {
			t.Fatalf("unexpected distributed data: users=%d business=%d settings=%d err=%v", users, business, settings, err)
		}
		service := &a.Service{Store: store, Hasher: passwordhash.Bcrypt{}}
		var workspace a.LoginResult
		for _, audience := range []string{"workspace", "operations"} {
			login, err := service.Login(ctx, "Daydream", "Daydream123456", "", audience)
			if err != nil || login.User.Username != "Daydream" || login.User.Email != a.InitialOperatorEmail || login.User.Role != ports.RoleOperator {
				t.Fatalf("initial login failed: user=%+v err=%v", login.User, err)
			}
			if audience == "workspace" {
				workspace = login
			}
		}
		actor, err := service.Authenticate(ctx, workspace.Token)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.UpdateProfile(ctx, actor, "部署者昵称"); err != nil {
			t.Fatal(err)
		}
		const bootstrapPassword = "Daydream123456" // gitleaks:allow -- intentionally public bootstrap password, also documented in README
		const changedPassword = "deployment-changed-password"
		if err = service.ChangePassword(ctx, actor, workspace.Token, bootstrapPassword, changedPassword); err != nil {
			t.Fatal(err)
		}
		login, err := service.Login(ctx, "Daydream", changedPassword, "", "workspace")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
		if created, err := store.InitializeDefaultOperator(ctx); err != nil || created {
			t.Fatal("default initialization changed existing accounts", created, err)
		}
		if _, err = service.Authenticate(ctx, login.Token); err != nil {
			t.Fatal("SQL repeat revoked a current session", err)
		}
		if _, err = service.Login(ctx, "Daydream", "Daydream123456", "", "workspace"); err == nil {
			t.Fatal("SQL repeat restored the public default password")
		}
		login, err = service.Login(ctx, "Daydream", changedPassword, "", "workspace")
		if err != nil || login.User.Username != "部署者昵称" {
			t.Fatal("SQL repeat changed the password or profile", login.User, err)
		}
	})
	t.Run("populated-installation-keeps-accounts-and-settings", func(t *testing.T) {
		store, conn := bootstrapDB(t)
		ctx := context.Background()
		if err := store.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		_, err := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES
 ('existing-operator','prior-owner@example.invalid','既有运营','operator','unchanged-hash'),
 ('existing-user','prior-user@example.invalid','既有用户','user','unchanged-hash');
UPDATE drama_catalog_v1.platform_settings SET name='既有部署',revision=5;
UPDATE drama_catalog_v1.model_config SET connections='[]',revision=8;
INSERT INTO drama_catalog_v1.projects(id,revision,payload,owner_id) VALUES
 ('existing-project',1,'{"id":"existing-project","revision":1,"title":"既有业务"}','existing-user');`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
		if created, err := store.InitializeDefaultOperator(ctx); err != nil || created {
			t.Fatal("default account was planted in an existing installation", created, err)
		}
		var users, originals, projects int
		var name string
		var revision int
		if err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM drama_catalog_v1.users),
 (SELECT count(*) FROM drama_catalog_v1.users WHERE id IN ('existing-operator','existing-user') AND password_hash=convert_to('unchanged-hash','UTF8')),
 (SELECT count(*) FROM drama_catalog_v1.projects WHERE id='existing-project' AND payload->>'title'='既有业务'),
 name,revision FROM drama_catalog_v1.platform_settings WHERE id`).Scan(&users, &originals, &projects, &name, &revision); err != nil || users != 2 || originals != 2 || projects != 1 || name != "既有部署" || revision != 5 {
			t.Fatalf("existing installation changed: users=%d originals=%d projects=%d name=%s revision=%d err=%v", users, originals, projects, name, revision, err)
		}
		// Even an installation containing only an ordinary user is not taken over.
		if _, err = conn.Exec(ctx, `DELETE FROM drama_catalog_v1.users WHERE id='existing-operator'`); err != nil {
			t.Fatal(err)
		}
		if created, err := store.InitializeDefaultOperator(ctx); err != nil || created {
			t.Fatal("ordinary-user-only installation was given a default administrator", created, err)
		}
	})
}
