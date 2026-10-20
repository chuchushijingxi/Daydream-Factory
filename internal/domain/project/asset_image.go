package project

import "drama-agent/internal/domain/fault"

func (p *Project) imageTarget(target VisualTarget) (*string, error) {
	if !p.CatalogReady() {
		return nil, fault.New("ASSETS_NOT_READY", "资产提取尚未完成")
	}
	for _, r := range p.Runs {
		if r.Status == RunRunning && r.ExecutionContract() != StoryboardContract {
			return nil, fault.New("RUN_ACTIVE", "资产任务运行中")
		}
	}
	if err := p.ValidateTarget(target); err != nil {
		return nil, err
	}
	a := p.Asset(target.AssetID)
	if a == nil {
		return nil, fault.ErrNotFound
	}
	if target.LookID == "" {
		return &a.ImageFileID, nil
	}
	if a.Kind != "character" {
		return nil, fault.ErrNotFound
	}
	for i := range a.Looks {
		if a.Looks[i].ID == target.LookID {
			return &a.Looks[i].ImageFileID, nil
		}
	}
	return nil, fault.ErrNotFound
}

func (p *Project) ValidateImageTarget(target VisualTarget) error {
	_, err := p.imageTarget(target)
	return err
}

// 图片关联是用户选择；它不改写事实，也不使用图片文件的文本正文失效。
func (p *Project) SetImage(target VisualTarget, fileID string) error {
	field, err := p.imageTarget(target)
	if err != nil {
		return err
	}
	if fileID == "" {
		return fault.ErrInvalid
	}
	*field = fileID
	p.CatalogEditedByUser = true
	return nil
}
