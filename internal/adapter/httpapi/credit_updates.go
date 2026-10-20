package httpapi

import (
	"net/http"

	"drama-agent/internal/domain/fault"
)

func (h *Handler) creditEvents(w http.ResponseWriter, r *http.Request) {
	// ServeMux GET patterns also match HEAD; never open a stream for HEAD.
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.Credits == nil {
		fail(w, fault.New("CREDIT_UPDATES_UNAVAILABLE", "积分实时同步暂不可用"))
		return
	}
	updates, closeWatch, err := h.Credits.Watch(r.Context(), actor(r), r.PathValue("userId"))
	if err != nil {
		fail(w, err)
		return
	}
	defer closeWatch()
	h.serveInvalidationEvents(w, r, updates, "credits")
}
