package project

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

type MediaTarget struct {
	AssetID   string `json:"assetId,omitempty"`
	LookID    string `json:"lookId,omitempty"`
	EpisodeID string `json:"episodeId,omitempty"`
	ShotID    string `json:"shotId,omitempty"`
}
type MediaCommand struct {
	AspectRatio  string           `json:"aspectRatio,omitempty"`
	ImageType    string           `json:"imageType,omitempty"`
	Options      *md.MediaOptions `json:"options,omitempty"`
	BillingBasis string           `json:"billingBasis,omitempty"`
	CommandID    string           `json:"commandId"`
	Revision     uint64           `json:"revision"`
	ModelID      string           `json:"modelId"`
	Target       MediaTarget      `json:"target"`
}
type MediaContext struct {
	FillMissingImage bool         `json:"fillMissingImage,omitempty"`
	Credit           *c.Charge    `json:"credit,omitempty"`
	Request          MediaCommand `json:"request"`
	SourceVersion    uint64       `json:"sourceVersion"`
	InputDigest      string       `json:"inputDigest"`
}
type MediaView struct {
	AspectRatio      string          `json:"aspectRatio"`
	ImageType        string          `json:"imageType"`
	FillMissingImage bool            `json:"fillMissingImage"`
	Options          md.MediaOptions `json:"options"`
	taskapp.View
	Credit          *MediaCreditView `json:"credit"`
	ProjectID       string           `json:"projectId"`
	Kind            string           `json:"kind"`
	Target          MediaTarget      `json:"target"`
	InputRevision   uint64           `json:"inputRevision"`
	SourceVersion   uint64           `json:"sourceVersion"`
	Stale           bool             `json:"stale"`
	AdoptedOutputID string           `json:"adoptedOutputId"`
}
type MediaListing struct {
	Items      []MediaView `json:"items"`
	NextCursor *string     `json:"nextCursor"`
}

func mediaScope(id string) string { return "project:" + id }
func (s *Service) mediaReady() error {
	if s.MediaTasks == nil {
		return fault.New("TASK_UNAVAILABLE", "媒体任务尚未配置")
	}
	return nil
}
func mediaInput(p *d.Project, in MediaCommand) (string, string, string, error) {
	t := in.Target
	if t.AssetID != "" && t.ShotID == "" && t.EpisodeID == "" {
		target := d.VisualTarget{AssetID: t.AssetID, LookID: t.LookID}
		if err := p.ValidateTarget(target); err != nil {
			return "", "", "", err
		}
		v := p.Prompt(target)
		if !p.ValidPrompt(v, "") {
			return "", "", "", fault.New("VISUAL_PROMPTS_REQUIRED", "请先保存有效的图片提示词")
		}
		style := ""
		if p.Style != nil {
			style = p.Style.Prompts.ZH
		}
		prompt := v.Text
		if style != "" {
			prompt += "\n\n" + style
		}
		ratio, err := mediaImageAspectRatio(p, t, in.ImageType, in.AspectRatio)
		if err != nil {
			return "", "", "", err
		}
		return "image", prompt, d.Digest([]any{p.ScriptSource.Version, target, prompt, ratio}), nil
	}
	if t.AssetID != "" || t.LookID != "" || t.ShotID == "" || t.EpisodeID == "" || p.Storyboard == nil {
		return "", "", "", fault.ErrInvalid
	}
	sh := p.Storyboard.Shot(t.ShotID)
	if sh == nil {
		return "", "", "", fault.ErrNotFound
	}
	sc := p.Storyboard.Scene(sh.SceneID)
	if sc == nil || sc.EpisodeID != t.EpisodeID {
		return "", "", "", fault.ErrNotFound
	}
	if sh.VideoPrompt == nil || len(p.VideoPromptIssues(sh, "")) > 0 {
		return "", "", "", fault.New("VIDEO_PROMPT_REQUIRED", "请先保存有效的镜头视频提示词")
	}
	var prompt strings.Builder
	for _, part := range sh.VideoPrompt.Parts {
		if part.Type == "text" {
			prompt.WriteString(part.Text)
		} else {
			target := d.VisualTarget{AssetID: part.AssetID, LookID: part.LookID}
			label, _ := p.PromptLabel(target)
			prompt.WriteString("@" + label)
		}
	}
	text := prompt.String()
	if p.Style != nil && p.Style.Prompts.ZH != "" {
		text += "\n\n" + p.Style.Prompts.ZH
	}
	return "video", text, d.Digest([]any{p.ScriptSource.Version, t, sh.VideoPrompt.Parts, text}), nil
}
func (s *Service) mediaView(ctx context.Context, p *d.Project, v taskapp.View) (MediaView, error) {
	snap, err := s.MediaTasks.Input(ctx, mediaScope(p.ID), v.ID)
	if err != nil {
		return MediaView{}, err
	}
	entries, err := s.mediaLedgerEntries(ctx, map[string]td.Snapshot{v.ID: snap})
	if err != nil {
		return MediaView{}, err
	}
	return s.mediaViewFromSnapshot(p, v, snap, entries)
}

