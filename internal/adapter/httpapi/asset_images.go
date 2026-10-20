package httpapi

import (
	d "drama-agent/internal/domain/project"
	"net/http"
)

type AssetImageView struct {
	FileID       string `json:"fileId"`
	OriginalURL  string `json:"originalUrl"`
	ThumbnailURL string `json:"thumbnailUrl"`
}

func assetImage(fileID string) *AssetImageView {
	if fileID == "" {
		return nil
	}
	base := "/api/files/" + fileID
	return &AssetImageView{FileID: fileID, OriginalURL: base + "/original", ThumbnailURL: base + "/thumbnail"}
}

type LookView struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Origin      string          `json:"origin"`
	Image       *AssetImageView `json:"image"`
}

func (h *Handler) registerAssetImages(mux *routes) {
	for _, pattern := range []string{"POST /api/projects/{id}/assets/{assetId}/image", "POST /api/projects/{id}/assets/{assetId}/looks/{lookId}/image"} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Revision uint64 `json:"revision"`
				FileID   string `json:"fileId"`
			}
			if err := readCommand(w, r, &body); err != nil {
				fail(w, err)
				return
			}
			target := d.VisualTarget{AssetID: r.PathValue("assetId"), LookID: r.PathValue("lookId")}
			p, err := h.Service.ForActor(actor(r)).SaveImage(r.Context(), r.PathValue("id"), body.Revision, target, body.FileID)
			respond(w, p, err, http.StatusOK)
		})
	}
}
