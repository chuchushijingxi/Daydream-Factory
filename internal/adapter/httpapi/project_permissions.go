package httpapi

import "net/http"

func (h *Handler) registerProjectPermissions(mux *routes) {
	mux.HandleFunc("GET /api/projects/{id}/permissions", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.projectService(r).AccessInfo(r.Context(), r.PathValue("id"))
		spaceResult(w, 200, v, err)
	})
}
