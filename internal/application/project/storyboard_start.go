package project

import (
	"context"
	"errors"
	"strings"
	"time"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
)

func hasUnknown(r *d.Run) bool {
	if r == nil {
		return false
	}
	for _, c := range r.Calls {
		if c.Status == "pending" || c.Status == "unknown" && !interruptedCall(c) {
			return true
		}
	}
	return false
}
func (s *Service) initialEpisodeIndex(ctx context.Context, p *d.Project) {
	r := d.Run{ID: identity.New("run"), Contract: d.EpisodeIndexContract, SourceVersion: p.ScriptSource.Version, InputRevision: p.Revision, Status: d.RunRunning, Stage: "episodeIndex", Calls: []d.Call{}, StoryboardScope: &d.StoryboardScope{Operation: "index"}, CreatedAt: time.Now().UTC()}
	if e := s.prepareRun(ctx, *p, &r); e != nil {
		r.Status = d.RunFailed
		ended := time.Now().UTC()
		r.EndedAt = &ended
		r.ErrorCode = errorCode(e)
		if errors.Is(e, fault.ErrModelUnavailable) {
			r.ErrorCode = "MODEL_UNAVAILABLE"
		}
		if errors.Is(e, fault.ErrBudget) {
			r.ErrorCode = "BUDGET_EXHAUSTED"
		}
		r.Error = e.Error()
	}
	p.Runs = append(p.Runs, r)
}
func (s *Service) RetryEpisodeIndex(ctx context.Context, id string, revision uint64) (d.Project, error) {
	p, e := s.Project(ctx, id)
	if e != nil {
		return p, e
	}
	if e = commandBoundary(&p, revision); e != nil {
		return p, e
	}
	old := p.LatestRunByContract(d.EpisodeIndexContract)
	if p.Storyboard == nil || old == nil || old.Status != d.RunFailed && old.Status != d.RunCancelled {
		return p, fault.New("EPISODE_INDEX_NOT_RETRYABLE", "只有明确失败或取消的当前源索引可以重试")
	}
	if hasUnknown(old) {
		return p, fault.New("MODEL_RESULT_UNKNOWN", "已有未知请求，不能重发")
	}
	release, e := s.configurationGuard(ctx)
	if e != nil {
		return p, e
	}
	defer release.Release()
	ctx = release.Context
	s.initialEpisodeIndex(ctx, &p)
	if p.OwnerID != "" && (p.LatestRun().ErrorCode == "BILLING_BASIS_CHANGED" || p.LatestRun().ErrorCode == "MODEL_PRICE_UNAVAILABLE") {
		return p, fault.New(p.LatestRun().ErrorCode, p.LatestRun().Error)
	}
	p.LatestRun().ContinueAssetPreparation = old.ContinueAssetPreparation
	return s.commit(ctx, p, revision)
}

