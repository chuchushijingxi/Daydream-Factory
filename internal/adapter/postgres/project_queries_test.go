package postgres

import (
	"context"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

func TestProjectSummaryCharacterImagesFollowCurrentRolesAndOwner(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("isolated PG opt-in")
	}
	ctx := context.Background()
	store, err := openRuntimePG(t, ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner := fixtureSpace(t, store, identity.New("preview_owner"))
	reader := store.ForProjectSpace(owner)
	for _, count := range []int{0, 1, 2, 4} {
		p := d.Project{ID: identity.New("preview_project"), OwnerID: owner, SpaceID: owner, CreatorID: owner, Revision: 1,
			Title: "角色图片", Original: []byte("原始剧本不进入列表"),
			Assets: []d.Asset{{ID: "location", Kind: "location", ImageFileID: "not-a-character"},
				{ID: "empty", Kind: "character", Looks: []d.Look{{ID: "look", ImageFileID: "not-the-main-image"}}}}}
		expected := []string{}
		for i := 0; i < count; i++ {
			fileID := "character-file-" + strconv.Itoa(i)
			p.Assets = append(p.Assets, d.Asset{ID: "character-" + strconv.Itoa(i), Kind: "character", ImageFileID: fileID})
			if i < 3 {
				expected = append(expected, fileID)
			}
		}
		if err = reader.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		defer store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.projects WHERE id=$1`, p.ID)
		// 第二次经过实际Redis序列化缓存，不能因HTTP隐藏字段丢失文件引用。
		for pass := 0; pass < 2; pass++ {
			rows, e := reader.ListSummaries(ctx)
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, row := range rows {
				if row.ID == p.ID {
					found = true
					if !reflect.DeepEqual(row.PreviewImageFileIDs, expected) {
						t.Fatalf("count=%d pass=%d images=%v want=%v", count, pass, row.PreviewImageFileIDs, expected)
					}
				}
			}
			if !found {
				t.Fatal("current project missing")
			}
		}
		other, e := store.ForProjectSpace("another-preview-owner").ListSummaries(ctx)
		if e != nil || len(other) != 0 {
			t.Fatal("another owner received project thumbnails", other, e)
		}
		if count > 0 {
			p.Assets[2].ImageFileID = "replacement-main-image"
			p.Revision++
			if e = reader.Commit(ctx, p, p.Revision-1); e != nil {
				t.Fatal(e)
			}
			rows, e := reader.ListSummaries(ctx)
			if e != nil {
				t.Fatal(e)
			}
			for _, row := range rows {
				if row.ID == p.ID && row.PreviewImageFileIDs[0] != "replacement-main-image" {
					t.Fatal("image adoption did not invalidate cached preview", row)
				}
			}
		}
		deleted := time.Now().UTC()
		p.DeletedAt = &deleted
		p.Revision++
		if err = reader.Commit(ctx, p, p.Revision-1); err != nil {
			t.Fatal(err)
		}
		rows, e := reader.ListSummaries(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, row := range rows {
			if row.ID == p.ID {
				t.Fatal("deleted project remained visible")
			}
		}
	}
}

func TestProjectQueryProjectionsPreserveVisibilityRecoveryAndModelReferences(t *testing.T) {
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("isolated PG opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := openRuntimePG(t, ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	uploaded := time.Now().UTC().Truncate(time.Microsecond)
	projects := []d.Project{
		{ID: identity.New("query_empty"), Revision: 1, Title: "无调用", ScriptSource: d.ScriptSource{Filename: "完整.txt"}, UploadedAt: &uploaded},
		{ID: identity.New("query_running"), Revision: 1, Runs: []d.Run{{ID: "run-summary", Contract: d.EpisodeIndexContract, Stage: "episodeIndexReview", Status: d.RunRunning, Model: "query-model-current"}}},
		{ID: identity.New("query_deleted"), Revision: 1, DeletedAt: &uploaded, ProjectSettings: &d.ProjectSettings{LLMModel: "query-model-deleted", ImageModel: "query-image", VideoModel: "query-video"}, Runs: []d.Run{{Status: d.RunRunning, Model: "query-model-history"}}},
	}
	for i := range projects {
		projects[i] = fixtureProject(t, store, projects[i])
		project := projects[i]
		if err = store.Create(ctx, project); err != nil {
			t.Fatal(err)
		}
		defer store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.projects WHERE id=$1`, project.ID)
	}
	rows, err := store.ListSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	visible := map[string]bool{}
	for _, row := range rows {
		visible[row.ID] = true
		if row.ID == projects[0].ID && row.LatestRun != nil {
			t.Fatal("empty project invented task", row.LatestRun)
		}
		if row.ID == projects[1].ID && (row.LatestRun == nil || row.LatestRun.ID != "run-summary" || row.LatestRun.Contract != d.EpisodeIndexContract || row.LatestRun.Stage != "episodeIndexReview" || row.LatestRun.Status != "running") {
			t.Fatal("list task projection lost current stage", row.LatestRun)
		}
		if row.ID == projects[0].ID && (row.Filename != "完整.txt" || row.Title != "无调用" || row.UploadedAt == nil || !row.UploadedAt.Equal(uploaded) || row.Revision != 1) {
			t.Fatal("列表投影改变公开字段", row)
		}
	}
	if !visible[projects[0].ID] || !visible[projects[1].ID] || visible[projects[2].ID] {
		t.Fatal("删除可见性发生变化", visible)
	}
	ids, err := store.RunningProjectIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	running := map[string]bool{}
	for _, id := range ids {
		running[id] = true
	}
	if running[projects[0].ID] || !running[projects[1].ID] || running[projects[2].ID] {
		t.Fatal("恢复范围发生变化", running)
	}
	// 空 runs 可以序列化为 null；删除项目的引用仍须保护历史模型身份。
	references, err := store.ModelReferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ref := range references {
		if ref.LLMModel == "query-model-deleted" {
			found = ref.HasSettings && ref.ImageModel == "query-image" && ref.VideoModel == "query-video" && len(ref.Runs) == 1 && ref.Runs[0].Model == "query-model-history" && ref.Runs[0].Status == "running"
		}
	}
	if !found {
		t.Fatal("删除项目的模型引用丢失")
	}
}
