package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/application/medialibrary"
	"drama-agent/internal/application/ports"
	spaceapp "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	pd "drama-agent/internal/domain/project"
	sd "drama-agent/internal/domain/spaces"
	td "drama-agent/internal/domain/task"
)

func TestTeamPGMediaLibrarySourceTitleFollowsProjectAccess(t *testing.T) {
	f := newTeamPG(t, 5)
	ctx := context.Background()
	f.join(t, f.one)
	f.join(t, f.two)
	if _, err := f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.teams SET policy='isolated' WHERE space_id=$1`, f.team.ID); err != nil {
		t.Fatal(err)
	}
	p := pd.Project{ID: identity.New("private_media_source"), SpaceID: f.team.ID, OwnerID: f.one.UserID, CreatorID: f.one.UserID, Revision: 1, Title: identity.New("private_source_title")}
	if err := f.store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	// 单独转交的创作资产可见，不代表其来源项目及后续改名也可见。
	task := td.Task{ID: identity.New("transferred_media"), SpaceID: f.team.ID, OwnerID: f.two.UserID, CreatorID: f.one.UserID, ScopeID: "project:" + p.ID, CommandID: identity.New("media_command"), Status: td.Succeeded,
		Snapshot: td.Snapshot{Input: json.RawMessage(`{"prompt":"获授权素材正文"}`)}, State: td.State{Operation: "image.generate", ModelID: "fixture", Result: &td.Result{Files: []td.Output{{ID: "out", Width: 100, Height: 100, ContentType: "image/png", Blob: td.Blob{SizeBytes: 1}}}}}}
	if _, err := NewTaskStore(f.store).CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	service := &medialibrary.Service{Store: f.store, Resolver: f.store, Access: f.store}
	teamScope := ports.SpaceScope{SpaceID: f.team.ID, UserID: f.two.UserID}
	// 详情按实际素材空间授权，当前选择个人空间也不能影响来源标题的判定。
	detailScope := ports.SpaceScope{SpaceID: f.two.UserID, UserID: f.two.UserID}
	check := func(wantTitle string) {
		t.Helper()
		page, err := service.ForScope(teamScope).List(ctx, "image", "", ports.ListAll, "", 24)
		if err != nil || len(page.Items) != 1 || page.Items[0].ProjectTitle != wantTitle || page.Items[0].OwnerID != f.two.UserID {
			t.Fatal("asset visibility or source title projection", page, err)
		}
		detail, err := service.ForScope(detailScope).Detail(ctx, task.ID, "out")
		if err != nil || detail.ProjectTitle != wantTitle || detail.Prompt != "获授权素材正文" {
			t.Fatal("detail source title projection", detail, err)
		}
		page, err = service.ForScope(teamScope).List(ctx, "image", p.Title, ports.ListAll, "", 24)
		wantCount := 0
		if wantTitle != "" {
			wantCount = 1
		}
		if err != nil || len(page.Items) != wantCount {
			t.Fatal("source title search exposed inaccessible project", page, err)
		}
		page, err = service.ForScope(teamScope).List(ctx, "image", "获授权素材正文", ports.ListAll, "", 24)
		if err != nil || len(page.Items) != 1 {
			t.Fatal("source masking hid independently visible asset", page, err)
		}
	}
	check("")
	if _, err := f.service.PutShare(ctx, f.one, f.team.ID, sd.ProjectResource, p.ID, spaceapp.ShareInput{Grantee: f.two.UserID, Permission: sd.Read}); err != nil {
		t.Fatal(err)
	}
	check(p.Title)
	if err := f.service.DeleteShare(ctx, f.one, f.team.ID, sd.ProjectResource, p.ID, f.two.UserID); err != nil {
		t.Fatal(err)
	}
	check("")
	p.Title = identity.New("renamed_private_source")
	p.Revision++
	if err := f.store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	check("")
	manager, err := service.ForScope(ports.SpaceScope{SpaceID: f.team.ID, UserID: f.owner.UserID}).Detail(ctx, task.ID, "out")
	if err != nil || manager.ProjectTitle != p.Title {
		t.Fatal("manager lost source metadata", manager, err)
	}
}

func TestMediaLibraryHidesOnlyTheAppendedFixedStyle(t *testing.T) {
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("isolated PG required")
	}
	ctx := context.Background()
	store, err := Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	style := "固定风格专用词：📷 电影照明与色彩。"
	p := pd.Project{ID: identity.New("style_projection"), OwnerID: identity.New("style_owner"), Revision: 1, Title: "风格投影项目", Style: &pd.ProjectStyle{ID: "fixed", Prompts: pd.StylePrompts{ZH: style}}}
	p = fixtureProject(t, store, p)
	if err = store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.projects WHERE id=$1`, p.ID)
	service := &medialibrary.Service{Store: store, Resolver: store, Access: store}
	actor := ports.Actor{UserID: p.OwnerID, Role: ports.RoleUser}
	quoted := "正文引用：\n\n" + style + "\n\n引用结束。"
	for _, tc := range []struct{ kind, prompt, want string }{
		{"image", "📷 当次图片正文\n\n" + style, "📷 当次图片正文"},
		{"video", "当次视频正文\n\n" + style, "当次视频正文"},
		{"image", quoted + "\n\n" + style + "\n\n画面构图保持完整。", quoted + "\n\n画面构图保持完整。"},
		{"image", "没有匹配风格的旧输入，不可裁剪。", "没有匹配风格的旧输入，不可裁剪。"},
	} {
		id := identity.New("style_task")
		input, _ := json.Marshal(map[string]any{"prompt": tc.prompt})
		mime := tc.kind + "/png"
		if tc.kind == "video" {
			mime = "video/mp4"
		}
		task := td.Task{ID: id, OwnerID: p.OwnerID, SpaceID: p.SpaceID, CreatorID: p.OwnerID, ScopeID: "project:" + p.ID, CommandID: id, Status: td.Succeeded,
			Snapshot: td.Snapshot{Input: input}, State: td.State{Operation: tc.kind + ".generate", ModelID: "fixture",
				Result: &td.Result{Files: []td.Output{{ID: "out", Width: 100, Height: 100, ContentType: mime, Blob: td.Blob{SizeBytes: 1}}}}}}
		if _, err = NewTaskStore(store).CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		defer store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.tasks WHERE id=$1`, id)
		v, e := service.ForScope(ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}).Detail(ctx, id, "out")
		if e != nil || v.Prompt != tc.want || v.PromptPreview != tc.want {
			t.Fatal("display changed original body or retained appended style", v, e)
		}
		var original string
		if e = store.pool.QueryRow(ctx, `SELECT input_snapshot#>>'{input,prompt}' FROM drama_catalog_v1.tasks WHERE id=$1`, id).Scan(&original); e != nil || original != tc.prompt {
			t.Fatal("transport snapshot was rewritten", e)
		}
	}
	page, err := service.ForScope(ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}).List(ctx, "", "固定风格专用词", ports.ListAll, "", 24)
	if err != nil || len(page.Items) != 1 || !strings.HasPrefix(page.Items[0].PromptPreview, "正文引用：") {
		t.Fatal("search used hidden style or lost an intentional body quote", page, err)
	}
}

