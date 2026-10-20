package httpapi

import (
	"mime"
	"net/http"
	"strconv"
)

func (h *Handler) originalScript(w http.ResponseWriter, r *http.Request) {
	p, raw, err := h.Service.ForActor(actor(r)).OriginalScript(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	kind := "application/octet-stream"
	if p.OriginalFile != nil {
		kind = p.OriginalFile.ContentType
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": p.ScriptSource.Filename}))
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
