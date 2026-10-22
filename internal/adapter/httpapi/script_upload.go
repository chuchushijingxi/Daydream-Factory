package httpapi

import "net/http"

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision     uint64 `json:"revision"`
		FileID       string `json:"fileId"`
		BillingBasis string `json:"billingBasis,omitempty"`
	}
	if err := readCommand(w, r, &input); err != nil {
		fail(w, err)
		return
	}
	p, err := h.projectService(r).WithBillingBasis(input.BillingBasis).UploadFromFile(r.Context(), r.PathValue("id"), input.Revision, input.FileID)
	if err != nil {
		fail(w, err)
		return
	}
	if p.LatestRun() != nil && p.LatestRun().Status == "running" {
		h.Launch(p.ID)
	}
	write(w, 200, view(p))
}

func (h *Handler) readSource(w http.ResponseWriter, r *http.Request) {
	p, err := h.projectService(r).ReadProject(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, p.ScriptSource)
}
