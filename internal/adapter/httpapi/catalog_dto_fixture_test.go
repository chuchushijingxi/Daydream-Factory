package httpapi_test

import (
	app "drama-agent/internal/application/project"
	d "drama-agent/internal/domain/project"
)

func catalogInputs(assets []d.Asset) []app.CatalogAssetInput {
	out := make([]app.CatalogAssetInput, 0, len(assets))
	for _, a := range assets {
		out = append(out, app.CatalogAssetInput{ID: a.ID, Kind: a.Kind, Name: a.Name, Aliases: a.Aliases, Description: a.Description, SourceIDs: a.SourceIDs})
	}
	return out
}
