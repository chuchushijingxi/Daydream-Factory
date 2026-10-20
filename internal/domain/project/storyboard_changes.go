package project

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
)

// Operation 是判别联合的线格式；采用前由应用侧对照共享严格 schema 校验。
type StoryboardOperation struct {
	Op           string          `json:"op"`
	EpisodeID    string          `json:"episodeId,omitempty"`
	SceneID      string          `json:"sceneId,omitempty"`
	ShotID       string          `json:"shotId,omitempty"`
	AfterShotID  string          `json:"afterShotId,omitempty"`
	BeforeShotID string          `json:"beforeShotId,omitempty"`
	Value        json.RawMessage `json:"value,omitempty"`
	Patch        json.RawMessage `json:"patch,omitempty"`
	Ranges       []SourceRange   `json:"ranges,omitempty"`
	Segmentation string          `json:"segmentation,omitempty"`
	SceneIDs     []string        `json:"sceneIds,omitempty"`
	ShotIDs      []string        `json:"shotIds,omitempty"`
}
type StoryboardChange struct {
	EntityType string `json:"entityType"`
	ID         string `json:"id"`
	Before     any    `json:"before"`
	After      any    `json:"after"`
}
type StoryboardInvalidated struct {
	EntityType string            `json:"entityType"`
	ID         string            `json:"id"`
	Reasons    []StoryboardIssue `json:"reasons"`
}
type StoryboardReceipt struct {
	ProjectID   string                  `json:"projectId"`
	Revision    uint64                  `json:"revision"`
	Changes     []StoryboardChange      `json:"changes"`
	Invalidated []StoryboardInvalidated `json:"invalidated"`
}
type StoryboardWrite struct {
	Scope         *StoryboardScope
	Ref           *ActionRef
	SupportRanges []SourceRange
}

