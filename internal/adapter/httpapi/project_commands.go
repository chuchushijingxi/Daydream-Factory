package httpapi

import (
	"net/http"
	"strings"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func (h *Handler) registerProjectCommands(mux *routes) {
	mux.HandleFunc("GET /api/models", func(w http.ResponseWriter, r *http.Request) {
		options, err := h.Service.ForActor(actor(r)).AvailableModels(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, options)
	})
	mux.HandleFunc("GET /api/visual-styles", func(w http.ResponseWriter, r *http.Request) {
		if h.Service.Styles == nil {
			fail(w, fault.ErrModelUnavailable)
			return
		}
		write(w, 200, map[string]any{"styles": h.Service.Styles})
	})
	mux.HandleFunc("POST /api/projects/{id}/settings", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Revision        uint64            `json:"revision"`
			ProjectSettings d.ProjectSettings `json:"settings"`
		}
		if err := readCommand(w, r, &body); err != nil {
			fail(w, err)
			return
		}
		p, err := h.Service.ForActor(actor(r)).SaveSettings(r.Context(), r.PathValue("id"), body.Revision, body.ProjectSettings)
		respond(w, p, err, 200)
	})
	mux.HandleFunc("POST /api/projects/{id}/style", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Revision uint64 `json:"revision"`
			StyleID  string `json:"styleId"`
			Version  string `json:"promptVersion"`
		}
		if err := readCommand(w, r, &body); err != nil {
			fail(w, err)
			return
		}
		p, err := h.Service.ForActor(actor(r)).SaveStyle(r.Context(), r.PathValue("id"), body.Revision, body.StyleID, body.Version)
		respond(w, p, err, 200)
	})
	for _, pattern := range []string{"POST /api/projects/{id}/assets", "POST /api/projects/{id}/assets/{assetId}"} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Revision uint64       `json:"revision"`
				Asset    d.AssetInput `json:"asset"`
			}
			if err := readCommand(w, r, &body); err != nil {
				fail(w, err)
				return
			}
			p, err := h.Service.ForActor(actor(r)).SaveAsset(r.Context(), r.PathValue("id"), body.Revision, r.PathValue("assetId"), body.Asset)
			status := 200
			if r.PathValue("assetId") == "" {
				status = 201
			}
			respond(w, p, err, status)
		})
	}
	mux.HandleFunc("POST /api/projects/{id}/assets/{assetId}/delete", func(w http.ResponseWriter, r *http.Request) {
		rev, err := revision(w, r)
		if err != nil {
			fail(w, err)
			return
		}
		p, err := h.Service.ForActor(actor(r)).DeleteAsset(r.Context(), r.PathValue("id"), rev, r.PathValue("assetId"))
		respond(w, p, err, 200)
	})
	for _, pattern := range []string{"POST /api/projects/{id}/assets/{assetId}/looks", "POST /api/projects/{id}/assets/{assetId}/looks/{lookId}"} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Revision uint64      `json:"revision"`
				Look     d.LookInput `json:"look"`
			}
			if err := readCommand(w, r, &body); err != nil {
				fail(w, err)
				return
			}
			p, err := h.Service.ForActor(actor(r)).SaveLook(r.Context(), r.PathValue("id"), body.Revision, r.PathValue("assetId"), r.PathValue("lookId"), body.Look)
			status := 200
			if r.PathValue("lookId") == "" {
				status = 201
			}
			respond(w, p, err, status)
		})
	}
	mux.HandleFunc("POST /api/projects/{id}/assets/{assetId}/looks/{lookId}/delete", func(w http.ResponseWriter, r *http.Request) {
		rev, err := revision(w, r)
		if err != nil {
			fail(w, err)
			return
		}
		p, err := h.Service.ForActor(actor(r)).DeleteLook(r.Context(), r.PathValue("id"), rev, r.PathValue("assetId"), r.PathValue("lookId"))
		respond(w, p, err, 200)
	})
	mux.HandleFunc("POST /api/projects/{id}/outline-runs", h.startContentTask)
	for _, pattern := range []string{"POST /api/projects/{id}/assets/{assetId}/visual-prompt", "POST /api/projects/{id}/assets/{assetId}/looks/{lookId}/visual-prompt"} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Revision uint64 `json:"revision"`
				Text     string `json:"text"`
			}
			if err := readCommand(w, r, &body); err != nil {
				fail(w, err)
				return
			}
			p, err := h.Service.ForActor(actor(r)).SaveVisualPrompt(r.Context(), r.PathValue("id"), body.Revision, d.VisualTarget{AssetID: r.PathValue("assetId"), LookID: r.PathValue("lookId")}, body.Text)
			respond(w, p, err, 200)
		})
	}
	mux.HandleFunc("POST /api/projects/{id}/visual-prompt-runs", h.startContentTask)
	mux.HandleFunc("POST /api/projects/{id}/assets/{assetId}/visual-prompt-runs", h.startContentTask)
	mux.HandleFunc("POST /api/projects/{id}/assets/{assetId}/looks/{lookId}/visual-prompt-runs", h.startContentTask)
}

func (h *Handler) startContentTask(w http.ResponseWriter, r *http.Request) {
	rev, basis, err := generationRevision(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	var p d.Project
	if strings.HasSuffix(r.URL.Path, "/outline-runs") {
		p, err = h.Service.ForActor(actor(r)).WithBillingBasis(basis).StartOutline(r.Context(), r.PathValue("id"), rev)
	} else if r.PathValue("assetId") != "" {
		p, err = h.Service.ForActor(actor(r)).WithBillingBasis(basis).StartSingleVisual(r.Context(), r.PathValue("id"), rev, d.VisualTarget{AssetID: r.PathValue("assetId"), LookID: r.PathValue("lookId")})
	} else {
		p, err = h.Service.ForActor(actor(r)).WithBillingBasis(basis).StartVisual(r.Context(), r.PathValue("id"), rev)
	}
	if err != nil {
		fail(w, err)
		return
	}
	status := 200
	if p.LatestRun().Status == "running" {
		status = 202
		h.Launch(p.ID)
	}
	write(w, status, view(p))
}
