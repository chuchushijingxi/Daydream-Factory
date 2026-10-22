package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/application/medialibrary"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	n "drama-agent/internal/domain/notifications"
	p "drama-agent/internal/domain/project"
	d "drama-agent/internal/domain/spaces"
	"drama-agent/internal/domain/spaces/spacestest"
	td "drama-agent/internal/domain/task"
	u "drama-agent/internal/domain/upload"
)

func TestSpaceVisibilitySQLUsesDomainAccessCases(t *testing.T) {
	store, _, _ := runtimeFixture(t)
	cases := spacestest.AccessCases()
	input := make([]map[string]any, 0, 2*len(cases))
	for _, c := range cases {
		shares := []map[string]any{}
		for _, share := range c.Shares {
			shares = append(shares, map[string]any{"grantee": share.Grantee, "permission": share.Permission})
		}
		input = append(input, map[string]any{"name": c.Name, "space_id": c.Scope.SpaceID, "user_id": c.Scope.UserID, "kind": c.Scope.Kind, "role": c.Scope.Role, "policy": c.Scope.Policy, "status": c.Scope.Status, "read_only": c.Scope.ReadOnly, "resource_space": c.Resource.SpaceID, "resource_owner": c.Resource.OwnerID, "resource_kind": c.Resource.Kind, "shares": shares})
	}
	expected := make([]d.Access, 0, 2*len(cases))
	for _, c := range cases {
		expected = append(expected, c.Want)
	}
	for i, c := range cases {
		crossed := map[string]any{}
		for key, value := range input[i] {
			crossed[key] = value
		}
		crossed["name"] = c.Name + "/cross-space"
		crossed["resource_space"] = "different-space"
		input = append(input, crossed)
		expected = append(expected, d.None)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	permission := `(SELECT max(sh.permission) FROM jsonb_to_recordset(c.shares) sh(grantee text,permission int) WHERE sh.grantee IN (c.user_id,'*') AND sh.permission IN (1,2))`
	expr := spaceAccessSQL("c", "c.resource_space", "c.resource_owner", "c.resource_kind", permission)
	rows, err := store.pool.Query(context.Background(), `SELECT c.name,`+expr+` FROM jsonb_to_recordset($1) c(name text,space_id text,user_id text,kind text,role text,policy text,status text,read_only boolean,resource_space text,resource_owner text,resource_kind text,shares jsonb)`, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var name string
		var got d.Access
		if err = rows.Scan(&name, &got); err != nil {
			t.Fatal(err)
		}
		if i >= len(expected) {
			t.Fatalf("unexpected extra row %s", name)
		}
		if name != input[i]["name"] || got != expected[i] {
			t.Fatalf("case %d %s SQL=%v domain-table=%v", i, name, got, expected[i])
		}
		i++
	}
	if rows.Err() != nil || i != len(expected) {
		t.Fatalf("checked %d/%d: %v", i, len(expected), rows.Err())
	}
}

func TestTeamPGProjectSharingVisibilityAndFreshFacts(t *testing.T) {
	f := newTeamPG(t, 5)
	ctx := context.Background()
	f.join(t, f.one)
	f.join(t, f.two)
	if _, err := f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.teams SET policy='isolated' WHERE space_id=$1`, f.team.ID); err != nil {
		t.Fatal(err)
	}
	project := p.Project{ID: identity.New("sharing_project"), SpaceID: f.team.ID, OwnerID: f.one.UserID, CreatorID: f.one.UserID, Revision: 1, Title: "共享项目"}
	if err := f.store.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	resource := d.Resource{SpaceID: project.SpaceID, OwnerID: project.OwnerID, Kind: d.ProjectResource, ID: project.ID}
	scope, err := f.store.Resolve(ctx, f.two, f.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want d.Access, filter ports.ListFilter, count int) {
		t.Helper()
		got, _, err := f.store.Access(ctx, f.two, resource)
		if err != nil || got != want {
			t.Fatalf("access %d want %d: %v", got, want, err)
		}
		rows, err := f.store.ListVisibleSummaries(ctx, scope, filter)
		if err != nil || len(rows) != count {
			t.Fatalf("list %s count %d want %d: %v", filter, len(rows), count, err)
		}
		if count > 0 {
			if rows[0].EffectiveAccess != want || rows[0].CreatorID != f.one.UserID || rows[0].CreatorName != "团队测试2" {
				t.Fatalf("metadata %+v", rows[0])
			}
		}
	}
	check(d.None, ports.ListAll, 0)
	if _, err = f.service.PutShare(ctx, f.one, f.team.ID, d.ProjectResource, project.ID, app.ShareInput{Grantee: f.two.UserID, Permission: d.Read}); err != nil {
		t.Fatal(err)
	}
	check(d.Read, ports.ListShared, 1)
	check(d.Read, ports.ListMine, 0)
	if _, err = f.service.PutShare(ctx, f.one, f.team.ID, d.ProjectResource, project.ID, app.ShareInput{Grantee: f.two.UserID, Permission: d.Edit}); err != nil {
		t.Fatal(err)
	}
	check(d.Edit, ports.ListAll, 1)
	if _, err = f.service.PutShare(ctx, f.two, f.team.ID, d.ProjectResource, project.ID, app.ShareInput{Grantee: "*", Permission: d.Edit}); err == nil {
		t.Fatal("editor re-shared resource")
	}
	if _, err = f.service.Sharing(ctx, f.owner, f.team.ID, d.CanvasResource, "unimplemented_canvas"); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("invented canvas", err)
	}
	if err = f.service.DeleteShare(ctx, f.one, f.team.ID, d.ProjectResource, project.ID, f.two.UserID); err != nil {
		t.Fatal(err)
	}
	check(d.None, ports.ListShared, 0)
	if _, err = f.service.PutShare(ctx, f.one, f.team.ID, d.ProjectResource, project.ID, app.ShareInput{Grantee: "*", Permission: d.Read}); err != nil {
		t.Fatal(err)
	}
	check(d.Read, ports.ListShared, 1)
	// Mine means original creator even after a deliberate ownership transfer.
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.projects SET owner_id=$2 WHERE id=$1`, project.ID, f.two.UserID); err != nil {
		t.Fatal(err)
	}
	resource.OwnerID = f.two.UserID
	check(d.Edit, ports.ListMine, 0)
	check(d.Edit, ports.ListShared, 0)
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.projects SET owner_id=$2 WHERE id=$1`, project.ID, f.one.UserID); err != nil {
		t.Fatal(err)
	}
	resource.OwnerID = f.one.UserID
	if err = f.service.DeleteShare(ctx, f.one, f.team.ID, d.ProjectResource, project.ID, "*"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.teams SET policy='edit_all' WHERE space_id=$1`, f.team.ID); err != nil {
		t.Fatal(err)
	}
	check(d.Edit, ports.ListAll, 1)
	check(d.Edit, ports.ListShared, 0)
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.teams SET policy='isolated' WHERE space_id=$1`, f.team.ID); err != nil {
		t.Fatal(err)
	}
	check(d.None, ports.ListAll, 0)
	if _, err = f.service.PutShare(ctx, f.one, f.team.ID, d.ProjectResource, project.ID, app.ShareInput{Grantee: f.two.UserID, Permission: d.Edit}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.teams SET expires_at=$2 WHERE space_id=$1`, f.team.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.credit_accounts SET balance=0,gift_balance=0,recharge_balance=0,legacy_balance=0 WHERE account_id=$1`, f.team.ID); err != nil {
		t.Fatal(err)
	}
	check(d.Read, ports.ListAll, 1)
	if _, err = f.store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.team_members WHERE space_id=$1 AND user_id=$2`, f.team.ID, f.two.UserID); err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.ListVisibleSummaries(ctx, scope, ports.ListAll)
	if err != nil || len(rows) != 0 {
		t.Fatal("stale scope retained removed membership", rows, err)
	}
	if _, _, err = f.store.Access(ctx, f.two, resource); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("removed member kept share", err)
	}
	var persisted bool
	if err = f.store.pool.QueryRow(ctx, `SELECT payload ?| ARRAY['creatorName','sharedWithMe','effectiveAccess'] FROM drama_catalog_v1.projects WHERE id=$1`, project.ID).Scan(&persisted); err != nil || persisted {
		t.Fatal("read metadata persisted", persisted, err)
	}
}

