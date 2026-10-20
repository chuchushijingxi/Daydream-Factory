package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"drama-agent/internal/domain/fault"
)

const EpisodeIndexContract = "episode-index-agent-v1"
const StoryboardContract = "storyboard-agent-v1"

type SourcePoint struct {
	SpanID string `json:"spanId"`
	Offset int    `json:"offset"`
}
type SourceRange struct {
	Start SourcePoint `json:"start"`
	End   SourcePoint `json:"end"`
}
type SourceFragment struct {
	SpanID      string `json:"spanId"`
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
	Text        string `json:"text"`
}
type StoryboardScope struct {
	Operation          string                     `json:"operation"`
	EpisodeID          string                     `json:"episodeId,omitempty"`
	ShotIDs            []string                   `json:"shotIds,omitempty"`
	EpisodeRangeDigest string                     `json:"episodeRangeDigest,omitempty"`
	BreakdownMode      string                     `json:"breakdownMode,omitempty"`
	VideoConstraint    *StoryboardVideoConstraint `json:"videoConstraint,omitempty"`
}

// 已受理任务使用自己的秒数上限；重启或运营调整模型不改变生成及复核依据。
type StoryboardVideoConstraint struct {
	ModelID            string  `json:"modelId"`
	ModelName          string  `json:"modelName"`
	MaxDurationSeconds float64 `json:"maxDurationSeconds"`
}
type Storyboard struct {
	SourceVersion uint64       `json:"sourceVersion"`
	Index         EpisodeIndex `json:"index"`
	Scenes        []Scene      `json:"scenes"`
	Shots         []Shot       `json:"shots"`
}
type EpisodeIndex struct {
	Segmentation  *string       `json:"segmentation"`
	SupportRanges []SourceRange `json:"supportRanges"`
	Episodes      []Episode     `json:"episodes"`
	Origin        string        `json:"origin"`
	ActionRef     *ActionRef    `json:"actionRef,omitempty"`
}
type EpisodeValue struct {
	EpisodeNumber *int          `json:"episodeNumber"`
	Title         string        `json:"title"`
	SourceRanges  []SourceRange `json:"sourceRanges"`
}
type Episode struct {
	EpisodeValue
	ID        string     `json:"id"`
	SceneIDs  []string   `json:"sceneIds"`
	Origin    string     `json:"origin"`
	ActionRef *ActionRef `json:"actionRef,omitempty"`
}
type SceneValue struct {
	Title           string        `json:"title"`
	SourceRanges    []SourceRange `json:"sourceRanges"`
	LocationAssetID *string       `json:"locationAssetId"`
	Time            string        `json:"time"`
	Summary         string        `json:"summary"`
}
type SceneBasis struct {
	SourceVersion      uint64 `json:"sourceVersion"`
	EpisodeRangeDigest string `json:"episodeRangeDigest"`
}
type Scene struct {
	SceneValue
	ID        string     `json:"id"`
	EpisodeID string     `json:"episodeId"`
	ShotIDs   []string   `json:"shotIds"`
	Basis     SceneBasis `json:"basis"`
	Origin    string     `json:"origin"`
	ActionRef *ActionRef `json:"actionRef,omitempty"`
}
type Dialogue struct {
	SpeakerAssetID *string `json:"speakerAssetId"`
	Text           string  `json:"text"`
}
type Camera struct {
	Size     string `json:"size"`
	Angle    string `json:"angle"`
	Movement string `json:"movement"`
}
type PromptPart struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	AssetID     string `json:"assetId,omitempty"`
	LookID      string `json:"lookId,omitempty"`
	LabelAtSave string `json:"labelAtSave,omitempty"`
}
type VideoPromptInput struct {
	Parts []PromptPart `json:"parts"`
}
type ShotValue struct {
	SourceRanges    []SourceRange  `json:"sourceRanges"`
	Action          string         `json:"action"`
	Dialogues       []Dialogue     `json:"dialogues"`
	Camera          Camera         `json:"camera"`
	DurationSeconds *float64       `json:"durationSeconds"`
	DesignNotes     string         `json:"designNotes"`
	SelectedAssets  []VisualTarget `json:"selectedAssets"`
}
type ShotBasis struct {
	SceneBasis
	SceneRangeDigest string `json:"sceneRangeDigest"`
}
type AssetBasis struct {
	Target           VisualTarget `json:"target"`
	InputDigest      string       `json:"inputDigest"`
	VisualPromptRefs []ActionRef  `json:"visualPromptRefs"`
}
type VideoPromptBasis struct {
	SceneBasis
	ShotInputDigest string        `json:"shotInputDigest"`
	Assets          []AssetBasis  `json:"assets"`
	SupportRanges   []SourceRange `json:"supportRanges"`
}
type VideoPrompt struct {
	VideoPromptInput
	Basis     VideoPromptBasis `json:"basis"`
	Origin    string           `json:"origin"`
	ActionRef *ActionRef       `json:"actionRef,omitempty"`
}
type Shot struct {
	ShotValue
	ID          string       `json:"id"`
	SceneID     string       `json:"sceneId"`
	VideoPrompt *VideoPrompt `json:"videoPrompt"`
	Basis       ShotBasis    `json:"basis"`
	Origin      string       `json:"origin"`
	ActionRef   *ActionRef   `json:"actionRef,omitempty"`
}
type StoryboardIssue struct {
	Code    string `json:"code"`
	AssetID string `json:"assetId,omitempty"`
	LookID  string `json:"lookId,omitempty"`
	SceneID string `json:"sceneId,omitempty"`
	ShotID  string `json:"shotId,omitempty"`
}

