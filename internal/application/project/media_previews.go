package project

import (
	"context"
	"encoding/json"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

// EpisodePreviews enriches only the UI read model; agent materials and the
// project aggregate never receive presentation URLs or a second media choice.
func (s *Service) EpisodePreviews(ctx context.Context, p d.Project, v EpisodeView) (EpisodeView, error) {
	if s.MediaTasks == nil || len(v.Shots) == 0 {
		return v, nil
	}
	shots := map[string]int{}
	remaining := map[string]bool{}
	for i, shot := range v.Shots {
		shots[shot.ID] = i
		remaining[shot.ID] = true
		v.Shots[i].VideoTask = nil
		v.Shots[i].ThumbnailURL = ""
		v.Shots[i].PreviewDurationSeconds = nil
	}
	selected := map[string]d.MediaSelection{}
	for _, choice := range p.MediaSelections {
		if _, ok := shots[choice.ShotID]; ok {
			selected[choice.ShotID] = choice
		}
	}
	cursor := ""
	for len(remaining) > 0 {
		list, err := s.MediaTasks.List(ctx, mediaScope(p.ID), "video.generate", cursor, 100)
		if err != nil {
			return v, err
		}
		// 一页任务一次取回快照；逐条 Input 会让分集视图的查询数随任务历史线性放大。
		ids := make([]string, 0, len(list.Items))
		for _, task := range list.Items {
			ids = append(ids, task.ID)
		}
		snapshots, err := s.MediaTasks.Inputs(ctx, mediaScope(p.ID), ids)
		if err != nil {
			return v, err
		}
		for _, task := range list.Items {
			var meta MediaContext
			if json.Unmarshal(snapshots[task.ID].CallerContext, &meta) != nil {
				return v, fault.ErrInvalid
			}
			index, ok := shots[meta.Request.Target.ShotID]
			if !ok || meta.Request.Target.EpisodeID != v.Episode.ID {
				continue
			}
			// 列表按受理时间倒序；最新任务的状态与可预览的成功输出独立投影。
			// 新任务排队/失败时保留旧封面，不能把已有成片误当成任务已结束。
			if v.Shots[index].VideoTask == nil {
				v.Shots[index].VideoTask = &ShotVideoTaskView{ID: task.ID, Status: task.Status}
			}
			if !remaining[meta.Request.Target.ShotID] || task.Status != td.Succeeded || task.Result == nil || len(task.Result.Files) == 0 {
				continue
			}
			choice, adopted := selected[meta.Request.Target.ShotID]
			if adopted && choice.TaskID != task.ID {
				continue
			}
			for _, file := range task.Result.Files {
				if file.ContentType != "video/mp4" || adopted && choice.OutputID != file.ID {
					continue
				}
				v.Shots[index].ThumbnailURL = mediaThumbnailURL(p.ID, task.ID, file.ID)
				v.Shots[index].PreviewDurationSeconds = nil
				if file.DurationSeconds != nil && *file.DurationSeconds > 0 {
					v.Shots[index].PreviewDurationSeconds = file.DurationSeconds
				}
				delete(remaining, meta.Request.Target.ShotID)
				break
			}
		}
		if list.NextCursor == nil {
			break
		}
		cursor = *list.NextCursor
	}
	return v, nil
}

func mediaThumbnailURL(project, task, output string) string {
	return "/api/projects/" + project + "/media-tasks/" + task + "/outputs/" + output + "/thumbnail"
}

func (s *Service) MediaThumbnail(ctx context.Context, id, task, output string) ([]byte, error) {
	if _, err := s.Media(ctx, id, task); err != nil {
		return nil, err
	}
	return s.MediaTasks.Thumbnail(ctx, mediaScope(id), task, output)
}