func (s *Service) mediaViewFromSnapshot(p *d.Project, v taskapp.View, snap td.Snapshot, entries map[string]c.Entry) (MediaView, error) {
	var meta MediaContext
	if json.Unmarshal(snap.CallerContext, &meta) != nil {
		return MediaView{}, fault.ErrInvalid
	}
	kind, _, digest, e := mediaInput(p, meta.Request)
	if kind == "" {
		kind = strings.TrimSuffix(v.Operation, ".generate")
	}
	options := md.MediaOptions{}
	// 默认值在受理时已解析进不可变运输输入；原始命令保持原样供重放，含被省略的选项。
	if json.Unmarshal(snap.Input, &options) != nil {
		return MediaView{}, fault.ErrInvalid
	}
	var frozen struct {
		AspectRatio string `json:"aspectRatio"`
	}
	if json.Unmarshal(snap.Input, &frozen) != nil {
		return MediaView{}, fault.ErrInvalid
	}
	out := MediaView{AspectRatio: frozen.AspectRatio, ImageType: meta.Request.ImageType, Options: options, View: v, ProjectID: p.ID, Kind: kind, Target: meta.Request.Target, InputRevision: meta.Request.Revision, SourceVersion: meta.SourceVersion, Stale: e != nil || digest != meta.InputDigest}
	out.FillMissingImage = meta.FillMissingImage
	var err error
	out.Credit, err = mediaCredit(p.OwnerID, p.ID, meta.Credit, v.Status, entries)
	if err != nil {
		return MediaView{}, err
	}
	if v.Result != nil {
		for _, f := range v.Result.Files {
			if kind == "image" {
				target := d.VisualTarget{AssetID: meta.Request.Target.AssetID, LookID: meta.Request.Target.LookID}
				a := p.Asset(target.AssetID)
				if a != nil {
					file := a.ImageFileID
					if target.LookID != "" {
						file = ""
						for _, l := range a.Looks {
							if l.ID == target.LookID {
								file = l.ImageFileID
							}
						}
					}
					if f.FileID != "" && file == f.FileID {
						out.AdoptedOutputID = f.ID
					}
				}
			}
			if kind == "video" {
				for _, selection := range p.MediaSelections {
					if selection.ShotID == meta.Request.Target.ShotID && selection.TaskID == v.ID && selection.OutputID == f.ID {
						out.AdoptedOutputID = f.ID
					}
				}
			}
		}
	}
	return out, nil
}
func (s *Service) StartMedia(ctx context.Context, id string, in MediaCommand) (MediaView, error) {
	if err := s.mediaReady(); err != nil {
		return MediaView{}, err
	}
	p, err := s.Project(ctx, id)
	if err != nil {
		return MediaView{}, err
	}
	return s.startMedia(ctx, p, in, false)
}