func TestMediaLibraryCursorSearchOwnershipAndFrozenInput(t *testing.T) {
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("isolated PG required")
	}
	ctx := context.Background()
	store, err := Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner := identity.New("library_owner")
	other := identity.New("library_other")
	deleted := time.Now().UTC()
	projects := []pd.Project{{ID: identity.New("lib_project"), OwnerID: owner, Revision: 1, Title: "项目甲"}, {ID: identity.New("lib_other"), OwnerID: other, Revision: 1, Title: "项目乙"}, {ID: identity.New("lib_deleted"), OwnerID: owner, Revision: 1, Title: "已删除", DeletedAt: &deleted}}
	for i := range projects {
		projects[i] = fixtureProject(t, store, projects[i])
		p := projects[i]
		if err = store.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		defer store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.projects WHERE id=$1`, p.ID)
	}
	at := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	ids := []string{}
	add := func(p pd.Project, suffix, kind, prompt string, status td.Status, n int) {
		id := identity.New("lib_task") + suffix
		ids = append(ids, id)
		input, _ := json.Marshal(map[string]any{"prompt": prompt, "aspectRatio": "16:9", "resolution": "4K"})
		contextRaw := json.RawMessage(`{"request":{"target":{"assetId":"asset"}}}`)
		files := []td.Output{}
		for i := 0; i < n; i++ {
			mime := "image/png"
			seconds := 0.
			if kind == "video" {
				mime = "video/mp4"
				seconds = 8
			}
			files = append(files, td.Output{ID: string(rune('a' + i)), FileID: "file", Width: 1024, Height: 576, DurationSeconds: seconds, ContentType: mime, Blob: td.Blob{SizeBytes: 128, ObjectKey: "private", Bucket: "secret"}})
		}
		task := td.Task{ID: id, OwnerID: p.OwnerID, SpaceID: p.SpaceID, CreatorID: p.OwnerID, ScopeID: "project:" + p.ID, CommandID: id, Status: status, Snapshot: td.Snapshot{Input: input, CallerContext: contextRaw}, State: td.State{Operation: kind + ".generate", ModelID: "frozen-model", Binding: td.Binding{RequestModelID: "original-provider-model", Credential: []byte("secret-key"), Endpoint: "private-endpoint"}, Result: &td.Result{Files: files}}}
		if _, err := NewTaskStore(store).CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `UPDATE drama_catalog_v1.tasks SET created_at=$2 WHERE id=$1`, id, at); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, id := range ids {
			store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.tasks WHERE id=$1`, id)
		}
	}()
	add(projects[0], "1", "image", "冻结提示词 %_ 字面", td.Succeeded, 2)
	add(projects[0], "2", "video", "冻结视频正文", td.Succeeded, 1)
	add(projects[1], "3", "image", "其他用户", td.Succeeded, 1)
	add(projects[2], "4", "image", "已删除项目", td.Succeeded, 1)
	add(projects[0], "5", "image", "失败不入库视图", td.Failed, 1)
	service := &medialibrary.Service{Store: store, Resolver: store, Access: store}
	actor := ports.Actor{UserID: owner, Role: ports.RoleUser}
	cursor := ""
	seen := map[string]bool{}
	var chosen medialibrary.Item
	for round := 0; round < 5; round++ {
		page, e := service.ForScope(ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}).List(ctx, "", "", ports.ListAll, cursor, 1)
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range page.Items {
			key := v.TaskID + v.OutputID
			if seen[key] || v.ProjectID != projects[0].ID {
				t.Fatal("duplicate or leaked row", v)
			}
			seen[key] = true
			chosen = v
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 3 {
		t.Fatal("output cursor lost same-timestamp files", seen)
	}
	page, err := service.ForScope(ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}).List(ctx, "image", "%_", ports.ListAll, "", 24)
	if err != nil || len(page.Items) != 2 {
		t.Fatal("literal wildcard search", page, err)
	}
	page, err = service.ForScope(ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}).List(ctx, "video", "项目甲", ports.ListAll, "", 24)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("project search or kind", page, err)
	}
	v, err := service.ForScope(ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}).Detail(ctx, chosen.TaskID, chosen.OutputID)
	if err != nil || v.Prompt == "" || v.ModelID != "frozen-model" || v.Resolution != "4K" {
		t.Fatal("frozen metadata lost", v, err)
	}
	for _, forbidden := range []struct {
		actor ports.Actor
		task  string
	}{{ports.Actor{UserID: other, Role: ports.RoleUser}, chosen.TaskID}, {actor, ids[3]}, {actor, ids[4]}} {
		if _, e := service.ForScope(ports.SpaceScope{SpaceID: forbidden.actor.UserID, UserID: forbidden.actor.UserID}).Detail(ctx, forbidden.task, "a"); !errors.Is(e, fault.ErrNotFound) {
			t.Fatal("hidden output visible", e)
		}
	}
	if _, err := store.ListGeneratedMedia(ctx, ports.SpaceScope{}, medialibrary.Query{Limit: 1}); err == nil {
		t.Fatal("blank owner accepted")
	}
	// 查询内投影不能成为跨请求缓存：改名和软删除须在下一次读取立即生效，
	// 任务的冻结提示词及分页依据保持原值。
	p, err := store.Snapshot(ctx, projects[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Title = "新项目名称"
	p.Revision++
	if err = store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	page, err = service.ForScope(ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}).List(ctx, "image", p.Title, ports.ListAll, "", 24)
	if err != nil || len(page.Items) != 2 || page.Items[0].ProjectTitle != p.Title {
		t.Fatal("renamed project metadata stale", page, err)
	}
	p.DeletedAt = &deleted
	p.Revision++
	if err = store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	page, err = service.ForScope(ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}).List(ctx, "", "", ports.ListAll, "", 24)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("deleted project remained in listing", page, err)
	}
	if _, err = service.ForScope(ports.SpaceScope{SpaceID: actor.UserID, UserID: actor.UserID}).Detail(ctx, chosen.TaskID, chosen.OutputID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("deleted project remained in detail", err)
	}
}
