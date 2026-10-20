package httpapi

import (
	"context"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"encoding/json"
	"net/http"
	"time"
)

type ProjectSummaryView struct {
	ID            string                   `json:"id"`
	Title         string                   `json:"title"`
	Filename      string                   `json:"filename"`
	UploadedAt    *time.Time               `json:"uploadedAt"`
	Revision      uint64                   `json:"revision"`
	LatestRun     *ports.ProjectRunSummary `json:"latestRun"`
	PreviewImages []string                 `json:"previewImages"`
}

func projectSummaryView(summary ports.ProjectSummary) ProjectSummaryView {
	out := ProjectSummaryView{ID: summary.ID, Title: summary.Title, Filename: summary.Filename,
		UploadedAt: summary.UploadedAt, Revision: summary.Revision, LatestRun: summary.LatestRun,
		PreviewImages: []string{}}
	for _, fileID := range summary.PreviewImageFileIDs {
		out.PreviewImages = append(out.PreviewImages, assetImage(fileID).ThumbnailURL)
	}
	return out
}

func (h *Handler) deleteProject(w http.ResponseWriter, r *http.Request) {
	rev, err := revision(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := h.Service.ForActor(actor(r)).DeleteProject(r.Context(), r.PathValue("id"), rev)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"id": p.ID, "revision": p.Revision, "deletedAt": p.DeletedAt})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	projects, err := h.Service.ForActor(actor(r)).ListProjects(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]ProjectSummaryView, 0, len(projects))
	for _, p := range projects {
		out = append(out, projectSummaryView(p))
	}
	write(w, 200, out)
}

func (h *Handler) project(w http.ResponseWriter, r *http.Request) {
	if h.Runtime != nil {
		key := "progress:" + actor(r).UserID + ":" + r.PathValue("id")
		raw, err := h.Runtime.ReadCached(r.Context(), key, 2*time.Second, func(ctx context.Context) ([]byte, error) {
			p, err := h.Service.ForActor(actor(r)).ReadProject(ctx, r.PathValue("id"))
			if err != nil {
				return nil, err
			}
			return json.Marshal(view(p))
		})
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		return
	}
	p, err := h.Service.ForActor(actor(r)).ReadProject(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, view(p))
}

func (h *Handler) projectEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := h.Service.ForActor(actor(r)).ReadProject(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	if h.Runtime == nil {
		fail(w, fault.New("RUNTIME_UNAVAILABLE", "Redis运行状态尚未组装"))
		return
	}
	updates, closeWatch, err := h.Runtime.Watch(r.Context(), "project:"+r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	defer closeWatch()
	h.serveInvalidationEvents(w, r, updates, "project")
}
