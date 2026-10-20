package httpapi

import (
	"net/http"

	d "drama-agent/internal/domain/project"
)

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	rev, basis, err := generationRevision(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := h.Service.ForActor(actor(r)).WithBillingBasis(basis).Start(r.Context(), r.PathValue("id"), rev)
	if err != nil {
		fail(w, err)
		return
	}
	h.Launch(p.ID)
	write(w, 202, view(p))
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	rev, err := revision(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := h.Service.ForActor(actor(r)).Cancel(r.Context(), r.PathValue("id"), rev)
	if err != nil {
		if p.ID != "" {
			current, readErr := h.Service.ForActor(actor(r)).Project(r.Context(), p.ID)
			if readErr == nil && current.LatestRun() != nil && current.LatestRun().Status == d.RunCancelled {
				h.CancelWorker(p.ID)
			}
		}
		fail(w, err)
		return
	}
	h.CancelWorker(p.ID)
	write(w, 200, view(p))
}