func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func NewStoryboard(version uint64) *Storyboard {
	return &Storyboard{SourceVersion: version, Index: EpisodeIndex{SupportRanges: []SourceRange{}, Episodes: []Episode{}}, Scenes: []Scene{}, Shots: []Shot{}}
}
func (b *Storyboard) Episode(id string) *Episode {
	for i := range b.Index.Episodes {
		if b.Index.Episodes[i].ID == id {
			return &b.Index.Episodes[i]
		}
	}
	return nil
}
func (b *Storyboard) Scene(id string) *Scene {
	for i := range b.Scenes {
		if b.Scenes[i].ID == id {
			return &b.Scenes[i]
		}
	}
	return nil
}
func (b *Storyboard) Shot(id string) *Shot {
	for i := range b.Shots {
		if b.Shots[i].ID == id {
			return &b.Shots[i]
		}
	}
	return nil
}

// 范围以原文 span 内的 Unicode 码点解析；分隔符不是原文，不得虚构。
func (s ScriptSource) rangeBounds(r SourceRange) (int, int, error) {
	if s.rangeIndex != nil {
		a, okA := s.rangeIndex[r.Start.SpanID]
		b, okB := s.rangeIndex[r.End.SpanID]
		if !okA || !okB || r.Start.Offset < 0 || r.Start.Offset > a.length || r.End.Offset < 0 || r.End.Offset > b.length {
			return 0, 0, fault.ErrInvalid
		}
		start, end := a.base+r.Start.Offset, b.base+r.End.Offset
		if end <= start {
			return 0, 0, fault.ErrInvalid
		}
		return start, end, nil
	}

	start, end, base := -1, -1, 0
	for _, span := range s.Spans {
		n := len([]rune(span.Text))
		if span.ID == r.Start.SpanID {
			if r.Start.Offset < 0 || r.Start.Offset > n {
				return 0, 0, fault.ErrInvalid
			}
			start = base + r.Start.Offset
		}
		if span.ID == r.End.SpanID {
			if r.End.Offset < 0 || r.End.Offset > n {
				return 0, 0, fault.ErrInvalid
			}
			end = base + r.End.Offset
		}
		base += n
	}
	if start < 0 || end <= start {
		return 0, 0, fault.ErrInvalid
	}
	return start, end, nil
}
func (s ScriptSource) ValidateRanges(ranges []SourceRange, empty bool) error {
	if len(ranges) == 0 && !empty {
		return fault.ErrInvalid
	}
	last := -1
	for _, r := range ranges {
		a, b, e := s.rangeBounds(r)
		if e != nil || a < last {
			return fault.ErrInvalid
		}
		last = b
	}
	return nil
}
func (s ScriptSource) Fragments(ranges []SourceRange) ([]SourceFragment, error) {
	if err := s.ValidateRanges(ranges, true); err != nil {
		return nil, err
	}
	out := []SourceFragment{}
	positions := map[string]int{}
	for i, span := range s.Spans {
		positions[span.ID] = i
	}
	for _, r := range ranges {
		start, end := positions[r.Start.SpanID], positions[r.End.SpanID]
		for i := start; i <= end; i++ {
			span := s.Spans[i]
			text := []rune(span.Text)
			lo, hi := 0, len(text)
			if i == start {
				lo = r.Start.Offset
			}
			if i == end {
				hi = r.End.Offset
			}
			out = append(out, SourceFragment{span.ID, lo, hi, string(text[lo:hi])})
		}
	}
	return out, nil
}
func (s ScriptSource) RangesWithin(child, parent []SourceRange) bool {
	if s.ValidateRanges(child, false) != nil {
		return false
	}
	parent = s.NormalizeRanges(parent)
	for _, r := range child {
		a, b, e := s.rangeBounds(r)
		if e != nil {
			return false
		}
		found := false
		for _, q := range parent {
			c, d, e := s.rangeBounds(q)
			found = found || e == nil && a >= c && b <= d
		}
		if !found {
			return false
		}
	}
	return true
}
func (p *Project) ValidateEpisodeIndex(complete bool) error {
	b := p.Storyboard
	if b == nil || b.SourceVersion != p.ScriptSource.Version {
		return fault.New("EPISODE_INDEX_REQUIRED", "当前原文没有有效分集目录")
	}
	if p.ScriptSource.ValidateRanges(b.Index.SupportRanges, true) != nil {
		return fault.ErrInvalid
	}
	type interval struct{ a, b int }
	all := []interval{}
	for _, r := range b.Index.SupportRanges {
		a, z, e := p.ScriptSource.rangeBounds(r)
		if e != nil {
			return e
		}
		all = append(all, interval{a, z})
	}
	seen := map[string]bool{}
	last := -1
	for _, ep := range b.Index.Episodes {
		if ep.ID == "" || seen[ep.ID] || strings.TrimSpace(ep.Title) == "" || p.ScriptSource.ValidateRanges(ep.SourceRanges, false) != nil {
			return fault.ErrInvalid
		}
		seen[ep.ID] = true
		first, _, _ := p.ScriptSource.rangeBounds(ep.SourceRanges[0])
		if first < last {
			return fault.ErrInvalid
		}
		last = first
		for _, r := range ep.SourceRanges {
			a, z, _ := p.ScriptSource.rangeBounds(r)
			all = append(all, interval{a, z})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].a < all[j].a })
	end := 0
	for _, r := range all {
		if r.a < end {
			return fault.ErrInvalid
		}
		if complete && r.a != end {
			return fault.ErrInvalid
		}
		end = r.b
	}
	if !complete {
		return nil
	}
	total := 0
	for _, s := range p.ScriptSource.Spans {
		total += len([]rune(s.Text))
	}
	if end != total || b.Index.Segmentation == nil || len(b.Index.Episodes) == 0 {
		return fault.ErrInvalid
	}
	switch *b.Index.Segmentation {
	case "episodes":
	case "whole":
		if len(b.Index.Episodes) != 1 || b.Index.Episodes[0].EpisodeNumber != nil {
			return fault.ErrInvalid
		}
	default:
		return fault.ErrInvalid
	}
	return nil
}
func (p *Project) IndexReady() bool {
	r := p.LatestRunByContract(EpisodeIndexContract)
	return r != nil && r.Status == RunCompleted && p.ValidateEpisodeIndex(true) == nil
}
func (p *Project) EpisodeSceneBasis(ep *Episode) SceneBasis {
	return SceneBasis{p.ScriptSource.Version, Digest(ep.SourceRanges)}
}
func (p *Project) ShotInputDigest(sh *Shot) string {
	sc := p.Storyboard.Scene(sh.SceneID)
	return Digest(struct {
		SourceVersion      uint64
		EpisodeRangeDigest string
		SceneID            string
		SceneRanges        []SourceRange
		LocationAssetID    *string
		Time, Summary      string
		Shot               ShotValue
	}{p.ScriptSource.Version, Digest(p.Storyboard.Episode(sc.EpisodeID).SourceRanges), sc.ID, sc.SourceRanges, sc.LocationAssetID, sc.Time, sc.Summary, sh.ShotValue})
}
func (p *Project) AssetMaterial(t VisualTarget) (any, AssetBasis, error) {
	if err := p.ValidateTarget(t); err != nil {
		return nil, AssetBasis{}, err
	}
	a := p.Asset(t.AssetID)
	v := p.Prompt(t)
	if !p.ValidPrompt(v, "") {
		return nil, AssetBasis{}, fault.New("VISUAL_PROMPTS_REQUIRED", "选用资产缺少有效视觉正文")
	}
	var look *Look
	if t.LookID != "" {
		for i := range a.Looks {
			if a.Looks[i].ID == t.LookID {
				l := a.Looks[i]
				l.ImageFileID = ""
				look = &l
			}
		}
	}
	prompts := []VisualPrompt{*v}
	if t.LookID != "" {
		prompts = append([]VisualPrompt{*p.Prompt(VisualTarget{AssetID: t.AssetID})}, prompts...)
	}
	refs := []ActionRef{}
	content := []VisualPromptInput{}
	for _, v := range prompts {
		if v.RunID != "" {
			refs = append(refs, v.ActionRef)
		}
		content = append(content, v.VisualPromptInput)
	}
	// 名称与图片仅作展示；所引用具体形象的有效视觉正文是唯一附加输入。
	var description *string
	if look != nil {
		description = &look.Description
	}
	input := struct {
		Description     string
		Character       *Character
		LookDescription *string
		SourceIDs       []string
		Prompts         []VisualPromptInput
	}{a.Description, a.Character, description, a.SourceIDs, content}
	basis := AssetBasis{t, Digest(input), refs}
	material := struct {
		Target        VisualTarget   `json:"target"`
		Asset         Asset          `json:"asset"`
		Look          *Look          `json:"look"`
		VisualPrompts []VisualPrompt `json:"visualPrompts"`
		Basis         AssetBasis     `json:"basis"`
	}{t, *a, look, prompts, basis}
	material.Asset.ImageFileID = ""
	material.Asset.Looks = []Look{}
	return material, basis, nil
}
func (p *Project) ValidateShotReferences(sc *Scene, sh *Shot) error {
	seen := map[VisualTarget]bool{}
	speaker := map[string]bool{}
	location := sc.LocationAssetID == nil
	for _, t := range sh.SelectedAssets {
		if seen[t] {
			return fault.ErrInvalid
		}
		seen[t] = true
		if _, _, e := p.AssetMaterial(t); e != nil {
			if errors.Is(e, fault.ErrNotFound) {
				return fmt.Errorf("%w: 所选资产或形象不属于本项目", fault.ErrInvalid)
			}
			return e
		}
		a := p.Asset(t.AssetID)
		speaker[a.ID] = a.Kind == "character"
		if sc.LocationAssetID != nil && t.AssetID == *sc.LocationAssetID && t.LookID == "" {
			location = true
		}
	}
	if !location {
		return fmt.Errorf("%w: 场景必须选择地点资产", fault.ErrInvalid)
	}
	for _, d := range sh.Dialogues {
		if strings.TrimSpace(d.Text) == "" || d.SpeakerAssetID != nil && !speaker[*d.SpeakerAssetID] {
			return fault.ErrInvalid
		}
	}
	if sh.DurationSeconds != nil && *sh.DurationSeconds <= 0 {
		return fault.ErrInvalid
	}
	if sh.VideoPrompt != nil {
		if len(sh.VideoPrompt.Parts) == 0 {
			return fault.ErrInvalid
		}
		for _, part := range sh.VideoPrompt.Parts {
			switch part.Type {
			case "text":
				if strings.TrimSpace(part.Text) == "" || part.AssetID != "" || part.LookID != "" {
					return fault.ErrInvalid
				}
			case "asset":
				t := VisualTarget{AssetID: part.AssetID, LookID: part.LookID}
				if !seen[t] || part.Text != "" {
					return fault.ErrInvalid
				}
			default:
				return fault.ErrInvalid
			}
		}
	}
	return nil
}
func (p *Project) SceneIssues(sc *Scene) []StoryboardIssue {
	out := []StoryboardIssue{}
	b := p.Storyboard
	ep := b.Episode(sc.EpisodeID)
	if b.SourceVersion != p.ScriptSource.Version || sc.Basis.SourceVersion != p.ScriptSource.Version {
		out = append(out, StoryboardIssue{Code: "SOURCE_CHANGED"})
	}
	if ep == nil || sc.Basis.EpisodeRangeDigest != Digest(ep.SourceRanges) || !p.ScriptSource.RangesWithin(sc.SourceRanges, ep.SourceRanges) {
		out = append(out, StoryboardIssue{Code: "EPISODE_RANGE_CHANGED", SceneID: sc.ID})
	}
	if sc.LocationAssetID != nil {
		a := p.Asset(*sc.LocationAssetID)
		if a == nil || a.Kind != "location" {
			out = append(out, StoryboardIssue{Code: "ASSET_MISSING", AssetID: *sc.LocationAssetID})
		}
	}
	return out
}
func (p *Project) ShotIssues(sh *Shot) []StoryboardIssue {
	out := []StoryboardIssue{}
	sc := p.Storyboard.Scene(sh.SceneID)
	if sc == nil {
		return []StoryboardIssue{{Code: "SCENE_RANGE_CHANGED", ShotID: sh.ID}}
	}
	out = append(out, p.SceneIssues(sc)...)
	if sh.Basis.SourceVersion != p.ScriptSource.Version {
		out = append(out, StoryboardIssue{Code: "SOURCE_CHANGED"})
	}
	if sh.Basis.EpisodeRangeDigest != sc.Basis.EpisodeRangeDigest {
		out = append(out, StoryboardIssue{Code: "EPISODE_RANGE_CHANGED"})
	}
	if sh.Basis.SceneRangeDigest != Digest(sc.SourceRanges) || !p.ScriptSource.RangesWithin(sh.SourceRanges, sc.SourceRanges) {
		out = append(out, StoryboardIssue{Code: "SCENE_RANGE_CHANGED", ShotID: sh.ID})
	}
	refIssues := []StoryboardIssue{}
	selected := map[VisualTarget]bool{}
	for _, t := range sh.SelectedAssets {
		selected[t] = true
		code := ""
		if p.Asset(t.AssetID) == nil {
			code = "ASSET_MISSING"
		} else if p.ValidateTarget(t) != nil {
			code = "LOOK_MISSING"
		} else if !p.ValidPrompt(p.Prompt(t), "") {
			code = "VISUAL_PROMPT_INVALID"
		}
		if code != "" {
			refIssues = append(refIssues, StoryboardIssue{Code: code, AssetID: t.AssetID, LookID: t.LookID, ShotID: sh.ID})
		}
	}
	if sh.VideoPrompt != nil {
		for _, part := range sh.VideoPrompt.Parts {
			if part.Type == "asset" && !selected[VisualTarget{AssetID: part.AssetID, LookID: part.LookID}] {
				refIssues = append(refIssues, StoryboardIssue{Code: "REFERENCE_NOT_SELECTED", AssetID: part.AssetID, LookID: part.LookID, ShotID: sh.ID})
			}
		}
	}
	if err := p.ValidateShotReferences(sc, sh); err != nil && len(refIssues) == 0 {
		refIssues = append(refIssues, StoryboardIssue{Code: "REFERENCE_NOT_SELECTED", ShotID: sh.ID})
	}
	out = append(out, refIssues...)
	return out
}
func (p *Project) VideoPromptIssues(sh *Shot, runID string) []StoryboardIssue {
	return p.videoPromptIssues(sh, runID, p.ShotIssues(sh))
}

