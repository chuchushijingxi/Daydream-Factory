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
	ports.ListMetadata
	SpaceID       string                   `json:"spaceId"`
	OwnerID       string                   `json:"ownerId"`
	ID            string                   `json:"id"`
	Title         string                   `json:"title"`
	Filename      string                   `json:"filename"`
	UploadedAt    *time.Time               `json:"uploadedAt"`
	Revision      uint64                   `json:"revision"`
	LatestRun     *ports.ProjectRunSummary `json:"latestRun"`
	PreviewImages []string                 `json:"previewImages"`
}

func projectSummaryView(summary ports.ProjectSummary) ProjectSummaryView {
	out := ProjectSummaryView{ListMetadata: summary.ListMetadata, SpaceID: summary.SpaceID, OwnerID: summary.OwnerID, ID: summary.ID, Title: summary.Title, Filename: summary.Filename,
		UploadedAt: summary.UploadedAt, Revision: summary.Revision, LatestRun: summary.LatestRun,
		PreviewImages: []string{}}
	for _, fileID := range summary.PreviewImageFileIDs {
		out.PreviewImages = append(out.PreviewImages, projectAssetImage(summary.ID, fileID).ThumbnailURL)
	}
	return out
}

func (h *Handler) deleteProject(w http.ResponseWriter, r *http.Request) {
	rev, err := revision(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := h.projectService(r).DeleteProject(r.Context(), r.PathValue("id"), rev)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"id": p.ID, "revision": p.Revision, "deletedAt": p.DeletedAt})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	projects, err := h.projectService(r).ListProjectsFiltered(r.Context(), ports.ListFilter(r.URL.Query().Get("filter")))
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
	// 权限在每个请求中重判，缓存正文不能保留已撤销的内容权限。
	p, err := h.projectService(r).ReadProject(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if h.Runtime != nil {
		key := "progress:" + p.SpaceID + ":" + p.ID
		raw, err := h.Runtime.ReadCached(r.Context(), key, 2*time.Second, func(context.Context) ([]byte, error) { return json.Marshal(view(p)) })
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		return
	}
	write(w, 200, view(p))
}

func (h *Handler) projectEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := h.projectService(r).ReadProject(r.Context(), r.PathValue("id")); err != nil {
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
	h.serveInvalidationEvents(w, r, updates, "project", func() error {
		// SSE 的一次连接包含多轮观察，每轮必须使用新的权限事实。
		ctx := ports.WithRequestedSpace(r.Context(), ports.RequestedSpace(r.Context()))
		_, err := h.projectService(r).ReadProject(ctx, r.PathValue("id"))
		return err
	})
}
