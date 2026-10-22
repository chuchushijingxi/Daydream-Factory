package medialibrary

import (
	"context"

	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
)

// File uses the asset's own access, not access to its source project. The actual
// task/output relationship comes from the saved media projection and task facts;
// a client-supplied project ID can never grant access to another task's output.
func (s *Service) File(ctx context.Context, taskID, outputID string) ([]byte, taskapp.FileView, error) {
	item, err := s.Detail(ctx, taskID, outputID)
	if err != nil {
		return nil, taskapp.FileView{}, err
	}
	if s.Tasks == nil {
		return nil, taskapp.FileView{}, fault.New("TASK_UNAVAILABLE", "创作资产文件读取尚未配置")
	}
	return s.Tasks.ForScope(s.scope).File(ctx, "project:"+item.ProjectID, item.TaskID, item.OutputID)
}
func (s *Service) Thumbnail(ctx context.Context, taskID, outputID string) ([]byte, error) {
	item, err := s.Detail(ctx, taskID, outputID)
	if err != nil {
		return nil, err
	}
	if item.Kind == "image" {
		if item.FileID == "" {
			return nil, fault.ErrNotFound
		}
		if s.Files == nil {
			return nil, fault.New("FILE_STORAGE_UNAVAILABLE", "创作资产缩略图读取尚未配置")
		}
		_, raw, err := s.Files.ReadContainerFile(ctx, item.SpaceID, item.FileID, true)
		return raw, err
	}
	if s.Tasks == nil {
		return nil, fault.New("TASK_UNAVAILABLE", "创作资产文件读取尚未配置")
	}
	return s.Tasks.ForScope(s.scope).Thumbnail(ctx, "project:"+item.ProjectID, item.TaskID, item.OutputID)
}