// 与索引结束回执同次提交下一项任务；Workers 退出旧会话后执行它。
// 没有浏览器也能接续/恢复，取消及失败不会进入这里，旧项目不补跑。
func (s *Service) continueAssetPreparation(ctx context.Context, p *d.Project) {
	index := p.LatestRun()
	if index == nil || index.Contract != d.EpisodeIndexContract || index.Status != d.RunCompleted || !index.ContinueAssetPreparation || p.LatestCatalogRun() != nil {
		return
	}
	r := d.Run{Credit: index.Credit, ID: identity.New("run"), Contract: d.CatalogExtractionContract, SourceVersion: p.ScriptSource.Version, InputRevision: p.Revision + 1, Status: d.RunRunning, Stage: "extract", Calls: []d.Call{}, CreatedAt: time.Now().UTC()}
	if e := s.prepareRun(ctx, *p, &r); e != nil {
		r.Status, r.ErrorCode, r.Error = d.RunFailed, errorCode(e), e.Error()
		ended := time.Now().UTC()
		r.EndedAt = &ended
		if errors.Is(e, fault.ErrModelUnavailable) {
			r.ErrorCode = "MODEL_UNAVAILABLE"
		}
		if errors.Is(e, fault.ErrBudget) {
			r.ErrorCode = "BUDGET_EXHAUSTED"
		}
	}
	p.Runs = append(p.Runs, r)
}
func (s *Service) storyboardReady(p *d.Project) error {
	if !p.IndexReady() {
		return fault.New("EPISODE_INDEX_REQUIRED", "解析中的分集识别尚未完整完成")
	}
	if e := p.EnsureNotFailed(); e != nil {
		return e
	}
	if p.HasCancelledTask() || !p.CatalogReady() {
		return fault.New("ASSETS_NOT_READY", "当前源资产生成尚未完成")
	}
	catalog := p.LatestCatalogRun()
	if catalog == nil || catalog.Status != d.RunCompleted || len(p.Assets) == 0 || len(p.PendingVisualTargets()) != 0 {
		return fault.New("ASSETS_NOT_READY", "请先完成资产及全部视觉正文")
	}
	return nil
}
func (s *Service) StartEpisode(ctx context.Context, id, episodeID string, in EpisodeRunInput) (d.Project, error) {
	// Service callers use the exact HTTP union as well.
	raw := mustJSON(in)
	var checked EpisodeRunInput
	if e := DecodeStoryboard(raw, "EpisodeRunInput", &checked); e != nil {
		return d.Project{}, e
	}
	release, e := s.configurationGuard(ctx)
	if e != nil {
		return d.Project{}, e
	}
	defer release.Release()
	ctx = release.Context
	p, e := s.Project(ctx, id)
	if e != nil {
		return p, e
	}
	if e = commandBoundary(&p, in.Revision); e != nil {
		return p, e
	}
	if e = s.storyboardReady(&p); e != nil {
		return p, e
	}
	ep := p.Storyboard.Episode(episodeID)
	if ep == nil {
		return p, fault.ErrNotFound
	}
	if hasUnknownStoryboardCall(p) {
		return p, fault.New("MODEL_RESULT_UNKNOWN", "项目存在未知分集拆解调用，不重新提交")
	}
	scope, episodeShots, e := storyboardPromptScope(&p, episodeID, in.Operation, in.ShotIDs)
	if e != nil {
		return p, e
	}
	if e = s.freezeBreakdownOptions(ctx, p, scope, in.BreakdownMode); e != nil {
		return p, e
	}
	if in.Operation != "breakdown" && len(episodeShots) == 0 {
		return p, fault.New("STORYBOARD_REQUIRED", "本集尚无可写正文的镜头")
	}
	if in.Operation != "breakdown" && len(scope.ShotIDs) == 0 {
		return p, fault.New("STORYBOARD_UP_TO_DATE", "当前没有缺失或失效的镜头正文")
	}
	r := d.Run{ID: identity.New("run"), Contract: d.StoryboardContract, SourceVersion: p.ScriptSource.Version, InputRevision: in.Revision, Status: d.RunRunning, Stage: "storyboard", Calls: []d.Call{}, StoryboardScope: scope, CreatedAt: time.Now().UTC()}
	if e = s.prepareRun(ctx, p, &r); e != nil {
		return p, e
	}
	p.Runs = append(p.Runs, r)
	return s.commit(ctx, p, in.Revision)
}
func (s *Service) ChangeStoryboard(ctx context.Context, id string, raw []byte) (d.StoryboardReceipt, error) {
	var in StoryboardChangeInput
	if e := DecodeStoryboard(raw, "ChangeInput", &in); e != nil {
		return d.StoryboardReceipt{}, e
	}
	p, e := s.Project(ctx, id)
	if e != nil {
		return d.StoryboardReceipt{}, e
	}
	if e = commandBoundary(&p, in.Revision); e != nil {
		return d.StoryboardReceipt{}, e
	}
	needsContent := false
	for _, op := range in.Operations {
		needsContent = needsContent || !(strings.HasSuffix(op.Op, "Episode") || op.Op == "setSegmentation" || op.Op == "setSupportRanges")
	}
	if !p.IndexReady() {
		return d.StoryboardReceipt{}, fault.New("EPISODE_INDEX_REQUIRED", "普通修改需要同源完整分集目录")
	}
	if needsContent {
		if e = s.storyboardReady(&p); e != nil {
			return d.StoryboardReceipt{}, e
		}
	}
	receipt, e := p.ApplyStoryboard(in.Operations, d.StoryboardWrite{})
	if e != nil {
		return receipt, e
	}
	if len(receipt.Changes) == 0 {
		return receipt, nil
	}
	_, e = s.commit(ctx, p, in.Revision)
	return receipt, e
}

func hasUnknownStoryboardCall(p d.Project) bool {
	for i := range p.Runs {
		r := &p.Runs[i]
		if isStoryboard(r.Contract) && hasUnknown(r) {
			return true
		}
	}
	return false
}

// storyboardPromptScope 冻结一集操作的镜头范围：breakdown 覆盖全本集镜头，
// fillPrompts 只含正文缺失或失效的镜头，regeneratePrompts 限本集显式清单且不重复。
// 报价预览与实际启动共用同一构造，两侧的 EpisodeRangeDigest 与 ShotIDs 才保证一致；
// 返回的本集镜头集合供启动侧判断是否有可写与待写正文。
func storyboardPromptScope(p *d.Project, episodeID, operation string, shotIDs []string) (*d.StoryboardScope, map[string]bool, error) {
	ep := p.Storyboard.Episode(episodeID)
	if ep == nil {
		return nil, nil, fault.ErrNotFound
	}
	scope := &d.StoryboardScope{Operation: operation, EpisodeID: ep.ID, ShotIDs: []string{}, EpisodeRangeDigest: d.Digest(ep.SourceRanges)}
	episodeShots := map[string]bool{}
	for _, sceneID := range ep.SceneIDs {
		sc := p.Storyboard.Scene(sceneID)
		for _, id := range sc.ShotIDs {
			episodeShots[id] = true
			needed := operation == "breakdown"
			if !needed && operation == "fillPrompts" {
				needed = len(p.VideoPromptIssues(p.Storyboard.Shot(id), "")) > 0
			}
			if needed {
				scope.ShotIDs = append(scope.ShotIDs, id)
			}
		}
	}
	if operation == "regeneratePrompts" {
		seen := map[string]bool{}
		for _, id := range shotIDs {
			if !episodeShots[id] {
				return nil, nil, fault.ErrNotFound
			}
			if seen[id] {
				return nil, nil, fault.ErrInvalid
			}
			seen[id] = true
			scope.ShotIDs = append(scope.ShotIDs, id)
		}
	}
	return scope, episodeShots, nil
}
