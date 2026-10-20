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
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	pd "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

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
	if err = store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.projects WHERE id=$1`, p.ID)
	service := &medialibrary.Service{Store: store}
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
		task := td.Task{ID: id, OwnerID: p.OwnerID, ScopeID: "project:" + p.ID, CommandID: id, Status: td.Succeeded,
			Snapshot: td.Snapshot{Input: input}, State: td.State{Operation: tc.kind + ".generate", ModelID: "fixture",
				Result: &td.Result{Files: []td.Output{{ID: "out", Width: 100, Height: 100, ContentType: mime, Blob: td.Blob{SizeBytes: 1}}}}}}
		if _, err = NewTaskStore(store).CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		defer store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.tasks WHERE id=$1`, id)
		v, e := service.Detail(ctx, actor, id, "out")
		if e != nil || v.Prompt != tc.want || v.PromptPreview != tc.want {
			t.Fatal("display changed original body or retained appended style", v, e)
		}
		var original string
		if e = store.pool.QueryRow(ctx, `SELECT input_snapshot#>>'{input,prompt}' FROM drama_catalog_v1.tasks WHERE id=$1`, id).Scan(&original); e != nil || original != tc.prompt {
			t.Fatal("transport snapshot was rewritten", e)
		}
	}
	page, err := service.List(ctx, actor, "", "固定风格专用词", "", 24)
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
	for _, p := range projects {
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
		task := td.Task{ID: id, OwnerID: p.OwnerID, ScopeID: "project:" + p.ID, CommandID: id, Status: status, Snapshot: td.Snapshot{Input: input, CallerContext: contextRaw}, State: td.State{Operation: kind + ".generate", ModelID: "frozen-model", Binding: td.Binding{RequestModelID: "original-provider-model", Credential: []byte("secret-key"), Endpoint: "private-endpoint"}, Result: &td.Result{Files: files}}}
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
	service := &medialibrary.Service{Store: store}
	actor := ports.Actor{UserID: owner, Role: ports.RoleUser}
	cursor := ""
	seen := map[string]bool{}
	var chosen medialibrary.Item
	for round := 0; round < 5; round++ {
		page, e := service.List(ctx, actor, "", "", cursor, 1)
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
	page, err := service.List(ctx, actor, "image", "%_", "", 24)
	if err != nil || len(page.Items) != 2 {
		t.Fatal("literal wildcard search", page, err)
	}
	page, err = service.List(ctx, actor, "video", "项目甲", "", 24)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("project search or kind", page, err)
	}
	v, err := service.Detail(ctx, actor, chosen.TaskID, chosen.OutputID)
	if err != nil || v.Prompt == "" || v.ModelID != "frozen-model" || v.Resolution != "4K" {
		t.Fatal("frozen metadata lost", v, err)
	}
	for _, forbidden := range []struct {
		actor ports.Actor
		task  string
	}{{ports.Actor{UserID: other, Role: ports.RoleUser}, chosen.TaskID}, {actor, ids[3]}, {actor, ids[4]}} {
		if _, e := service.Detail(ctx, forbidden.actor, forbidden.task, "a"); !errors.Is(e, fault.ErrNotFound) {
			t.Fatal("hidden output visible", e)
		}
	}
	if _, err := store.ListGeneratedMedia(ctx, "", medialibrary.Query{Limit: 1}); err == nil {
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
	page, err = service.List(ctx, actor, "image", p.Title, "", 24)
	if err != nil || len(page.Items) != 2 || page.Items[0].ProjectTitle != p.Title {
		t.Fatal("renamed project metadata stale", page, err)
	}
	p.DeletedAt = &deleted
	p.Revision++
	if err = store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	page, err = service.List(ctx, actor, "", "", "", 24)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("deleted project remained in listing", page, err)
	}
	if _, err = service.Detail(ctx, actor, chosen.TaskID, chosen.OutputID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("deleted project remained in detail", err)
	}
}
