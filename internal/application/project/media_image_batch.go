package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

type ImageBatchItem struct {
	CommandID string
	Target    MediaTarget
	Task      *MediaView
	Err       error
}

func (s *Service) StartImageBatch(ctx context.Context, id string, commands []MediaCommand) ([]ImageBatchItem, error) {
	if len(commands) == 0 || len(commands) > 200 {
		return nil, fault.ErrInvalid
	}
	ids, targets := map[string]bool{}, map[string]bool{}
	for _, in := range commands {
		key := d.Digest(in.Target)
		if in.CommandID == "" || in.Revision == 0 || ids[in.CommandID] || targets[key] || in.Target.AssetID == "" || in.Target.EpisodeID != "" || in.Target.ShotID != "" {
			return nil, fault.ErrInvalid
		}
		ids[in.CommandID], targets[key] = true, true
	}
	if err := s.mediaReady(); err != nil {
		return nil, err
	}
	p, err := s.editingProject(ctx, id)
	if err != nil {
		return nil, err
	}
	items := make([]ImageBatchItem, 0, len(commands))
	for _, in := range commands {
		if err := ctx.Err(); err != nil {
			// 已受理项仍由原task推进；请求中断不能虚构剩余目标已受理。
			return nil, err
		}
		v, err := s.startMedia(ctx, p, in, true)
		item := ImageBatchItem{CommandID: in.CommandID, Target: in.Target, Err: err}
		if err == nil {
			item.Task = &v
		}
		items = append(items, item)
	}
	return items, nil
}

func imageFileID(p d.Project, target MediaTarget) string {
	asset := p.Asset(target.AssetID)
	if asset == nil {
		return ""
	}
	if target.LookID == "" {
		return asset.ImageFileID
	}
	for _, look := range asset.Looks {
		if look.ID == target.LookID {
			return look.ImageFileID
		}
	}
	return ""
}

// RecoverImageFills 根据原任务冻结的授权补空位；文件关联本身即完成事实。
// 多节点或上传竞争只会产生CAS冲突，下一轮必须重新核对空位和输入依赖。
func (s *Service) RecoverImageFills(ctx context.Context) error {
	queries, ok := s.Store.(ports.MediaImageFills)
	if !ok || s.MediaTasks == nil || s.Files == nil {
		return nil
	}
	var failures []error
	after := ""
	for {
		items, err := queries.PendingImageFills(ctx, after, 100)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, item := range items {
			after = item.TaskID
			if err := s.fillGeneratedImage(ctx, item); err != nil && !errors.Is(err, fault.ErrConflict) {
				failures = append(failures, fmt.Errorf("fill image task %s: %w", item.TaskID, err))
			}
		}
		if len(items) < 100 {
			return errors.Join(failures...)
		}
	}
}

func (s *Service) fillGeneratedImage(ctx context.Context, item ports.MediaImageFill) error {
	owned := s.forSpace(item.SpaceID)
	p, err := owned.Project(ctx, item.ProjectID)
	if errors.Is(err, fault.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	task, err := owned.MediaTasks.Get(ctx, mediaScope(p.ID), item.TaskID)
	if err != nil {
		return err
	}
	if task.Status != td.Succeeded || task.Result == nil {
		return nil
	}
	snapshot, err := owned.MediaTasks.Input(ctx, mediaScope(p.ID), task.ID)
	if err != nil {
		return err
	}
	var meta MediaContext
	if err = json.Unmarshal(snapshot.CallerContext, &meta); err != nil {
		return err
	}
	if !meta.FillMissingImage || meta.Request.Target.AssetID == "" || imageFileID(p, meta.Request.Target) != "" {
		return nil
	}
	kind, _, digest, err := mediaInput(&p, meta.Request)
	if err != nil || kind != "image" || digest != meta.InputDigest || p.ScriptSource.Version != meta.SourceVersion {
		return nil // 已变更/删除的目标保留候选，不以旧输入补图。
	}
	for _, file := range task.Result.Files {
		if file.FileID != "" {
			_, err = owned.SaveImage(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: meta.Request.Target.AssetID, LookID: meta.Request.Target.LookID}, file.FileID)
			return err
		}
	}
	return fault.New("INVALID_ASSET_IMAGE", "成功图片任务缺少已发布文件")
}
