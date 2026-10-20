package project

import d "drama-agent/internal/domain/project"

// 文本模型输入保持现有合同；上传图片身份是产品状态，
// 不进入剧本理解或提示词生成。
func textAsset(asset d.Asset) d.Asset {
	asset.ImageFileID = ""
	if asset.Looks != nil {
		asset.Looks = append([]d.Look{}, asset.Looks...)
		for i := range asset.Looks {
			asset.Looks[i].ImageFileID = ""
		}
	}
	return asset
}
