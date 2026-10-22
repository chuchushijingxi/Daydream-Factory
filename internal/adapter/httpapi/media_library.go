package httpapi

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"drama-agent/internal/application/medialibrary"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

func libraryLinks(v medialibrary.Item) medialibrary.Item {
	base := "/api/media-library/" + url.PathEscape(v.TaskID) + "/outputs/" + url.PathEscape(v.OutputID)
	v.ContentURL = base + "/file"
	v.ThumbnailURL = base + "/thumbnail"
	if v.Kind == "image" && v.FileID == "" {
		v.ThumbnailURL = ""
	}
	return v
}
func (h *Handler) registerMediaLibrary(mux *routes) {
	mux.HandleFunc("GET /api/media-library/{taskId}/outputs/{outputId}/file", func(w http.ResponseWriter, r *http.Request) {
		body, meta, err := h.MediaLibrary.ForScope(requestScope(r)).File(r.Context(), r.PathValue("taskId"), r.PathValue("outputId"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", meta.ContentType)
		w.Header().Set("ETag", `"`+meta.SHA256+`"`)
		w.Header().Set("Content-Security-Policy", "sandbox")
		http.ServeContent(w, r, meta.ID, time.Time{}, bytes.NewReader(body))
	})
	mux.HandleFunc("GET /api/media-library/{taskId}/outputs/{outputId}/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		body, err := h.MediaLibrary.ForScope(requestScope(r)).Thumbnail(r.Context(), r.PathValue("taskId"), r.PathValue("outputId"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("ETag", fmt.Sprintf("\"%x\"", sha256.Sum256(body)))
		http.ServeContent(w, r, "thumbnail.webp", time.Time{}, bytes.NewReader(body))
	})
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
		v, err := h.MediaLibrary.ForScope(requestScope(r)).List(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("q"), ports.ListFilter(r.URL.Query().Get("filter")), r.URL.Query().Get("cursor"), limit)
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
		v, err := h.MediaLibrary.ForScope(requestScope(r)).Detail(r.Context(), r.PathValue("taskId"), r.PathValue("outputId"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, libraryLinks(v))
	})
}
