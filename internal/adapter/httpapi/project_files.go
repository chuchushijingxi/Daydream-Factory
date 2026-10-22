package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

func (h *Handler) registerProjectFiles(mux *routes) {
	mux.HandleFunc("GET /api/projects/{id}/files/{fileId}", func(w http.ResponseWriter, r *http.Request) {
		file, err := h.projectService(r).ReferencedFile(r.Context(), r.PathValue("id"), r.PathValue("fileId"))
		if err != nil {
			fail(w, err)
			return
		}
		v := fileView(file)
		base := "/api/projects/" + url.PathEscape(r.PathValue("id")) + "/files/" + url.PathEscape(file.ID)
		v.OriginalURL = base + "/original"
		if v.ThumbnailURL != nil {
			thumbnail := base + "/thumbnail"
			v.ThumbnailURL = &thumbnail
		}
		write(w, 200, v)
	})
	serve := func(w http.ResponseWriter, r *http.Request) {
		thumbnail := strings.HasSuffix(r.URL.Path, "/thumbnail")
		file, raw, err := h.projectService(r).ReferencedFileBytes(r.Context(), r.PathValue("id"), r.PathValue("fileId"), thumbnail)
		if err != nil {
			fail(w, err)
			return
		}
		serveUploadedContent(w, r, file, raw, thumbnail)
	}
	mux.HandleFunc("GET /api/projects/{id}/files/{fileId}/original", serve)
	mux.HandleFunc("GET /api/projects/{id}/files/{fileId}/thumbnail", serve)
}