func patchValue[T any](value *T, raw json.RawMessage, omit ...string) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	var fields, patch map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	if json.Unmarshal(raw, &patch) != nil || len(patch) == 0 {
		return fault.ErrInvalid
	}
	for _, key := range omit {
		delete(patch, key)
	}
	for key, v := range patch {
		fields[key] = v
	}
	b, e = json.Marshal(fields)
	if e != nil {
		return e
	}
	// 合并对象已含全部未触碰字段；解码为新值，替换数组不得继承被省略的旧形象 ID。
	var replacement T
	if e = json.Unmarshal(b, &replacement); e != nil {
		return e
	}
	*value = replacement
	return nil
}
func hasPatch(raw json.RawMessage, key string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	_, ok := m[key]
	return ok
}
func removeID(ids []string, id string) []string {
	out := []string{}
	for _, s := range ids {
		if s != id {
			out = append(out, s)
		}
	}
	return out
}
func permutation(old, next []string) bool {
	if len(old) != len(next) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range old {
		seen[id] = true
	}
	for _, id := range next {
		if !seen[id] {
			return false
		}
		delete(seen, id)
	}
	return len(seen) == 0
}
func (b *Storyboard) deleteShot(id string) {
	sh := b.Shot(id)
	if sh == nil {
		return
	}
	sc := b.Scene(sh.SceneID)
	sc.ShotIDs = removeID(sc.ShotIDs, id)
	out := []Shot{}
	for _, v := range b.Shots {
		if v.ID != id {
			out = append(out, v)
		}
	}
	b.Shots = out
}
func (b *Storyboard) deleteScene(id string) {
	sc := b.Scene(id)
	if sc == nil {
		return
	}
	for _, id := range append([]string{}, sc.ShotIDs...) {
		b.deleteShot(id)
	}
	ep := b.Episode(sc.EpisodeID)
	ep.SceneIDs = removeID(ep.SceneIDs, id)
	out := []Scene{}
	for _, v := range b.Scenes {
		if v.ID != id {
			out = append(out, v)
		}
	}
	b.Scenes = out
}
func (b *Storyboard) deleteEpisode(id string) {
	ep := b.Episode(id)
	if ep == nil {
		return
	}
	for _, id := range append([]string{}, ep.SceneIDs...) {
		b.deleteScene(id)
	}
	out := []Episode{}
	for _, v := range b.Index.Episodes {
		if v.ID != id {
			out = append(out, v)
		}
	}
	b.Index.Episodes = out
}
func (p *Project) stampPrompt(sh *Shot, origin string, write StoryboardWrite) error {
	if sh.VideoPrompt == nil {
		return nil
	}
	used := map[VisualTarget]bool{}
	for _, part := range sh.VideoPrompt.Parts {
		if part.Type == "asset" {
			used[VisualTarget{AssetID: part.AssetID, LookID: part.LookID}] = true
		}
	}
	if len(used) != len(sh.SelectedAssets) {
		return fmt.Errorf("%w: 新保存的正文必须与已选资产/形象引用完全一致", fault.ErrInvalid)
	}
	sc := p.Storyboard.Scene(sh.SceneID)
	ep := p.Storyboard.Episode(sc.EpisodeID)
	basis := VideoPromptBasis{SceneBasis: p.EpisodeSceneBasis(ep), ShotInputDigest: p.ShotInputDigest(sh), Assets: []AssetBasis{}, SupportRanges: append([]SourceRange{}, write.SupportRanges...)}
	for _, t := range sh.SelectedAssets {
		_, ab, e := p.AssetMaterial(t)
		if e != nil {
			return e
		}
		basis.Assets = append(basis.Assets, ab)
	}
	for i := range sh.VideoPrompt.Parts {
		part := &sh.VideoPrompt.Parts[i]
		if part.Type == "asset" {
			part.LabelAtSave, _ = p.PromptLabel(VisualTarget{AssetID: part.AssetID, LookID: part.LookID})
		}
	}
	sh.VideoPrompt.Basis, sh.VideoPrompt.Origin, sh.VideoPrompt.ActionRef = basis, origin, write.Ref
	return nil
}
func (p *Project) ApplyStoryboard(ops []StoryboardOperation, write StoryboardWrite) (StoryboardReceipt, error) {
	receipt := StoryboardReceipt{ProjectID: p.ID, Revision: p.Revision, Changes: []StoryboardChange{}, Invalidated: []StoryboardInvalidated{}}
	if write.Ref != nil || write.Scope != nil {
		r := p.LatestRun()
		if r == nil || r.Status != RunRunning {
			return receipt, fault.ErrCancelled
		}
		if write.Ref == nil || write.Scope == nil || r.StoryboardScope != write.Scope || write.Ref.RunID != r.ID || write.Ref.CallID == "" || write.Ref.ActionID == "" || r.SourceVersion != p.ScriptSource.Version || (r.Contract != EpisodeIndexContract && r.Contract != StoryboardContract) {
			return receipt, fault.ErrInvalid
		}
		if (r.Contract == EpisodeIndexContract) != (write.Scope.Operation == "index") {
			return receipt, fault.ErrInvalid
		}
		if write.Scope.Operation != "index" {
			if p.Storyboard == nil || p.Storyboard.Episode(write.Scope.EpisodeID) == nil {
				return receipt, fault.ErrNotFound
			}
			if Digest(p.Storyboard.Episode(write.Scope.EpisodeID).SourceRanges) != write.Scope.EpisodeRangeDigest {
				return receipt, fault.New("EPISODE_RANGE_STALE", "任务冻结的本集范围已变化")
			}
		}
	}
	if p.Storyboard == nil || len(ops) == 0 {
		return receipt, fault.ErrInvalid
	}
	candidate := Clone(*p)
	b := candidate.Storyboard
	origin := "user"
	if write.Ref != nil {
		origin = "model"
	}
	originalEpisodes, originalScenes, originalShots := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, v := range b.Index.Episodes {
		originalEpisodes[v.ID] = true
	}
	for _, v := range b.Scenes {
		originalScenes[v.ID] = true
	}
	for _, v := range b.Shots {
		originalShots[v.ID] = true
	}
	touched := map[string]bool{}
	sourceChanged := map[string]bool{}
	promptChanged := map[string]bool{}
	sceneChanged := map[string]bool{}
	shotChanged := map[string]bool{}
	indexChanged := false
	updatedEpisodeRanges := map[string]bool{}
	supportReplaced := false
	orders := []StoryboardOperation{}
	for _, op := range ops {
		key := op.Op
		id := op.EpisodeID + op.SceneID + op.ShotID
		switch {
		case strings.Contains(op.Op, "Episode"):
			key = "episode"
		case strings.Contains(op.Op, "Scene") && op.Op != "setSceneOrder":
			key = "scene"
		case strings.Contains(op.Op, "Shot") && op.Op != "setShotOrder":
			key = "shot"
		}
		if strings.HasPrefix(op.Op, "create") {
			id = identity.New(key)
		}
		if touched[key+":"+id] {
			return receipt, fault.ErrInvalid
		}
		touched[key+":"+id] = true
		// 任何变更前先冻结任务与集范围；本批次新分配的对象不允许被后续操作引用。
		if scope := write.Scope; scope != nil {
			isIndex := op.Op == "setSupportRanges" || op.Op == "setSegmentation" || strings.HasSuffix(op.Op, "Episode")
			if (scope.Operation == "index") != isIndex {
				return receipt, fault.New("STORYBOARD_SCOPE_VIOLATION", "操作超出当前任务范围")
			}
			if !isIndex {
				epID := op.EpisodeID
				if op.SceneID != "" {
					sc := b.Scene(op.SceneID)
					if sc == nil {
						return receipt, fault.ErrNotFound
					}
					epID = sc.EpisodeID
				}
				if op.ShotID != "" {
					sh := b.Shot(op.ShotID)
					if sh == nil {
						return receipt, fault.ErrNotFound
					}
					epID = b.Scene(sh.SceneID).EpisodeID
				}
				if epID != scope.EpisodeID {
					return receipt, fault.New("STORYBOARD_SCOPE_VIOLATION", "不能修改其他集")
				}
				if scope.Operation != "breakdown" {
					allowed := false
					for _, shotID := range scope.ShotIDs {
						allowed = allowed || op.ShotID == shotID
					}
					var fields map[string]json.RawMessage
					_ = json.Unmarshal(op.Patch, &fields)
					if !allowed || op.Op != "updateShot" || len(fields) != 1 || fields["videoPrompt"] == nil || string(fields["videoPrompt"]) == "null" {
						return receipt, fault.New("STORYBOARD_SCOPE_VIOLATION", "正文任务只能保存冻结镜头的非空正文")
					}
				}
			}
		}
		switch op.Op {
		case "setSegmentation":
			if op.Segmentation != "episodes" && op.Segmentation != "whole" {
				return receipt, fault.ErrInvalid
			}
			v := op.Segmentation
			b.Index.Segmentation = &v
			indexChanged = true
		case "setSupportRanges":
			supportReplaced = true
			b.Index.SupportRanges = append([]SourceRange{}, op.Ranges...)
			indexChanged = true
		case "createEpisode":
			var v EpisodeValue
			if json.Unmarshal(op.Value, &v) != nil {
				return receipt, fault.ErrInvalid
			}
			b.Index.Episodes = append(b.Index.Episodes, Episode{EpisodeValue: v, ID: id, SceneIDs: []string{}, Origin: origin, ActionRef: write.Ref})
			indexChanged = true
		case "updateEpisode":
			ep := b.Episode(op.EpisodeID)
			if ep == nil || !originalEpisodes[ep.ID] {
				return receipt, fault.ErrNotFound
			}
			if e := patchValue(&ep.EpisodeValue, op.Patch); e != nil {
				return receipt, e
			}
			if hasPatch(op.Patch, "sourceRanges") {
				updatedEpisodeRanges[ep.ID] = true
			}
			ep.Origin, ep.ActionRef = origin, write.Ref
			indexChanged = true
		case "deleteEpisode":
			if !originalEpisodes[op.EpisodeID] || b.Episode(op.EpisodeID) == nil {
				return receipt, fault.ErrNotFound
			}
			b.deleteEpisode(op.EpisodeID)
			indexChanged = true
		case "createScene":
			ep := b.Episode(op.EpisodeID)
			if ep == nil || !originalEpisodes[ep.ID] {
				return receipt, fault.ErrNotFound
			}
			var v SceneValue
			if json.Unmarshal(op.Value, &v) != nil {
				return receipt, fault.ErrInvalid
			}
			b.Scenes = append(b.Scenes, Scene{SceneValue: v, ID: id, EpisodeID: ep.ID, ShotIDs: []string{}, Basis: candidate.EpisodeSceneBasis(ep), Origin: origin, ActionRef: write.Ref})
			ep.SceneIDs = append(ep.SceneIDs, id)
			sceneChanged[id], sourceChanged[id] = true, true
		case "updateScene":
			sc := b.Scene(op.SceneID)
			if sc == nil || !originalScenes[sc.ID] {
				return receipt, fault.ErrNotFound
			}
			if e := patchValue(&sc.SceneValue, op.Patch); e != nil {
				return receipt, e
			}
			if hasPatch(op.Patch, "sourceRanges") {
				sc.Basis = candidate.EpisodeSceneBasis(b.Episode(sc.EpisodeID))
				sourceChanged[sc.ID] = true
			}
			sc.Origin, sc.ActionRef = origin, write.Ref
			sceneChanged[sc.ID] = true
		case "deleteScene":
			if b.Scene(op.SceneID) == nil || !originalScenes[op.SceneID] {
				return receipt, fault.ErrNotFound
			}
			b.deleteScene(op.SceneID)
		case "setSceneOrder":
			orders = append(orders, op)
		case "createShot":
			sc := b.Scene(op.SceneID)
			if sc == nil || !originalScenes[sc.ID] {
				return receipt, fault.ErrNotFound
			}
			insertAt := len(sc.ShotIDs)
			if op.AfterShotID != "" && op.BeforeShotID != "" {
				return receipt, fault.ErrInvalid
			}
			anchorID := op.BeforeShotID
			if op.AfterShotID != "" {
				anchorID = op.AfterShotID
			}
			if anchorID != "" {
				anchor := b.Shot(anchorID)
				if anchor == nil || !originalShots[anchor.ID] || anchor.SceneID != sc.ID {
					return receipt, fault.ErrInvalid
				}
				for i, shotID := range sc.ShotIDs {
					if shotID == anchor.ID {
						insertAt = i
						if op.AfterShotID != "" {
							insertAt++
						}
						break
					}
				}
			}
			var v struct {
				ShotValue
				VideoPrompt *VideoPrompt `json:"videoPrompt"`
			}
			if json.Unmarshal(op.Value, &v) != nil {
				return receipt, fault.ErrInvalid
			}
			b.Shots = append(b.Shots, Shot{ShotValue: v.ShotValue, ID: id, SceneID: sc.ID, VideoPrompt: v.VideoPrompt, Basis: ShotBasis{candidate.EpisodeSceneBasis(b.Episode(sc.EpisodeID)), Digest(sc.SourceRanges)}, Origin: origin, ActionRef: write.Ref})
			sc.ShotIDs = append(sc.ShotIDs, "")
			copy(sc.ShotIDs[insertAt+1:], sc.ShotIDs[insertAt:])
			sc.ShotIDs[insertAt] = id
			shotChanged[id], sourceChanged[id], promptChanged[id] = true, true, true
		case "updateShot":
			sh := b.Shot(op.ShotID)
			if sh == nil || !originalShots[sh.ID] {
				return receipt, fault.ErrNotFound
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(op.Patch, &fields)
			if e := patchValue(&sh.ShotValue, op.Patch, "videoPrompt", "sceneId"); e != nil {
				return receipt, e
			}
			if raw, ok := fields["sceneId"]; ok {
				var newID string
				if json.Unmarshal(raw, &newID) != nil {
					return receipt, fault.ErrInvalid
				}
				old, sc := b.Scene(sh.SceneID), b.Scene(newID)
				if sc == nil || !originalScenes[newID] || sc.EpisodeID != old.EpisodeID {
					return receipt, fault.ErrInvalid
				}
				if old.ID != sc.ID {
					old.ShotIDs = removeID(old.ShotIDs, sh.ID)
					sc.ShotIDs = append(sc.ShotIDs, sh.ID)
					sh.SceneID = sc.ID
					sourceChanged[sh.ID] = true
				}
			}
			sc := b.Scene(sh.SceneID)
			if hasPatch(op.Patch, "sourceRanges") || sourceChanged[sh.ID] {
				sh.Basis = ShotBasis{candidate.EpisodeSceneBasis(b.Episode(sc.EpisodeID)), Digest(sc.SourceRanges)}
				sourceChanged[sh.ID] = true
			}
			if raw, ok := fields["videoPrompt"]; ok {
				// 提供的正文整体替换联合数组；解码进既有切片会保留旧元素的遗漏字段。
				var replacement *VideoPrompt
				if json.Unmarshal(raw, &replacement) != nil {
					return receipt, fault.ErrInvalid
				}
				sh.VideoPrompt = replacement
				promptChanged[sh.ID] = true
			}
			if len(fields) != 1 || !hasPatch(op.Patch, "videoPrompt") {
				sh.Origin, sh.ActionRef = origin, write.Ref
			}
			shotChanged[sh.ID] = true
		case "deleteShot":
			if b.Shot(op.ShotID) == nil || !originalShots[op.ShotID] {
				return receipt, fault.ErrNotFound
			}
			b.deleteShot(op.ShotID)
		case "setShotOrder":
			orders = append(orders, op)
		default:
			return receipt, fault.ErrInvalid
		}
	}
	for _, op := range orders {
		if op.Op == "setSceneOrder" {
			ep := b.Episode(op.EpisodeID)
			if ep == nil || !permutation(ep.SceneIDs, op.SceneIDs) {
				return receipt, fault.ErrInvalid
			}
			ep.SceneIDs = append([]string{}, op.SceneIDs...)
		} else {
			sc := b.Scene(op.SceneID)
			if sc == nil || !permutation(sc.ShotIDs, op.ShotIDs) {
				return receipt, fault.ErrInvalid
			}
			sc.ShotIDs = append([]string{}, op.ShotIDs...)
		}
	}

	if indexChanged {
		if b.SourceVersion != candidate.ScriptSource.Version {
			if !supportReplaced {
				return receipt, fault.ErrInvalid
			}
			for _, ep := range b.Index.Episodes {
				if originalEpisodes[ep.ID] && !updatedEpisodeRanges[ep.ID] {
					return receipt, fault.New("EPISODE_RANGE_STALE", "旧源范围不能解释为新源，须明确重定位或删除")
				}
			}
			b.SourceVersion = candidate.ScriptSource.Version
		}
		for _, ep := range b.Index.Episodes {
			if len(ep.SourceRanges) == 0 {
				return receipt, fault.ErrInvalid
			}
		}
		sort.SliceStable(b.Index.Episodes, func(i, j int) bool {
			a, _, _ := candidate.ScriptSource.rangeBounds(b.Index.Episodes[i].SourceRanges[0])
			z, _, _ := candidate.ScriptSource.rangeBounds(b.Index.Episodes[j].SourceRanges[0])
			return a < z
		})
		if err := candidate.ValidateEpisodeIndex(write.Scope == nil); err != nil {
			return receipt, err
		}
		b.Index.Origin, b.Index.ActionRef = origin, write.Ref
	}
	for id := range sceneChanged {
		sc := b.Scene(id)
		if sc == nil {
			continue
		}
		if strings.TrimSpace(sc.Title) == "" {
			return receipt, fault.ErrInvalid
		}
		if sourceChanged[id] && !candidate.ScriptSource.RangesWithin(sc.SourceRanges, b.Episode(sc.EpisodeID).SourceRanges) {
			return receipt, fault.ErrInvalid
		}
		if sc.LocationAssetID != nil {
			a := candidate.Asset(*sc.LocationAssetID)
			if a == nil || a.Kind != "location" {
				return receipt, fault.ErrInvalid
			}
			if _, _, e := candidate.AssetMaterial(VisualTarget{AssetID: a.ID}); e != nil {
				return receipt, e
			}
		}
	}
	for id := range shotChanged {
		sh := b.Shot(id)
		if sh == nil {
			continue
		}
		sc := b.Scene(sh.SceneID)
		if sourceChanged[id] && !candidate.ScriptSource.RangesWithin(sh.SourceRanges, sc.SourceRanges) {
			return receipt, fault.ErrInvalid
		}
		if err := candidate.ValidateShotReferences(sc, sh); err != nil {
			return receipt, err
		}
		if err := write.Scope.validateShotDuration(sh, false); err != nil {
			return receipt, err
		}
		if promptChanged[id] {
			if err := candidate.stampPrompt(sh, origin, write); err != nil {
				return receipt, err
			}
		}
	}
	receipt.Changes = storyboardDiff(p.Storyboard, b)
	if len(receipt.Changes) > 0 {
		receipt.Revision = p.Revision + 1
	}
	for _, sh := range b.Shots {
		issues := candidate.VideoPromptIssues(&sh, "")
		before := p.Storyboard.Shot(sh.ID)
		if len(issues) > 0 && before != nil && !reflect.DeepEqual(issues, p.VideoPromptIssues(before, "")) {
			receipt.Invalidated = append(receipt.Invalidated, StoryboardInvalidated{"videoPrompt", sh.ID, issues})
		}
	}
	p.Storyboard = b
	return receipt, nil
}
func storyboardDiff(before, after *Storyboard) []StoryboardChange {
	out := []StoryboardChange{}
	if !reflect.DeepEqual(before.Index.Segmentation, after.Index.Segmentation) || !reflect.DeepEqual(before.Index.SupportRanges, after.Index.SupportRanges) {
		out = append(out, StoryboardChange{"index", "index", struct {
			Segmentation  *string       `json:"segmentation"`
			SupportRanges []SourceRange `json:"supportRanges"`
		}{before.Index.Segmentation, before.Index.SupportRanges}, struct {
			Segmentation  *string       `json:"segmentation"`
			SupportRanges []SourceRange `json:"supportRanges"`
		}{after.Index.Segmentation, after.Index.SupportRanges}})
	}
	diff := func(kind string, old, next map[string]any, order []string) {
		for _, id := range order {
			a, aok := old[id]
			b, bok := next[id]
			if aok == bok && reflect.DeepEqual(a, b) {
				continue
			}
			out = append(out, StoryboardChange{kind, id, a, b})
		}
	}
	for _, kind := range []string{"episode", "scene", "shot"} {
		old, next := map[string]any{}, map[string]any{}
		order := []string{}
		switch kind {
		case "episode":
			for _, v := range before.Index.Episodes {
				old[v.ID] = v
				order = append(order, v.ID)
			}
			for _, v := range after.Index.Episodes {
				next[v.ID] = v
				if old[v.ID] == nil {
					order = append(order, v.ID)
				}
			}
		case "scene":
			for _, v := range before.Scenes {
				old[v.ID] = v
				order = append(order, v.ID)
			}
			for _, v := range after.Scenes {
				next[v.ID] = v
				if old[v.ID] == nil {
					order = append(order, v.ID)
				}
			}
		case "shot":
			for _, v := range before.Shots {
				old[v.ID] = v
				order = append(order, v.ID)
			}
			for _, v := range after.Shots {
				next[v.ID] = v
				if old[v.ID] == nil {
					order = append(order, v.ID)
				}
			}
		}
		diff(kind, old, next, order)
	}
	return out
}
func (p *Project) StoryboardComplete(r *Run) error {
	if r.StoryboardScope == nil {
		return fault.ErrInvalid
	}
	scope := r.StoryboardScope
	if scope.Operation == "index" {
		return p.ValidateEpisodeIndex(true)
	}
	ep := p.Storyboard.Episode(scope.EpisodeID)
	if ep == nil || Digest(ep.SourceRanges) != scope.EpisodeRangeDigest {
		return fault.ErrConflict
	}
	ids := scope.ShotIDs
	if scope.Operation == "breakdown" {
		ids = []string{}
		for _, sceneID := range ep.SceneIDs {
			sc := p.Storyboard.Scene(sceneID)
			if sc == nil || len(p.SceneIssues(sc)) > 0 || len(sc.ShotIDs) == 0 {
				return fault.ErrInvalid
			}
			ids = append(ids, sc.ShotIDs...)
		}
	}
	if len(ids) == 0 {
		return fmt.Errorf("%w: 没有可处理的分镜目标", fault.ErrInvalid)
	}
	for _, id := range ids {
		sh := p.Storyboard.Shot(id)
		if sh == nil || len(p.VideoPromptIssues(sh, r.ID)) > 0 {
			return fault.New("STORYBOARD_INCOMPLETE", "目标镜头缺少有效正文或依据")
		}
		if err := scope.validateShotDuration(sh, true); err != nil {
			return err
		}
		if scope.Operation != "breakdown" && (sh.VideoPrompt.ActionRef == nil || sh.VideoPrompt.ActionRef.RunID != r.ID) {
			return fault.New("STORYBOARD_INCOMPLETE", "冻结正文尚未在本任务保存")
		}
	}
	return nil
}

func (scope *StoryboardScope) validateShotDuration(sh *Shot, require bool) error {
	if scope == nil || scope.Operation != "breakdown" || scope.VideoConstraint == nil {
		return nil
	}
	if sh.DurationSeconds == nil {
		if require {
			return fault.New("SHOT_DURATION_REQUIRED", fmt.Sprintf("镜头 %s 须估算时长，最长 %g 秒", sh.ID, scope.VideoConstraint.MaxDurationSeconds))
		}
		return nil
	}
	if *sh.DurationSeconds > scope.VideoConstraint.MaxDurationSeconds {
		return fault.New("SHOT_DURATION_EXCEEDED", fmt.Sprintf("镜头 %s 时长 %g 秒超过所选视频模型上限 %g 秒；须重新核对动作、完整对白及停顿的自然完成时间，内容装不下时合理分镜并同步修订正文；不能为通过校验只改秒数、加速台词或删对白", sh.ID, *sh.DurationSeconds, scope.VideoConstraint.MaxDurationSeconds))
	}
	return nil
}
