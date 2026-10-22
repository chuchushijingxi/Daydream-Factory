package project

import (
	"context"
	"time"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

type CreationChoices struct {
	Settings      *d.ProjectSettings
	StyleID       string
	PromptVersion string
}

func (s *Service) Create(ctx context.Context, title, filename string, raw []byte, limit int64) (d.Project, error) {
	return s.CreateWithChoices(ctx, title, filename, raw, limit, CreationChoices{})
}

// 创建前先完整校验项目选项与原文，一次 Create 提交。
func (s *Service) CreateWithChoices(ctx context.Context, title, filename string, raw []byte, limit int64, choices CreationChoices) (d.Project, error) {
	return s.createFromSource(ctx, title, filename, raw, "", limit, choices)
}

func (s *Service) CreateFromFile(ctx context.Context, title, fileID string, limit int64, choices CreationChoices) (d.Project, error) {
	if s.Files == nil {
		return d.Project{}, fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
	}
	file, raw, err := s.Files.ReadOriginal(ctx, fileID)
	if err != nil {
		return d.Project{}, err
	}
	scope, err := s.resolveSpace(ctx)
	if err != nil {
		return d.Project{}, err
	}
	if file.SpaceID != scope.SpaceID {
		return d.Project{}, fault.ErrNotFound
	}
	return s.createFromSource(ctx, title, file.Filename, raw, file.ID, limit, choices)
}

func (s *Service) createFromSource(ctx context.Context, title, filename string, raw []byte, fileID string, limit int64, choices CreationChoices) (d.Project, error) {
	if err := ctx.Err(); err != nil {
		return d.Project{}, err
	}
	if limit < 0 {
		return d.Project{}, fault.ErrInvalid
	}
	source, err := s.Importer.Parse(filename, raw, 1)
	if err != nil {
		return d.Project{}, err
	}
	scope, err := s.resolveSpace(ctx)
	if err != nil {
		return d.Project{}, err
	}
	p := d.Project{SpaceID: scope.SpaceID, OwnerID: scope.UserID, CreatorID: scope.UserID, ID: identity.New("project"), Title: title, Revision: 1, ScriptSource: source, Assets: []d.Asset{}, Runs: []d.Run{}, VisualPrompts: []d.VisualPrompt{}, LimitMicroCNY: limit}
	if choices.Settings != nil || choices.StyleID != "" || choices.PromptVersion != "" {
		release, err := s.configurationGuard(ctx)
		if err != nil {
			return p, err
		}
		defer release.Release()
		ctx = release.Context
		if choices.Settings != nil {
			if err = s.validateSettings(ctx, *choices.Settings, nil); err != nil {
				return p, err
			}
			value := *choices.Settings
			p.ProjectSettings = &value
		}
		if choices.StyleID != "" || choices.PromptVersion != "" {
			for _, style := range s.Styles {
				if style.ID != choices.StyleID {
					continue
				}
				if style.PromptVersion != choices.PromptVersion {
					return p, fault.New("STYLE_VERSION_CONFLICT", "风格库版本已变化")
				}
				value := style.ProjectStyle
				p.Style = &value
			}
			if p.Style == nil {
				return p, fault.ErrInvalid
			}
		}
	}
	uploadedAt := time.Now().UTC()
	p.UploadedAt = &uploadedAt
	p.SourceFileID = fileID
	// 直连字节构造仅供隔离的应用层夹具使用；HTTP 只从已持久化的上传 ID
	// 创建项目，从不复制字节。
	if fileID == "" {
		p.Original = append([]byte(nil), raw...)
	}
	if err = ctx.Err(); err != nil {
		return p, err
	}
	if fileID != "" {
		p.Storyboard = d.NewStoryboard(source.Version)
		s.initialEpisodeIndex(ctx, &p)
		p.LatestRun().ContinueAssetPreparation = true
		if p.LatestRun().Credit == nil && (p.LatestRun().ErrorCode == "BILLING_BASIS_CHANGED" || p.LatestRun().ErrorCode == "MODEL_PRICE_UNAVAILABLE") {
			return p, fault.New(p.LatestRun().ErrorCode, p.LatestRun().Error)
		}
	}
	if err = s.Store.Create(ctx, p); err != nil {
		return p, err
	}
	return p, nil
}

func (s *Service) Upload(ctx context.Context, id string, revision uint64, filename string, raw []byte) (d.Project, error) {
	return s.replaceSource(ctx, id, revision, func() (string, []byte, string, error) { return filename, raw, "", nil })
}

func (s *Service) UploadFromFile(ctx context.Context, id string, revision uint64, fileID string) (d.Project, error) {
	return s.replaceSource(ctx, id, revision, func() (string, []byte, string, error) {
		if s.Files == nil {
			return "", nil, "", fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
		}
		file, raw, err := s.Files.ReadOriginal(ctx, fileID)
		if err == nil {
			p, readErr := s.editingProject(ctx, id)
			if readErr != nil {
				return "", nil, "", readErr
			}
			if file.SpaceID != p.SpaceID {
				return "", nil, "", fault.ErrNotFound
			}
		}
		return file.Filename, raw, file.ID, err
	})
}

func (s *Service) replaceSource(ctx context.Context, id string, revision uint64, load func() (string, []byte, string, error)) (d.Project, error) {
	p, err := s.editingProject(ctx, id)
	if err != nil {
		return p, err
	}
	if err = commandBoundary(&p, revision); err != nil {
		return p, err
	}
	if p.CatalogEditedByUser {
		return p, fault.New("CATALOG_USER_EDITED", "用户管理的目录不能被换源覆盖")
	}
	catalog := p.LatestCatalogRun()
	// 合并任务进入视觉阶段仍保留原合同；换源保护按所处阶段判断，
	// 与独立视觉任务同权。
	if p.LatestRunByContract(d.OutlineContract) != nil || p.LatestRunByContract(d.VisualContract) != nil || catalog != nil && catalog.ExecutionContract() == d.VisualContract {
		return p, fault.New("SOURCE_LOCKED", "大纲或视觉任务启动后原文固定")
	}
	if err = p.EnsureNotFailed(); err != nil {
		return p, err
	}
	if p.HasCancelledTask() {
		return p, fault.New("ASSETS_NOT_READY", "任务已取消")
	}
	filename, raw, fileID, err := load()
	if err != nil {
		return p, err
	}
	source, err := s.Importer.Parse(filename, raw, p.ScriptSource.Version+1)
	if err != nil {
		return p, err
	}
	p.ScriptSource = source
	p.SourceFileID, p.OriginalFile, p.Original = fileID, nil, nil
	if fileID == "" {
		p.Original = append([]byte(nil), raw...)
	}
	p.Assets = []d.Asset{}
	p.VisualPrompts = []d.VisualPrompt{}
	if p.Storyboard != nil {
		release, guardErr := s.configurationGuard(ctx)
		if guardErr != nil {
			return p, guardErr
		}
		defer release.Release()
		ctx = release.Context
		continueAssets := false
		for i := len(p.Runs) - 1; i >= 0; i-- {
			if p.Runs[i].Contract == d.EpisodeIndexContract {
				continueAssets = p.Runs[i].ContinueAssetPreparation
				break
			}
		}
		s.initialEpisodeIndex(ctx, &p)
		if p.LatestRun().ErrorCode == "BILLING_BASIS_CHANGED" || p.LatestRun().ErrorCode == "MODEL_PRICE_UNAVAILABLE" {
			return p, fault.New(p.LatestRun().ErrorCode, p.LatestRun().Error)
		}
		p.LatestRun().ContinueAssetPreparation = continueAssets
	}
	if err = ctx.Err(); err != nil {
		return p, err
	}
	return s.commit(ctx, p, revision)
}
