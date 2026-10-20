package project

import d "drama-agent/internal/domain/project"

// CatalogAssetInput 固定提取协议的可写范围，资产管理字段不进入模型 schema。
// schema 与解析共用此类型；映射到领域实体时不补事实、不按名称合并。
type CatalogAssetInput struct {
	ID          string   `json:"id,omitempty" jsonschema:"nullable"`
	Kind        string   `json:"kind" jsonschema:"enum=character,enum=location,enum=prop"`
	Name        string   `json:"name" jsonschema:"minLength=1"`
	Aliases     []string `json:"aliases,omitempty" jsonschema:"nullable"`
	Description string   `json:"description,omitempty" jsonschema:"nullable"`
	SourceIDs   []string `json:"sourceIds" jsonschema:"minItems=1"`
}

type CatalogChangeSet struct {
	Assets    []CatalogAssetInput `json:"assets"`
	RemoveIDs []string            `json:"removeIds,omitempty" jsonschema:"nullable"`
}

func (draft CatalogChangeSet) DomainAssets() []d.Asset {
	assets := make([]d.Asset, 0, len(draft.Assets))
	for _, a := range draft.Assets {
		assets = append(assets, d.Asset{ID: a.ID, Kind: a.Kind, Name: a.Name, Aliases: a.Aliases, Description: a.Description, SourceIDs: a.SourceIDs})
	}
	return assets
}
