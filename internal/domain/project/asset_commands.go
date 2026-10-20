package project

import (
	"strings"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
)

// 资产与附加形象通过项目聚合变更，资料、归属和正文失效在同一快照内完成。
// EnsureNotFailed 只检查已保存任务状态；不依据剧情文字推断成功或失败。
func (p *Project) EnsureNotFailed() error {
	if r := p.FailedTask(); r != nil {
		return fault.New("PROJECT_FAILED", r.Error)
	}
	return nil
}

func (p *Project) ensureAssetsEditable() error {
	// 普通资料写入与任务采用分开；聚合直接调用也不能改变运行中的输入。
	if p.Active() {
		return fault.New("RUN_ACTIVE", "任务运行中")
	}
	if err := p.EnsureNotFailed(); err != nil {
		return err
	}
	if p.HasCancelledTask() || !p.CatalogReady() {
		return fault.New("ASSETS_NOT_READY", "当前源资产提取尚未完成，资产暂不可编辑")
	}
	return nil
}

func (p *Project) invalidatePrompts(assetID, lookID string, remove bool) {
	kept := make([]VisualPrompt, 0, len(p.VisualPrompts))
	for _, v := range p.VisualPrompts {
		match := v.Target.AssetID == assetID && (lookID == "" || v.Target.LookID == lookID)
		if match && remove {
			continue
		}
		if match {
			v.Stale = true
		}
		kept = append(kept, v)
	}
	p.VisualPrompts = kept
}

func (p *Project) UpdateAsset(assetID string, v AssetInput) error {
	if err := p.ensureAssetsEditable(); err != nil {
		return err
	}
	if err := v.Validate(); err != nil {
		return err
	}
	a := p.Asset(assetID)
	if assetID != "" && a == nil {
		return fault.ErrNotFound
	}
	if a == nil {
		p.Assets = append(p.Assets, Asset{ID: identity.New("asset"), SourceIDs: []string{}, Looks: []Look{}})
		a = &p.Assets[len(p.Assets)-1]
	} else {
		if a.Kind != v.Kind {
			return fault.ErrInvalid
		}
		p.invalidatePrompts(a.ID, "", false)
	}
	a.Kind, a.Name, a.Aliases, a.Description, a.Character, a.Origin = v.Kind, strings.TrimSpace(v.Name), append([]string{}, v.Aliases...), v.Description, v.Character, "user"
	p.CatalogEditedByUser = true
	return nil
}

func (p *Project) RemoveAsset(assetID string) error {
	if err := p.ensureAssetsEditable(); err != nil {
		return err
	}
	if p.Asset(assetID) == nil {
		return fault.ErrNotFound
	}
	out := []Asset{}
	for _, a := range p.Assets {
		if a.ID != assetID {
			out = append(out, a)
		}
	}
	p.Assets = out
	p.invalidatePrompts(assetID, "", true)
	p.CatalogEditedByUser = true
	return nil
}

func (p *Project) UpdateLook(assetID, lookID string, v LookInput) error {
	if err := p.ensureAssetsEditable(); err != nil {
		return err
	}
	a := p.Asset(assetID)
	if a == nil {
		return fault.ErrNotFound
	}
	if a.Kind != "character" || strings.TrimSpace(v.Name) == "" {
		return fault.ErrInvalid
	}
	var l *Look
	if lookID == "" {
		a.Looks = append(a.Looks, Look{ID: identity.New("look")})
		l = &a.Looks[len(a.Looks)-1]
	} else {
		for i := range a.Looks {
			if a.Looks[i].ID == lookID {
				l = &a.Looks[i]
			}
		}
		if l == nil {
			return fault.ErrNotFound
		}
		p.invalidatePrompts(assetID, lookID, false)
	}
	l.Name, l.Description, l.Origin = strings.TrimSpace(v.Name), v.Description, "user"
	p.CatalogEditedByUser = true
	return nil
}

func (p *Project) RemoveLook(assetID, lookID string) error {
	if err := p.ensureAssetsEditable(); err != nil {
		return err
	}
	a := p.Asset(assetID)
	if a == nil {
		return fault.ErrNotFound
	}
	if a.Kind != "character" {
		return fault.ErrInvalid
	}
	out := []Look{}
	found := false
	for _, l := range a.Looks {
		if l.ID == lookID {
			found = true
		} else {
			out = append(out, l)
		}
	}
	if !found {
		return fault.ErrNotFound
	}
	a.Looks = out
	p.invalidatePrompts(assetID, lookID, true)
	p.CatalogEditedByUser = true
	return nil
}
