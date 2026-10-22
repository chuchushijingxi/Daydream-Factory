package httpapi

import (
	"bytes"
	"crypto/sha256"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/fault"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func mediaLinks(v app.MediaView) app.MediaView {
	if v.Result != nil {
		for i := range v.Result.Files {
			f := &v.Result.Files[i]
			f.ContentURL = "/api/projects/" + v.ProjectID + "/media-tasks/" + v.ID + "/outputs/" + f.ID + "/file"
			f.ThumbnailURL = ""
			if v.Status == "succeeded" && (f.ContentType == "video/mp4" || strings.HasPrefix(f.ContentType, "image/")) {
				f.ThumbnailURL = "/api/projects/" + v.ProjectID + "/media-tasks/" + v.ID + "/outputs/" + f.ID + "/thumbnail"
			}
		}
	}
	return v
}
func (h *Handler) registerProjectMedia(mux *routes) {
	mux.HandleFunc("POST /api/projects/{id}/media-tasks/batch-images", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Commands []app.MediaCommand `json:"commands"`
		}
		if _, err := readJSONBody(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		items, err := h.projectService(r).StartImageBatch(r.Context(), r.PathValue("id"), in.Commands)
		if err != nil {
			fail(w, err)
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			row := map[string]any{"commandId": item.CommandID, "target": item.Target, "task": nil, "error": nil}
			if item.Task != nil {
				row["task"] = mediaLinks(*item.Task)
			}
			if item.Err != nil {
				_, row["error"] = errorResponse(item.Err)
			}
			out = append(out, row)
		}
		write(w, 202, map[string]any{"items": out})
	})
	mux.HandleFunc("GET /api/projects/{id}/media-tasks/{taskId}/outputs/{mediaId}/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		body, err := h.projectService(r).MediaThumbnail(r.Context(), r.PathValue("id"), r.PathValue("taskId"), r.PathValue("mediaId"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("ETag", fmt.Sprintf("\"%x\"", sha256.Sum256(body)))
		http.ServeContent(w, r, "thumbnail.webp", time.Time{}, bytes.NewReader(body))
	})
	mux.HandleFunc("POST /api/projects/{id}/media-tasks", func(w http.ResponseWriter, r *http.Request) {
		var in app.MediaCommand
		if err := readCommand(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		v, err := h.projectService(r).StartMedia(r.Context(), r.PathValue("id"), in)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 202, mediaLinks(v))
	})
	mux.HandleFunc("GET /api/projects/{id}/media-tasks", func(w http.ResponseWriter, r *http.Request) {
		limit := 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			var e error
			limit, e = strconv.Atoi(raw)
			if e != nil {
				fail(w, fault.ErrInvalid)
				return
			}
		}
		v, err := h.projectService(r).ListMedia(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"), limit, r.URL.Query().Get("kind"))
		if err != nil {
			fail(w, err)
			return
		}
		for i := range v.Items {
			v.Items[i] = mediaLinks(v.Items[i])
		}
		write(w, 200, v)
	})
	mux.HandleFunc("GET /api/projects/{id}/media-tasks/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.projectService(r).Media(r.Context(), r.PathValue("id"), r.PathValue("taskId"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, mediaLinks(v))
	})
	mux.HandleFunc("POST /api/projects/{id}/media-tasks/{taskId}/cancel", func(w http.ResponseWriter, r *http.Request) {
		rev, err := revision(w, r)
		if err != nil {
			fail(w, err)
			return
		}
		v, err := h.projectService(r).CancelMedia(r.Context(), r.PathValue("id"), r.PathValue("taskId"), rev)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, mediaLinks(v))
	})
	mux.HandleFunc("POST /api/projects/{id}/media-tasks/{taskId}/adopt", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision uint64 `json:"revision"`
			OutputID string `json:"outputId"`
		}
		if err := readCommand(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		p, err := h.projectService(r).AdoptMedia(r.Context(), r.PathValue("id"), r.PathValue("taskId"), in.OutputID, in.Revision)
		respond(w, p, err, 200)
	})
	mux.HandleFunc("GET /api/projects/{id}/media-tasks/{taskId}/outputs/{mediaId}/file", func(w http.ResponseWriter, r *http.Request) {
		body, meta, err := h.projectService(r).MediaFile(r.Context(), r.PathValue("id"), r.PathValue("taskId"), r.PathValue("mediaId"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", meta.ContentType)
		w.Header().Set("ETag", `"`+meta.SHA256+`"`)
		http.ServeContent(w, r, meta.ID, time.Time{}, bytes.NewReader(body))
	})
}
