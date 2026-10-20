package project

import (
	"context"
	"strings"

	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

type StoryboardRunView struct {
	ID              string             `json:"id"`
	Contract        string             `json:"contract"`
	Status          d.RunStatus        `json:"status"`
	Stage           string             `json:"stage"`
	SourceVersion   uint64             `json:"sourceVersion"`
	InputRevision   uint64             `json:"inputRevision"`
	Model           string             `json:"model"`
	CallCount       int                `json:"callCount"`
	Usage           billing.Usage      `json:"usage"`
	ErrorCode       string             `json:"errorCode,omitempty"`
	Error           string             `json:"error,omitempty"`
	StoryboardScope *d.StoryboardScope `json:"storyboardScope,omitempty"`
}

func storyboardRunView(r *d.Run) *StoryboardRunView {
	if r == nil {
		return nil
	}
	v := &StoryboardRunView{ID: r.ID, Contract: r.Contract, Status: r.Status, Stage: r.Stage, SourceVersion: r.SourceVersion, InputRevision: r.InputRevision, Model: r.Model, CallCount: len(r.Calls), Usage: billing.Usage{Known: true}, ErrorCode: r.ErrorCode, Error: r.Error, StoryboardScope: r.StoryboardScope}
	for _, c := range r.Calls {
		v.Usage.Input += c.Usage.Input
		v.Usage.Output += c.Usage.Output
		v.Usage.CacheHit += c.Usage.CacheHit
		v.Usage.Known = v.Usage.Known && c.Usage.Known
	}
	return v
}

type StoryboardAccess struct {
	Editable bool   `json:"editable"`
	CanStart bool   `json:"canStart"`
	Reason   string `json:"reason"`
}
type StoryboardBaseView struct {
	ProjectID     string             `json:"projectId"`
	Revision      uint64             `json:"revision"`
	SourceVersion uint64             `json:"sourceVersion"`
	Access        StoryboardAccess   `json:"access"`
	ActiveRun     *StoryboardRunView `json:"activeRun"`
}

func (s *Service) storyboardBase(p d.Project) StoryboardBaseView {
	v := StoryboardBaseView{ProjectID: p.ID, Revision: p.Revision, SourceVersion: p.ScriptSource.Version, Access: StoryboardAccess{}}
	if p.Active() {
		for i := range p.Runs {
			if p.Runs[i].Status == d.RunRunning {
				v.ActiveRun = storyboardRunView(&p.Runs[i])
				v.Access.Reason = "RUN_ACTIVE"
				return v
			}
		}
	}
	if e := s.storyboardReady(&p); e != nil {
		v.Access.Reason = errorCode(e)
		return v
	}
	v.Access.Editable, v.Access.CanStart = true, true
	if hasUnknownStoryboardCall(p) {
		v.Access.CanStart = false
		v.Access.Reason = "MODEL_RESULT_UNKNOWN"
	}
	return v
}

type EpisodeSummaryView struct {
	d.Episode
	CharacterCount     int                `json:"characterCount"`
	SceneCount         int                `json:"sceneCount"`
	ShotCount          int                `json:"shotCount"`
	PendingPromptCount int                `json:"pendingPromptCount"`
	LatestRun          *StoryboardRunView `json:"latestRun"`
}
type EpisodeIndexView struct {
	SourceVersion uint64               `json:"sourceVersion"`
	Segmentation  *string              `json:"segmentation"`
	SupportRanges []d.SourceRange      `json:"supportRanges"`
	Valid         bool                 `json:"valid"`
	Issues        []d.StoryboardIssue  `json:"issues"`
	Episodes      []EpisodeSummaryView `json:"episodes"`
}
type StoryboardIndexView struct {
	StoryboardBaseView
	EpisodeCount   *int               `json:"episodeCount"`
	Index          *EpisodeIndexView  `json:"index"`
	LatestIndexRun *StoryboardRunView `json:"latestIndexRun"`
}

func latestEpisodeRun(p *d.Project, id string) *d.Run {
	for i := len(p.Runs) - 1; i >= 0; i-- {
		r := &p.Runs[i]
		if r.Contract == d.StoryboardContract && r.SourceVersion == p.ScriptSource.Version && r.StoryboardScope != nil && r.StoryboardScope.EpisodeID == id {
			return r
		}
	}
	return nil
}
func indexIssues(p *d.Project) []d.StoryboardIssue {
	issues := []d.StoryboardIssue{}
	if p.ValidateEpisodeIndex(true) != nil {
		issues = append(issues, d.StoryboardIssue{Code: "EPISODE_INDEX_INVALID"})
	}
	if !p.IndexReady() {
		issues = append(issues, d.StoryboardIssue{Code: "RUN_INCOMPLETE"})
	}
	return issues
}
func (s *Service) StoryboardIndex(p d.Project) StoryboardIndexView {
	p.ScriptSource = p.ScriptSource.WithRangeIndex()
	v := StoryboardIndexView{StoryboardBaseView: s.storyboardBase(p), LatestIndexRun: storyboardRunView(p.LatestRunByContract(d.EpisodeIndexContract))}
	b := p.Storyboard
	if b == nil {
		return v
	}
	issues := indexIssues(&p)
	v.Index = &EpisodeIndexView{SourceVersion: b.SourceVersion, Segmentation: b.Index.Segmentation, SupportRanges: b.Index.SupportRanges, Valid: len(issues) == 0, Issues: issues, Episodes: []EpisodeSummaryView{}}
	if len(issues) == 0 && b.Index.Segmentation != nil && *b.Index.Segmentation == "episodes" {
		n := len(b.Index.Episodes)
		v.EpisodeCount = &n
	}
	for _, ep := range b.Index.Episodes {
		e := EpisodeSummaryView{Episode: ep, SceneCount: len(ep.SceneIDs), LatestRun: storyboardRunView(latestEpisodeRun(&p, ep.ID))}
		characters := map[string]bool{}
		for _, id := range ep.SceneIDs {
			sc := b.Scene(id)
			if sc == nil {
				continue
			}
			e.ShotCount += len(sc.ShotIDs)
			for _, id := range sc.ShotIDs {
				sh := b.Shot(id)
				if sh != nil {
					for _, ref := range sh.SelectedAssets {
						if asset := p.Asset(ref.AssetID); asset != nil && asset.Kind == "character" {
							characters[asset.ID] = true
						}
					}
				}
				if sh == nil || len(p.VideoPromptIssues(sh, "")) > 0 {
					e.PendingPromptCount++
				}
			}
		}
		e.CharacterCount = len(characters)
		v.Index.Episodes = append(v.Index.Episodes, e)
	}
	return v
}

type SceneView struct {
	d.Scene
	SceneNumber int                 `json:"sceneNumber"`
	Valid       bool                `json:"valid"`
	Issues      []d.StoryboardIssue `json:"issues"`
}
type SelectedAssetView struct {
	d.VisualTarget
	Label        string `json:"label"`
	Kind         string `json:"kind"`
	Available    bool   `json:"available"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
}
type PromptPartView struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	AssetID      string `json:"assetId,omitempty"`
	LookID       string `json:"lookId,omitempty"`
	Label        string `json:"label,omitempty"`
	Available    *bool  `json:"available,omitempty"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
}
type VideoPromptView struct {
	Parts     []PromptPartView    `json:"parts"`
	PlainText string              `json:"plainText"`
	Origin    string              `json:"origin"`
	ActionRef *d.ActionRef        `json:"actionRef,omitempty"`
	Valid     bool                `json:"valid"`
	Issues    []d.StoryboardIssue `json:"issues"`
}
type ShotView struct {
	d.Shot
	VideoTask              *ShotVideoTaskView  `json:"videoTask,omitempty"`
	PreviewDurationSeconds *float64            `json:"previewDurationSeconds,omitempty"`
	ThumbnailURL           string              `json:"thumbnailUrl,omitempty"`
	ShotNumber             int                 `json:"shotNumber"`
	SelectedAssets         []SelectedAssetView `json:"selectedAssets"`
	VideoPrompt            *VideoPromptView    `json:"videoPrompt"`
	Valid                  bool                `json:"valid"`
	Issues                 []d.StoryboardIssue `json:"issues"`
}

// ShotVideoTaskView 只投影原任务状态，不作为镜头聚合的第二份生成事实。
type ShotVideoTaskView struct {
	ID     string    `json:"id"`
	Status td.Status `json:"status"`
}
type EpisodeView struct {
	StoryboardBaseView
	Episode d.Episode   `json:"episode"`
	Scenes  []SceneView `json:"scenes"`
	Shots   []ShotView  `json:"shots"`
	Index   struct {
		SupportRanges []d.SourceRange     `json:"supportRanges"`
		Valid         bool                `json:"valid"`
		Issues        []d.StoryboardIssue `json:"issues"`
	} `json:"index"`
	LatestRun *StoryboardRunView `json:"latestRun"`
	PlainText string             `json:"plainText"`
}

func selectedAssetView(p *d.Project, t d.VisualTarget) SelectedAssetView {
	label, available := p.PromptLabel(t)
	v := SelectedAssetView{VisualTarget: t, Label: label, Available: available}
	if a := p.Asset(t.AssetID); a != nil {
		v.Kind = a.Kind
		file := a.ImageFileID
		if t.LookID != "" {
			file = ""
			for _, l := range a.Looks {
				if l.ID == t.LookID {
					file = l.ImageFileID
				}
			}
		}
		if file != "" {
			v.ThumbnailURL = "/api/files/" + file + "/thumbnail"
		}
	}
	return v
}
func (s *Service) StoryboardEpisode(p d.Project, id string) (EpisodeView, error) {
	p.ScriptSource = p.ScriptSource.WithRangeIndex()
	v := EpisodeView{StoryboardBaseView: s.storyboardBase(p), Scenes: []SceneView{}, Shots: []ShotView{}, LatestRun: storyboardRunView(latestEpisodeRun(&p, id))}
	b := p.Storyboard
	if b == nil || b.Episode(id) == nil {
		return v, fault.ErrNotFound
	}
	v.Episode = *b.Episode(id)
	v.Index.SupportRanges = append([]d.SourceRange{}, b.Index.SupportRanges...)
	v.Index.Issues = indexIssues(&p)
	v.Index.Valid = len(v.Index.Issues) == 0
	copies := []string{}
	for n, sceneID := range v.Episode.SceneIDs {
		sc := b.Scene(sceneID)
		// 引用一致性由 ApplyStoryboard 维护；悬挂引用与目录视图同样跳过，不渲染成 panic。
		if sc == nil {
			continue
		}
		issues := p.SceneIssues(sc)
		v.Scenes = append(v.Scenes, SceneView{*sc, n + 1, len(issues) == 0, issues})
		for n, shotID := range sc.ShotIDs {
			sh := b.Shot(shotID)
			if sh == nil {
				continue
			}
			issues, promptIssues := p.ShotAndVideoPromptIssues(sh, "")
			sv := ShotView{Shot: *sh, ShotNumber: n + 1, SelectedAssets: []SelectedAssetView{}, Valid: len(issues) == 0, Issues: issues}
			for _, t := range sh.SelectedAssets {
				sv.SelectedAssets = append(sv.SelectedAssets, selectedAssetView(&p, t))
			}
			if prompt := sh.VideoPrompt; prompt != nil {
				pi := promptIssues
				pv := &VideoPromptView{Parts: []PromptPartView{}, Origin: prompt.Origin, ActionRef: prompt.ActionRef, Valid: len(pi) == 0, Issues: pi}
				for _, part := range prompt.Parts {
					out := PromptPartView{Type: part.Type, Text: part.Text, AssetID: part.AssetID, LookID: part.LookID}
					if part.Type == "asset" {
						a := selectedAssetView(&p, d.VisualTarget{AssetID: part.AssetID, LookID: part.LookID})
						out.Label, out.Available, out.ThumbnailURL = a.Label, &a.Available, a.ThumbnailURL
						if !a.Available && part.LabelAtSave != "" {
							out.Label = part.LabelAtSave + "（已删除）"
						}
						pv.PlainText += "@" + out.Label
					} else {
						pv.PlainText += part.Text
					}
					pv.Parts = append(pv.Parts, out)
				}
				sv.VideoPrompt = pv
				copies = append(copies, pv.PlainText)
			}
			v.Shots = append(v.Shots, sv)
		}
	}
	v.PlainText = strings.Join(copies, "\n\n")
	return v, nil
}

type EpisodeSourceView struct {
	StoryboardBaseView
	EpisodeID     string             `json:"episodeId"`
	Ranges        []d.SourceRange    `json:"ranges"`
	Fragments     []d.SourceFragment `json:"fragments"`
	SupportRanges []d.SourceRange    `json:"supportRanges"`
}

func (s *Service) StoryboardSource(p d.Project, id string) (EpisodeSourceView, error) {
	v := EpisodeSourceView{StoryboardBaseView: s.storyboardBase(p), EpisodeID: id}
	b := p.Storyboard
	if b == nil || b.Episode(id) == nil {
		return v, fault.ErrNotFound
	}
	if b.SourceVersion != p.ScriptSource.Version {
		return v, fault.New("EPISODE_RANGE_STALE", "分集范围所属原文已变化")
	}
	v.Ranges = b.Episode(id).SourceRanges
	v.SupportRanges = b.Index.SupportRanges
	var e error
	v.Fragments, e = p.ScriptSource.Fragments(v.Ranges)
	if e != nil {
		return v, fault.New("EPISODE_RANGE_STALE", "分集原文范围已失效")
	}
	return v, nil
}
func (s *Service) ReadStoryboard(ctx context.Context, id string) (StoryboardIndexView, error) {
	p, e := s.ReadProject(ctx, id)
	if e != nil {
		return StoryboardIndexView{}, e
	}
	return s.StoryboardIndex(p), nil
}
