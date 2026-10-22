package httpapi

import (
	spaceapp "drama-agent/internal/application/spaces"
	"net/http"
)

func (h *Handler) registerOperationsTeams(mux *routes) {
	ops := mux.operations()
	ops.HandleFunc("GET /api/operations/teams", h.spaceRoute(func(w http.ResponseWriter, r *http.Request) {
		p, n, e := page(r)
		if e != nil {
			fail(w, e)
			return
		}
		v, e := h.Spaces.OperatorList(r.Context(), actor(r), r.URL.Query().Get("q"), p, n)
		spaceResult(w, 200, v, e)
	}))
	ops.HandleFunc("POST /api/operations/teams", h.spaceRoute(func(w http.ResponseWriter, r *http.Request) {
		var in spaceapp.TeamInput
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Spaces.Open(r.Context(), actor(r), in)
		spaceResult(w, 201, v, e)
	}))
	ops.HandleFunc("GET /api/operations/teams/{id}", h.spaceRoute(func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.OperatorDetail(r.Context(), actor(r), r.PathValue("id"))
		spaceResult(w, 200, v, e)
	}))
	ops.HandleFunc("PATCH /api/operations/teams/{id}", h.spaceRoute(func(w http.ResponseWriter, r *http.Request) {
		var in spaceapp.TeamPatch
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Spaces.OperatorUpdate(r.Context(), actor(r), r.PathValue("id"), in)
		spaceResult(w, 200, v, e)
	}))
}
