package httpapi

import (
	"testing"

	app "drama-agent/internal/application/project"
	taskapp "drama-agent/internal/application/task"
	td "drama-agent/internal/domain/task"
)

func TestProjectMediaImageAndVideoThumbnailLinksUseTaskOutputRoute(t *testing.T) {
	for _, contentType := range []string{"image/png", "image/jpeg", "video/mp4"} {
		view := app.MediaView{ProjectID: "project", View: taskapp.View{ID: "task", Status: td.Succeeded, Result: &taskapp.ResultView{Files: []taskapp.FileView{{ID: "output", FileID: "private-file", ContentType: contentType, ThumbnailURL: "/api/files/private-file/thumbnail"}}}}}
		linked := mediaLinks(view)
		file := linked.Result.Files[0]
		if file.ThumbnailURL != "/api/projects/project/media-tasks/task/outputs/output/thumbnail" || file.ContentURL != "/api/projects/project/media-tasks/task/outputs/output/file" {
			t.Fatal("project media retained independent-upload URL", contentType, file)
		}
	}
	view := app.MediaView{ProjectID: "project", View: taskapp.View{ID: "task", Status: td.Running, Result: &taskapp.ResultView{Files: []taskapp.FileView{{ID: "output", ContentType: "image/png", ThumbnailURL: "/api/files/private/thumbnail"}}}}}
	if got := mediaLinks(view).Result.Files[0].ThumbnailURL; got != "" {
		t.Fatal("unfinished output retained file thumbnail route", got)
	}
}
