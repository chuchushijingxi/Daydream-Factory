package project

import d "drama-agent/internal/domain/project"

// 模型材料不嵌入页面视图，避免重复复制正文或随页面变化带入运行、预览字段。
// 业务正文、来源/依据和诊断仍来自同一项目快照，不建立另一份保存事实。
type storyboardEpisodeMaterial struct {
	SourceVersion uint64                    `json:"sourceVersion"`
	Episode       d.Episode                 `json:"episode"`
	Scenes        []storyboardSceneMaterial `json:"scenes"`
	Shots         []storyboardShotMaterial  `json:"shots"`
	Index         storyboardValidity        `json:"index"`
}

type storyboardValidity struct {
	Valid  bool                `json:"valid"`
	Issues []d.StoryboardIssue `json:"issues"`
}

type storyboardSceneMaterial struct {
	d.Scene
	storyboardValidity
}

type storyboardShotMaterial struct {
	d.Shot
	SelectedAssets []storyboardAssetMaterial `json:"selectedAssets"`
	VideoPrompt    *storyboardPromptMaterial `json:"videoPrompt"`
	storyboardValidity
}

type storyboardAssetMaterial struct {
	d.VisualTarget
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Available bool   `json:"available"`
}

type storyboardPartMaterial struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	AssetID   string `json:"assetId,omitempty"`
	LookID    string `json:"lookId,omitempty"`
	Label     string `json:"label,omitempty"`
	Available *bool  `json:"available,omitempty"`
}

type storyboardPromptMaterial struct {
	Parts     []storyboardPartMaterial `json:"parts"`
	Origin    string                   `json:"origin"`
	ActionRef *d.ActionRef             `json:"actionRef,omitempty"`
	storyboardValidity
}

func assetModelMaterial(p *d.Project, target d.VisualTarget) storyboardAssetMaterial {
	label, available := p.PromptLabel(target)
	v := storyboardAssetMaterial{VisualTarget: target, Label: label, Available: available}
	if a := p.Asset(target.AssetID); a != nil {
		v.Kind = a.Kind
	}
	return v
}

func storyboardEpisodeModelMaterial(p d.Project, episode d.Episode, runID string) storyboardEpisodeMaterial {
	issues := indexIssues(&p)
	v := storyboardEpisodeMaterial{
		SourceVersion: p.ScriptSource.Version,
		Episode:       episode,
		Scenes:        []storyboardSceneMaterial{},
		Shots:         []storyboardShotMaterial{},
		Index:         storyboardValidity{len(issues) == 0, issues},
	}
	for _, sceneID := range episode.SceneIDs {
		scene := p.Storyboard.Scene(sceneID)
		if scene == nil {
			continue // 沿现有只读视图跳过悬挂引用，失效仍由领域诊断提供。
		}
		issues := p.SceneIssues(scene)
		v.Scenes = append(v.Scenes, storyboardSceneMaterial{*scene, storyboardValidity{len(issues) == 0, issues}})
		for _, shotID := range scene.ShotIDs {
			shot := p.Storyboard.Shot(shotID)
			if shot == nil {
				continue
			}
			issues := p.ShotIssues(shot)
			sh := storyboardShotMaterial{
				Shot:               *shot,
				SelectedAssets:     []storyboardAssetMaterial{},
				storyboardValidity: storyboardValidity{len(issues) == 0, issues},
			}
			for _, target := range shot.SelectedAssets {
				sh.SelectedAssets = append(sh.SelectedAssets, assetModelMaterial(&p, target))
			}
			if prompt := shot.VideoPrompt; prompt != nil {
				issues := p.VideoPromptIssues(shot, runID)
				body := &storyboardPromptMaterial{
					Parts:              []storyboardPartMaterial{},
					Origin:             prompt.Origin,
					ActionRef:          prompt.ActionRef,
					storyboardValidity: storyboardValidity{len(issues) == 0, issues},
				}
				for _, part := range prompt.Parts {
					out := storyboardPartMaterial{Type: part.Type, Text: part.Text, AssetID: part.AssetID, LookID: part.LookID}
					if part.Type == "asset" {
						a := assetModelMaterial(&p, d.VisualTarget{AssetID: part.AssetID, LookID: part.LookID})
						out.Label, out.Available = a.Label, &a.Available
						if !a.Available && part.LabelAtSave != "" {
							out.Label = part.LabelAtSave + "（已删除）"
						}
					}
					body.Parts = append(body.Parts, out)
				}
				sh.VideoPrompt = body
			}
			v.Shots = append(v.Shots, sh)
		}
	}
	return v
}