// ShotAndVideoPromptIssues 为同一读视图共用镜头校验，不省略任何正文依赖核对。
func (p *Project) ShotAndVideoPromptIssues(sh *Shot, runID string) ([]StoryboardIssue, []StoryboardIssue) {
	issues := p.ShotIssues(sh)
	return issues, p.videoPromptIssues(sh, runID, issues)
}
func (p *Project) videoPromptIssues(sh *Shot, runID string, out []StoryboardIssue) []StoryboardIssue {
	v := sh.VideoPrompt
	if v == nil {
		return append(out, StoryboardIssue{Code: "PROMPT_MISSING", ShotID: sh.ID})
	}
	if len(out) == 0 && (v.Basis.SourceVersion != p.ScriptSource.Version || v.Basis.ShotInputDigest != p.ShotInputDigest(sh)) {
		out = append(out, StoryboardIssue{Code: "SHOT_INPUT_CHANGED", ShotID: sh.ID})
	}
	for _, ab := range v.Basis.Assets {
		_, current, e := p.AssetMaterial(ab.Target)
		if e != nil || current.InputDigest != ab.InputDigest {
			out = append(out, StoryboardIssue{Code: "ASSET_INPUT_CHANGED", AssetID: ab.Target.AssetID, LookID: ab.Target.LookID})
		}
	}
	if v.Origin == "model" {
		done := false
		for _, r := range p.Runs {
			done = done || v.ActionRef != nil && r.ID == v.ActionRef.RunID && (r.Status == RunCompleted || r.ID == runID && r.Status == RunRunning)
		}
		if !done {
			out = append(out, StoryboardIssue{Code: "RUN_INCOMPLETE", ShotID: sh.ID})
		}
	}
	return out
}
func (p *Project) PromptLabel(t VisualTarget) (string, bool) {
	a := p.Asset(t.AssetID)
	if a == nil {
		return t.AssetID, false
	}
	if t.LookID == "" {
		return a.Name, true
	}
	for _, l := range a.Looks {
		if l.ID == t.LookID {
			return a.Name + "·" + l.Name, true
		}
	}
	return a.Name + "·" + t.LookID, false
}

