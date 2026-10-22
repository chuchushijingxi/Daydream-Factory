package httpapi

import (
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"encoding/json"
	"net/http"
)

func (h *Handler) registerChargeQuotes(mux *routes) {
	mux.HandleFunc("POST /api/projects/quote", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			FileID   string            `json:"fileId"`
			Settings d.ProjectSettings `json:"settings"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.projectService(r).QuoteFile(r.Context(), in.FileID, in.Settings)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("POST /api/projects/{id}/quote", func(w http.ResponseWriter, r *http.Request) {
		var in app.QuoteInput
		raw, e := readJSONBody(w, r, &in)
		if e != nil {
			fail(w, e)
			return
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if in.Revision == 0 || fields["breakdownMode"] != nil && (in.Operation != "breakdown" || in.BreakdownMode == "") {
			fail(w, fault.ErrInvalid)
			return
		}
		v, e := h.projectService(r).Quote(r.Context(), r.PathValue("id"), in)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
}
