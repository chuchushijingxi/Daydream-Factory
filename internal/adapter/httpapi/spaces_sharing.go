package httpapi

import (
	"net/http"

	spaceapp "drama-agent/internal/application/spaces"
)

func (h *Handler) registerSpaceSharing(mux *routes) {
	mux.HandleFunc("GET /api/spaces/{id}/shares/{kind}/{resourceId}", h.spaceRoute(func(w http.ResponseWriter, r *http.Request) {
		v, err := h.Spaces.Sharing(r.Context(), actor(r), r.PathValue("id"), r.PathValue("kind"), r.PathValue("resourceId"))
		spaceResult(w, 200, v, err)
	}))
	mux.HandleFunc("PUT /api/spaces/{id}/shares/{kind}/{resourceId}", h.spaceRoute(func(w http.ResponseWriter, r *http.Request) {
		var in spaceapp.ShareInput
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		v, err := h.Spaces.PutShare(r.Context(), actor(r), r.PathValue("id"), r.PathValue("kind"), r.PathValue("resourceId"), in)
		spaceResult(w, 200, v, err)
	}))
	mux.HandleFunc("DELETE /api/spaces/{id}/shares/{kind}/{resourceId}", h.spaceRoute(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Grantee string `json:"grantee"`
		}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		err := h.Spaces.DeleteShare(r.Context(), actor(r), r.PathValue("id"), r.PathValue("kind"), r.PathValue("resourceId"), in.Grantee)
		spaceResult(w, 200, map[string]bool{"removed": true}, err)
	}))
}