// NormalizeRanges 记录所给支持范围的精确并集：去重、不虚构未读间隙。
func (s ScriptSource) NormalizeRanges(ranges []SourceRange) []SourceRange {
	valid := append([]SourceRange{}, ranges...)
	sort.SliceStable(valid, func(i, j int) bool {
		startI, _, _ := s.rangeBounds(valid[i])
		startJ, _, _ := s.rangeBounds(valid[j])
		return startI < startJ
	})
	out := []SourceRange{}
	for _, r := range valid {
		a, b, e := s.rangeBounds(r)
		if e != nil {
			continue
		}
		if len(out) == 0 {
			out = append(out, r)
			continue
		}
		last := &out[len(out)-1]
		_, end, _ := s.rangeBounds(*last)
		if a <= end {
			if b > end {
				last.End = r.End
			}
		} else {
			out = append(out, r)
		}
	}
	return out
}

// 索引范围没有集目标；分集范围始终暴露冻结的镜头清单，包括空集。
func (scope StoryboardScope) MarshalJSON() ([]byte, error) {
	if scope.Operation == "index" {
		return json.Marshal(struct {
			Operation string `json:"operation"`
		}{scope.Operation})
	}
	return json.Marshal(struct {
		Operation          string                     `json:"operation"`
		EpisodeID          string                     `json:"episodeId"`
		ShotIDs            []string                   `json:"shotIds"`
		EpisodeRangeDigest string                     `json:"episodeRangeDigest"`
		BreakdownMode      string                     `json:"breakdownMode,omitempty"`
		VideoConstraint    *StoryboardVideoConstraint `json:"videoConstraint,omitempty"`
	}{scope.Operation, scope.EpisodeID, append([]string{}, scope.ShotIDs...), scope.EpisodeRangeDigest, scope.BreakdownMode, scope.VideoConstraint})
}