// 单项和批量共用输入、计费、幂等及独立任务受理；批量使用同一已核对快照，
// 避免本批先完成的补图推进revision，误拒绝仍基于相同材料的其他目标。
func (s *Service) startMedia(ctx context.Context, p d.Project, in MediaCommand, fillMissing bool) (MediaView, error) {
	id := p.ID
	old, err := s.MediaTasks.GetByCommand(ctx, mediaScope(id), in.CommandID)
	if err == nil {
		snap, e := s.MediaTasks.Input(ctx, mediaScope(id), old.ID)
		if e != nil {
			return MediaView{}, e
		}
		var meta MediaContext
		if json.Unmarshal(snap.CallerContext, &meta) != nil || d.Digest(meta.Request) != d.Digest(in) || meta.FillMissingImage != fillMissing {
			return MediaView{}, fault.New("COMMAND_ID_CONFLICT", "同一命令对应不同请求")
		}
		return s.mediaView(ctx, &p, old)
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return MediaView{}, err
	}
	if p.Revision != in.Revision {
		return MediaView{}, fault.ErrConflict
	}
	if in.Target.AssetID != "" {
		if e := p.ValidateImageTarget(d.VisualTarget{AssetID: in.Target.AssetID, LookID: in.Target.LookID}); e != nil {
			return MediaView{}, e
		}
		if fillMissing && imageFileID(p, in.Target) != "" {
			return MediaView{}, fault.New("ASSET_IMAGE_EXISTS", "资产已有图片，未重复生成")
		}
	} else if p.Active() {
		return MediaView{}, fault.New("RUN_ACTIVE", "本集拆解或提示词任务尚在运行")
	}
	kind, prompt, digest, err := mediaInput(&p, in)
	if err != nil {
		return MediaView{}, err
	}
	composition, err := characterImageComposition(&p, in.Target, in.ImageType)
	if err != nil {
		return MediaView{}, err
	}
	if composition != "" {
		prompt += "\n\n" + composition
	}
	release, err := s.configurationGuard(ctx)
	if err != nil {
		return MediaView{}, err
	}
	defer release.Release()
	ctx = release.Context
	charge, resolvedOptions, err := s.freezeMediaCredit(ctx, p, in)
	if err != nil {
		return MediaView{}, err
	}
	ratio, err := mediaImageAspectRatio(&p, in.Target, in.ImageType, in.AspectRatio)
	if err != nil {
		return MediaView{}, err
	}
	options, e := mediaOptions(in.ModelID, resolvedOptions)
	if charge == nil {
		options, e = mediaOptions(in.ModelID, in.Options)
	}
	if e != nil {
		return MediaView{}, e
	}
	input, _ := json.Marshal(struct {
		Prompt          string  `json:"prompt"`
		AspectRatio     string  `json:"aspectRatio"`
		Resolution      string  `json:"resolution"`
		DurationSeconds float64 `json:"durationSeconds,omitempty"`
	}{prompt, ratio, options.Resolution, options.DurationSeconds})
	meta, _ := json.Marshal(MediaContext{FillMissingImage: fillMissing, Credit: charge, Request: in, SourceVersion: p.ScriptSource.Version, InputDigest: digest})
	tasks := s.MediaTasks
	if p.OwnerID != "" {
		// 归属已由项目存储确立；即使是受信内部调用，也显式带入独立任务。
		tasks = tasks.ForOwner(p.OwnerID)
	}
	v, err := tasks.Submit(ctx, taskapp.Command{RequirePermit: charge != nil, ScopeID: mediaScope(id), CommandID: in.CommandID, Operation: kind + ".generate", ModelID: in.ModelID, Input: input, CallerContext: meta, ActiveKey: d.Digest(in.Target)})
	if err != nil {
		var f *fault.Error
		if errors.As(err, &f) && f.Code == "COMMAND_ID_CONFLICT" {
			// 并发相同启动可能冻结不同时间戳；重放只以被受理的不可变请求为准，不用落败快照。
			old, e := s.MediaTasks.GetByCommand(ctx, mediaScope(id), in.CommandID)
			if e == nil {
				snap, e := s.MediaTasks.Input(ctx, mediaScope(id), old.ID)
				var accepted MediaContext
				if e == nil && json.Unmarshal(snap.CallerContext, &accepted) == nil && d.Digest(accepted.Request) == d.Digest(in) && accepted.FillMissingImage == fillMissing {
					return s.mediaView(ctx, &p, old)
				}
			}
		}
		return MediaView{}, err
	}
	return s.mediaView(ctx, &p, v)
}

// 读模型省去完整模型原答；生成、采用和取消的写入仍沿完整 Project/Snapshot。
func (s *Service) mediaProject(ctx context.Context, id string) (d.Project, error) {
	if reader, ok := s.Store.(ports.ProjectMediaReader); ok {
		p, err := reader.MediaProject(ctx, id)
		if err == nil && p.DeletedAt != nil {
			err = fault.ErrNotFound
		}
		return p, err
	}
	return s.Project(ctx, id)
}

