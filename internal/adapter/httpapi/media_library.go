package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"drama-agent/internal/application/medialibrary"
	"drama-agent/internal/domain/fault"
)

func libraryLinks(v medialibrary.Item) medialibrary.Item {
	base := "/api/projects/" + url.PathEscape(v.ProjectID) + "/media-tasks/" + url.PathEscape(v.TaskID) + "/outputs/" + url.PathEscape(v.OutputID)
	v.ContentURL = base + "/file"
	v.ThumbnailURL = base + "/thumbnail"
	if v.Kind == "image" {
		v.ThumbnailURL = ""
		if v.FileID != "" {
			v.ThumbnailURL = "/api/files/" + url.PathEscape(v.FileID) + "/thumbnail"
		}
	}
	return v
}
func (h *Handler) registerMediaLibrary(mux *routes) {
	mux.HandleFunc("GET /api/media-library", func(w http.ResponseWriter, r *http.Request) {
		limit := 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			var err error
			limit, err = strconv.Atoi(raw)
			if err != nil || limit == 0 {
				fail(w, fault.ErrInvalid)
				return
			}
		}
		v, err := h.MediaLibrary.List(r.Context(), actor(r), r.URL.Query().Get("kind"), r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), limit)
		if err != nil {
			fail(w, err)
			return
		}
		for i := range v.Items {
			v.Items[i] = libraryLinks(v.Items[i])
		}
		write(w, 200, v)
	})
	mux.HandleFunc("GET /api/media-library/{taskId}/outputs/{outputId}", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.MediaLibrary.Detail(r.Context(), actor(r), r.PathValue("taskId"), r.PathValue("outputId"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, libraryLinks(v))
	})
}
