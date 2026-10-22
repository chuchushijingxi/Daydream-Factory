package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	"drama-agent/internal/domain/spaces"
)

func spaceContractStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set DRAMA_TEST_DATABASE_URL to run isolated space PG contracts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store, ctx
}

func TestSpaceResolverAndAccessUseResourceSpace(t *testing.T) {
	store, ctx := spaceContractStore(t)
	user, other, team := identity.New("space-user"), identity.New("space-other"), identity.New("team")
	if _, err := store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,$1,'user','fixture')`, user, user+"@example.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.spaces(id,kind) VALUES($1,'personal'),($2,'personal'),($3,'team')`, user, other, team); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.spaces WHERE id=ANY($1)`, []string{user, other, team})
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.users WHERE id=$1`, user)
	})
	actor := ports.Actor{UserID: user, Role: ports.RoleUser}
	resolved, err := store.Resolve(ctx, actor, "")
	if err != nil || resolved.SpaceID != user || resolved.Kind != spaces.Personal || resolved.UserID != user || resolved.Role != spaces.Owner || resolved.Status != spaces.Active {
		t.Fatal("personal space did not resolve to stable user identity", resolved, err)
	}
	for _, role := range []ports.Role{ports.RoleUser, ports.RoleOperator} {
		for _, inaccessible := range []string{other, team, "missing-space"} {
			if _, err := store.Resolve(ctx, ports.Actor{UserID: user, Role: role}, inaccessible); !errors.Is(err, fault.ErrNotFound) {
				t.Fatalf("%s accessed foreign space %s: %v", role, inaccessible, err)
			}
		}
	}
	if _, err = store.Resolve(ctx, ports.Actor{}, user); err == nil {
		t.Fatal("anonymous actor resolved a space")
	}
	resource := spaces.Resource{SpaceID: user, OwnerID: user, Kind: spaces.ProjectResource, ID: "project"}
	// 原生文件请求与跨空间链接不能依赖当前选择头授权。
	selected := ports.WithRequestedSpace(ctx, other)
	permission, actual, err := store.Access(selected, actor, resource)
	if err != nil || permission != spaces.Edit || actual.SpaceID != user {
		t.Fatal("current space selector overrode resource identity", permission, actual, err)
	}
	resource.OwnerID = other
	if permission, _, err = store.Access(ctx, actor, resource); err != nil || permission != spaces.None {
		t.Fatal("personal resource owned by another user became visible", permission, err)
	}
	resource.SpaceID = other
	if permission, _, err = store.Access(ctx, actor, resource); permission != spaces.None || !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("foreign resource leaked existence", permission, err)
	}
	resource.SpaceID = ""
	if permission, _, err = store.Access(ctx, actor, resource); permission != spaces.None || !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("missing resource space fell back to personal", permission, err)
	}
}
