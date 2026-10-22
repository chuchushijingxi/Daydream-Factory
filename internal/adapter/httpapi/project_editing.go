package httpapi

import "net/http"

func (h *Handler) registerProjectEditing(mux *routes) {
	mux.HandleFunc("GET /api/projects/{id}/permissions", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.projectService(r).AccessInfo(r.Context(), r.PathValue("id"))
		spaceResult(w, 200, v, err)
	})
	mux.HandleFunc("POST /api/projects/{id}/editing", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.projectService(r).AcquireEditing(r.Context(), r.PathValue("id"))
		spaceResult(w, 200, v, err)
	})
	mux.HandleFunc("PUT /api/projects/{id}/editing", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.projectService(r).RenewEditing(r.Context(), r.PathValue("id"))
		spaceResult(w, 200, v, err)
	})
	mux.HandleFunc("DELETE /api/projects/{id}/editing", func(w http.ResponseWriter, r *http.Request) {
		err := h.projectService(r).ReleaseEditing(r.Context(), r.PathValue("id"))
		spaceResult(w, 200, map[string]bool{"released": true}, err)
	})
}
