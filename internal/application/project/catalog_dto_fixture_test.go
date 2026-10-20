package project

import (
	d "drama-agent/internal/domain/project"
)

func catalogInputs(assets []d.Asset) []CatalogAssetInput {
	out := make([]CatalogAssetInput, 0, len(assets))
	for _, a := range assets {
		out = append(out, CatalogAssetInput{ID: a.ID, Kind: a.Kind, Name: a.Name, Aliases: a.Aliases, Description: a.Description, SourceIDs: a.SourceIDs})
	}
	return out
}
func adoptTestDraft(draft CatalogChangeSet, current []d.Asset, source d.ScriptSource) ([]d.Asset, error) {
	return d.AdoptCatalogChanges(draft.DomainAssets(), draft.RemoveIDs, current, source)
}
