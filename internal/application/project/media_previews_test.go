package project

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

type previewTaskStore struct {
	ports.TaskStore
	rows   []td.Task
	inputs map[string]td.Snapshot
	pages  int
}

func (s *previewTaskStore) Task(_ context.Context, id string) (td.Task, error) {
	for _, row := range s.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return td.Task{}, fault.ErrNotFound
}
func (s *previewTaskStore) TaskInput(_ context.Context, id string) (td.Snapshot, error) {
	return s.inputs[id], nil
}
func (s *previewTaskStore) TaskInputs(_ context.Context, scope string, ids []string) (map[string]td.Snapshot, error) {
	out := map[string]td.Snapshot{}
	for _, id := range ids {
		snapshot, ok := s.inputs[id]
		if !ok {
			return nil, fault.ErrNotFound
		}
		out[id] = snapshot
	}
	return out, nil
}
func (s *previewTaskStore) ListTasks(_ context.Context, scope, operation string, cursor *ports.TaskCursor, limit int) ([]td.Task, error) {
	s.pages++
	out := []td.Task{}
	for _, row := range s.rows {
		if row.ScopeID != scope || row.State.Operation != operation || cursor != nil && !row.CreatedAt.Before(cursor.At) {
			continue
		}
		out = append(out, row)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func TestEpisodeCoversPreferAdoptedAndPagePastUnrelatedAndFailedTasks(t *testing.T) {
	store := &previewTaskStore{inputs: map[string]td.Snapshot{}}
	add := func(id, shot string, status td.Status) {
		meta, _ := json.Marshal(MediaContext{Request: MediaCommand{Target: MediaTarget{EpisodeID: "episode", ShotID: shot}}})
		store.inputs[id] = td.Snapshot{CallerContext: meta}
		duration := map[string]float64{"adopted": 4.5, "new": 10}[id]
		store.rows = append(store.rows, td.Task{ID: id, ScopeID: mediaScope("p"), Status: status, CreatedAt: time.Unix(2000-int64(len(store.rows)), 0), State: td.State{Operation: "video.generate", Result: &td.Result{Files: []td.Output{{ID: "video", ContentType: "video/mp4", DurationSeconds: duration}}}}})
	}
	add("failed", "a", td.Failed)
	add("new", "a", td.Succeeded)
	for i := 0; i < 101; i++ {
		add(fmt.Sprint("other", i), "unrelated", td.Succeeded)
	}
	add("adopted", "a", td.Succeeded)
	add("b-video", "b", td.Succeeded)
	s := &Service{MediaTasks: &taskapp.Service{Store: store}}
	p := d.Project{ID: "p", MediaSelections: []d.MediaSelection{{ShotID: "a", TaskID: "adopted", OutputID: "video"}}}
	designDuration := 3.0
	view := EpisodeView{Episode: d.Episode{ID: "episode"}, Shots: []ShotView{{Shot: d.Shot{ID: "a", ShotValue: d.ShotValue{DurationSeconds: &designDuration}}}, {Shot: d.Shot{ID: "b"}}}}
	got, err := s.EpisodePreviews(context.Background(), p, view)
	if err != nil || store.pages < 2 || got.Shots[0].ThumbnailURL != mediaThumbnailURL("p", "adopted", "video") || got.Shots[1].ThumbnailURL != mediaThumbnailURL("p", "b-video", "video") {
		t.Fatal("adopted/pagination", got.Shots, store.pages, err)
	}
	if got.Shots[0].PreviewDurationSeconds == nil || *got.Shots[0].PreviewDurationSeconds != 4.5 || *got.Shots[0].DurationSeconds != 3 || got.Shots[1].PreviewDurationSeconds != nil {
		t.Fatal("preview duration must match chosen output without overwriting design or inventing missing metadata", got.Shots)
	}
	p.MediaSelections = nil
	got, err = s.EpisodePreviews(context.Background(), p, view)
	if err != nil || got.Shots[0].ThumbnailURL != mediaThumbnailURL("p", "new", "video") || got.Shots[0].PreviewDurationSeconds == nil || *got.Shots[0].PreviewDurationSeconds != 10 {
		t.Fatal("latest successful cover", got.Shots, err)
	}
}

func TestEpisodeVideoTaskStateSurvivesRereadAndKeepsSuccessfulCover(t *testing.T) {
	ctx := context.Background()
	store := &previewTaskStore{inputs: map[string]td.Snapshot{}}
	add := func(id, episode, shot string, status td.Status) {
		meta, _ := json.Marshal(MediaContext{Request: MediaCommand{Target: MediaTarget{EpisodeID: episode, ShotID: shot}}})
		store.inputs[id] = td.Snapshot{CallerContext: meta}
		row := td.Task{ID: id, ScopeID: mediaScope("p"), Status: status, CreatedAt: time.Unix(2000-int64(len(store.rows)), 0), State: td.State{Operation: "video.generate"}}
		if status == td.Succeeded {
			row.State.Result = &td.Result{Files: []td.Output{{ID: "video", ContentType: "video/mp4", DurationSeconds: 5}}}
		}
		store.rows = append(store.rows, row)
	}
	add("other-episode", "another", "a", td.Running)
	add("new-a", "episode", "a", td.Queued)
	for i := 0; i < 101; i++ {
		add(fmt.Sprint("unrelated", i), "episode", "unrelated", td.Running)
	}
	add("new-b", "episode", "b", td.Running)
	add("old-a", "episode", "a", td.Succeeded)
	p := d.Project{ID: "p", Revision: 7}
	read := func() EpisodeView {
		t.Helper()
		// 新服务/读视图没有前端内存标记，仍从原持久任务恢复。
		s := &Service{MediaTasks: &taskapp.Service{Store: store}}
		v, err := s.EpisodePreviews(ctx, p, EpisodeView{Episode: d.Episode{ID: "episode"}, Shots: []ShotView{{Shot: d.Shot{ID: "a"}}, {Shot: d.Shot{ID: "b"}}, {Shot: d.Shot{ID: "no-task"}}}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, status := range []td.Status{td.Queued, td.Running, td.Unknown, td.Failed, td.Cancelled, td.Succeeded} {
		store.rows[1].Status = status
		if status == td.Succeeded {
			store.rows[1].State.Result = &td.Result{Files: []td.Output{{ID: "new-video", ContentType: "video/mp4", DurationSeconds: 9}}}
		}
		v := read()
		if v.Shots[0].VideoTask == nil || *v.Shots[0].VideoTask != (ShotVideoTaskView{ID: "new-a", Status: status}) || v.Shots[1].VideoTask == nil || v.Shots[1].VideoTask.ID != "new-b" || v.Shots[2].VideoTask != nil {
			t.Fatal("task association/pagination", status, v.Shots)
		}
		expected := mediaThumbnailURL("p", "old-a", "video")
		if status == td.Succeeded {
			expected = mediaThumbnailURL("p", "new-a", "new-video")
		}
		if v.Shots[0].ThumbnailURL != expected || v.Shots[1].ThumbnailURL != "" || p.Revision != 7 || len(p.MediaSelections) != 0 {
			t.Fatal("completion must update preview without writing/adopting the project", status, v.Shots)
		}
	}
	p.MediaSelections = []d.MediaSelection{{ShotID: "a", TaskID: "old-a", OutputID: "video"}}
	if v := read(); v.Shots[0].ThumbnailURL != mediaThumbnailURL("p", "old-a", "video") || v.Shots[0].VideoTask.ID != "new-a" {
		t.Fatal("new success silently replaced explicit selection", v.Shots)
	}
}