func TestTeamPGSharingNotificationFailureRollsBackFactAndAudit(t *testing.T) {
	f := newTeamPG(t, 3)
	ctx := context.Background()
	f.join(t, f.one)
	project := p.Project{ID: identity.New("sharing_rollback"), SpaceID: f.team.ID, OwnerID: f.one.UserID, CreatorID: f.one.UserID, Revision: 1, Title: "事务"}
	if err := f.store.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	err := f.store.SpaceTx(ctx, f.team.ID, func(tx app.Tx) error {
		if _, e := tx.SharingResource(ctx, d.ProjectResource, project.ID); e != nil {
			return e
		}
		if e := tx.SaveResourceShare(ctx, d.ProjectResource, project.ID, d.Share{Grantee: "*", Permission: d.Edit}, f.one.UserID); e != nil {
			return e
		}
		if e := tx.AppendAudit(ctx, app.AuditEvent{SpaceID: f.team.ID, ActorID: f.one.UserID, ActorKind: "user", Action: "share_atomic_failure", Target: project.ID}); e != nil {
			return e
		}
		return tx.SystemNotify(ctx, n.SystemInput{OperationID: identity.New("share_failure"), RecipientUserID: "missing-user", Title: "失败", Body: "回滚", Link: n.Link{Type: "project", SpaceID: f.team.ID, TargetID: project.ID}})
	})
	if err == nil {
		t.Fatal("missing recipient committed")
	}
	var count int
	if err = f.store.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM drama_catalog_v1.space_shares WHERE space_id=$1)+(SELECT count(*) FROM drama_catalog_v1.team_audit_events WHERE space_id=$1 AND action='share_atomic_failure')`, f.team.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial sharing commit", count, err)
	}
}

func TestTeamPGSelectorsFollowTransferredOwnerAndKeepCreator(t *testing.T) {
	f := newTeamPG(t, 5)
	ctx := context.Background()
	f.join(t, f.one)
	f.join(t, f.two)
	if _, err := f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.teams SET policy='isolated' WHERE space_id=$1`, f.team.ID); err != nil {
		t.Fatal(err)
	}
	project := p.Project{ID: identity.New("selector_project"), SpaceID: f.team.ID, OwnerID: f.one.UserID, CreatorID: f.one.UserID, Revision: 1, Title: "选择器"}
	if err := f.store.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	files := []u.File{}
	for i, name := range []string{"素材%_A", "素材B"} {
		id := identity.New("file")
		file := u.File{ID: id, SpaceID: f.team.ID, OwnerID: f.one.UserID, CreatorID: f.one.UserID, Filename: name, CreatedAt: f.at.Add(time.Duration(i) * time.Second), Original: u.Blob{Bucket: "private-bucket", ObjectKey: u.ObjectKey(id, "original"), ContentType: "image/png", SHA256: strings.Repeat("0", 64), SizeBytes: 16}}
		if err := f.store.CreateUploadedFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	task := td.Task{ID: identity.New("selector_task"), SpaceID: f.team.ID, OwnerID: f.one.UserID, CreatorID: f.one.UserID, ScopeID: "project:" + project.ID, CommandID: identity.New("selector_command"), Status: td.Succeeded, Snapshot: td.Snapshot{Input: json.RawMessage(`{"prompt":"完整私有提示词"}`)}, State: td.State{Operation: "image.generate", ModelID: "fixture", Result: &td.Result{Files: []td.Output{{ID: "out", FileID: files[0].ID, Width: 100, Height: 100, ContentType: "image/png", Blob: td.Blob{SizeBytes: 16}}}}}}
	if _, err := NewTaskStore(f.store).CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	two := ports.SpaceScope{SpaceID: f.team.ID, UserID: f.two.UserID}
	one := ports.SpaceScope{SpaceID: f.team.ID, UserID: f.one.UserID}
	check := func(scope ports.SpaceScope, filter ports.ListFilter, uploads, media int) {
		t.Helper()
		list, err := f.store.ListUploadedFiles(ctx, scope, ports.UploadedFileQuery{Filter: filter, Limit: 25})
		if err != nil || len(list) != uploads {
			t.Fatalf("upload %s %d/%d: %v", filter, len(list), uploads, err)
		}
		generated, err := f.store.ListGeneratedMedia(ctx, scope, medialibrary.Query{Filter: filter, Limit: 25})
		if err != nil || len(generated) != media {
			t.Fatalf("media %s %d/%d: %v", filter, len(generated), media, err)
		}
		for _, item := range list {
			if item.CreatorID != f.one.UserID || item.CreatorName != "团队测试2" || item.SharedWithMe || item.EffectiveAccess == d.None {
				t.Fatalf("upload metadata %+v", item)
			}
			raw, _ := json.Marshal(item)
			if strings.Contains(string(raw), "private-bucket") || strings.Contains(string(raw), "objectKey") {
				t.Fatal("selector storage coordinates leaked", string(raw))
			}
		}
		for _, item := range generated {
			if item.CreatorID != f.one.UserID || item.CreatorName != "团队测试2" || item.SharedWithMe || item.EffectiveAccess == d.None {
				t.Fatalf("media metadata %+v", item)
			}
		}
	}
	check(one, ports.ListMine, 2, 1)
	check(two, ports.ListAll, 0, 0)
	// Explicit container sharing grants its referenced bytes, not selector rows.
	if _, err := f.service.PutShare(ctx, f.one, f.team.ID, d.ProjectResource, project.ID, app.ShareInput{Grantee: f.two.UserID, Permission: d.Read}); err != nil {
		t.Fatal(err)
	}
	check(two, ports.ListAll, 0, 0)
	check(two, ports.ListShared, 0, 0)
	first, err := f.store.ListUploadedFiles(ctx, one, ports.UploadedFileQuery{Filter: ports.ListAll, Limit: 1})
	if err != nil || len(first) != 1 || first[0].ID != files[1].ID {
		t.Fatal("upload cursor first", first, err)
	}
	second, err := f.store.ListUploadedFiles(ctx, one, ports.UploadedFileQuery{Filter: ports.ListAll, Limit: 1, After: &ports.UploadedFilePosition{At: first[0].CreatedAt, ID: first[0].ID}})
	if err != nil || len(second) != 1 || second[0].ID != files[0].ID {
		t.Fatal("upload cursor second", second, err)
	}
	literal, err := f.store.ListUploadedFiles(ctx, one, ports.UploadedFileQuery{Filter: ports.ListAll, Limit: 25, Search: "%_"})
	if err != nil || len(literal) != 1 || literal[0].ID != files[0].ID {
		t.Fatal("upload literal search", literal, err)
	}
	if err = f.service.RemoveMember(ctx, f.owner, f.team.ID, f.one.UserID); err != nil {
		t.Fatal(err)
	}
	receipts, err := f.service.TransferContent(ctx, f.owner, f.team.ID, app.TransferInput{FromUserID: f.one.UserID, ToUserID: f.two.UserID, All: true})
	if err != nil || len(receipts) != 4 {
		t.Fatal("content transfer", receipts, err)
	}
	for _, receipt := range receipts {
		if !receipt.Transferred {
			t.Fatal(receipt)
		}
	}
	check(two, ports.ListAll, 2, 1)
	check(two, ports.ListMine, 0, 0)
	check(one, ports.ListAll, 0, 0)
	// Rejoining does not turn immutable authorship into ownership permission.
	f.join(t, f.one)
	if err = f.service.DeleteShare(ctx, f.two, f.team.ID, d.ProjectResource, project.ID, f.two.UserID); err != nil {
		t.Fatal(err)
	}
	check(one, ports.ListMine, 0, 0)
	for _, ref := range []d.Resource{{SpaceID: f.team.ID, OwnerID: f.two.UserID, Kind: d.UploadResource, ID: files[0].ID}, {SpaceID: f.team.ID, OwnerID: f.two.UserID, Kind: d.MediaResource, ID: task.ID}} {
		got, _, err := f.store.Access(ctx, f.two, ref)
		if err != nil || got != d.Edit {
			t.Fatal("new responsible member inaccessible", got, err)
		}
		got, _, err = f.store.Access(ctx, f.one, ref)
		if err != nil || got != d.None {
			t.Fatal("old creator regained private access", got, err)
		}
	}
	stored, err := f.store.UploadedFile(ctx, files[0].ID)
	if err != nil || stored.OwnerID != f.two.UserID || stored.CreatorID != f.one.UserID {
		t.Fatal("file source changed", stored, err)
	}
	detail, err := f.store.GeneratedMedia(ctx, ports.SpaceScope{}, task.ID, "out")
	if err != nil || detail.OwnerID != f.two.UserID || detail.CreatorID != f.one.UserID {
		t.Fatal("media source changed", detail, err)
	}
}