func (s *Service) Media(ctx context.Context, id, taskID string) (MediaView, error) {
	if err := s.mediaReady(); err != nil {
		return MediaView{}, err
	}
	p, err := s.mediaProject(ctx, id)
	if err != nil {
		return MediaView{}, err
	}
	v, err := s.MediaTasks.Get(ctx, mediaScope(id), taskID)
	if err != nil {
		return MediaView{}, err
	}
	return s.mediaView(ctx, &p, v)
}
func (s *Service) ListMedia(ctx context.Context, id, cursor string, limit int, kind string) (MediaListing, error) {
	operation := ""
	if kind != "" {
		if kind != "image" && kind != "video" {
			return MediaListing{}, fault.ErrInvalid
		}
		operation = kind + ".generate"
	}
	if err := s.mediaReady(); err != nil {
		return MediaListing{}, err
	}
	p, err := s.mediaProject(ctx, id)
	if err != nil {
		return MediaListing{}, err
	}
	list, err := s.MediaTasks.List(ctx, mediaScope(id), operation, cursor, limit)
	if err != nil {
		return MediaListing{}, err
	}
	out := MediaListing{Items: []MediaView{}, NextCursor: list.NextCursor}
	ids := make([]string, 0, len(list.Items))
	for _, v := range list.Items {
		ids = append(ids, v.ID)
	}
	snapshots, err := s.MediaTasks.Inputs(ctx, mediaScope(id), ids)
	if err != nil {
		return out, err
	}
	entries, err := s.mediaLedgerEntries(ctx, snapshots)
	if err != nil {
		return out, err
	}
	for _, v := range list.Items {
		item, e := s.mediaViewFromSnapshot(&p, v, snapshots[v.ID], entries)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
func (s *Service) CancelMedia(ctx context.Context, id, taskID string, revision uint64) (MediaView, error) {
	if _, err := s.Media(ctx, id, taskID); err != nil {
		return MediaView{}, err
	}
	v, err := s.MediaTasks.Cancel(ctx, mediaScope(id), taskID, revision)
	if err != nil {
		return MediaView{}, err
	}
	p, err := s.Project(ctx, id)
	if err != nil {
		return MediaView{}, err
	}
	return s.mediaView(ctx, &p, v)
}
func (s *Service) MediaFile(ctx context.Context, id, taskID, output string) ([]byte, taskapp.FileView, error) {
	if _, err := s.Media(ctx, id, taskID); err != nil {
		return nil, taskapp.FileView{}, err
	}
	return s.MediaTasks.File(ctx, mediaScope(id), taskID, output)
}
func (s *Service) AdoptMedia(ctx context.Context, id, taskID, output string, revision uint64) (d.Project, error) {
	v, err := s.Media(ctx, id, taskID)
	if err != nil {
		return d.Project{}, err
	}
	if v.Status != td.Succeeded || v.Result == nil {
		return d.Project{}, fault.New("TASK_TERMINAL", "只能采用完整成功的媒体结果")
	}
	file := taskapp.FileView{}
	for _, f := range v.Result.Files {
		if f.ID == output {
			file = f
		}
	}
	if file.ID == "" {
		return d.Project{}, fault.ErrNotFound
	}
	p, err := s.Project(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Revision != revision {
		return p, fault.ErrConflict
	}
	snap, err := s.MediaTasks.Input(ctx, mediaScope(id), taskID)
	if err != nil {
		return p, err
	}
	var meta MediaContext
	if err = json.Unmarshal(snap.CallerContext, &meta); err != nil {
		return p, err
	}
	kind, _, digest, err := mediaInput(&p, meta.Request)
	if err != nil || digest != meta.InputDigest {
		return p, fault.New("MEDIA_OUTPUT_STALE", "生成输入已变化，历史结果保留，请重新生成")
	}
	if kind == "image" {
		return s.SaveImage(ctx, id, revision, d.VisualTarget{AssetID: v.Target.AssetID, LookID: v.Target.LookID}, file.FileID)
	}
	selection := d.MediaSelection{ShotID: v.Target.ShotID, TaskID: taskID, OutputID: output, InputDigest: digest}
	if err := p.AdoptMediaSelection(selection); err != nil {
		return p, err
	}
	return s.commit(ctx, p, revision)
}
