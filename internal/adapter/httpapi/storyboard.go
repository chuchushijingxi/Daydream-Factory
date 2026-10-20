package httpapi

import (
	"encoding/json"
	"net/http"

	app "drama-agent/internal/application/project"
)

func (h *Handler) storyboardAccess(v *app.StoryboardBaseView) {
	if h.ReadOnly {
		v.Access = app.StoryboardAccess{Reason: "SERVICE_READ_ONLY"}
	}
}
func (h *Handler) registerStoryboard(mux *routes) {
	mux.HandleFunc("GET /api/projects/{id}/storyboard", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Service.ForActor(actor(r)).ReadStoryboard(r.Context(), r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		h.storyboardAccess(&v.StoryboardBaseView)
		write(w, 200, v)
	})
	mux.HandleFunc("GET /api/projects/{id}/storyboard/episodes/{episodeId}", func(w http.ResponseWriter, r *http.Request) {
		p, e := h.Service.ForActor(actor(r)).ReadProject(r.Context(), r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		v, e := h.Service.ForActor(actor(r)).StoryboardEpisode(p, r.PathValue("episodeId"))
		if e == nil {
			v, e = h.Service.ForActor(actor(r)).EpisodePreviews(r.Context(), p, v)
		}
		if e != nil {
			fail(w, e)
			return
		}
		h.storyboardAccess(&v.StoryboardBaseView)
		write(w, 200, v)
	})
	mux.HandleFunc("GET /api/projects/{id}/storyboard/episodes/{episodeId}/source", func(w http.ResponseWriter, r *http.Request) {
		p, e := h.Service.ForActor(actor(r)).ReadProject(r.Context(), r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		v, e := h.Service.ForActor(actor(r)).StoryboardSource(p, r.PathValue("episodeId"))
		if e != nil {
			fail(w, e)
			return
		}
		h.storyboardAccess(&v.StoryboardBaseView)
		write(w, 200, v)
	})
	mux.HandleFunc("POST /api/projects/{id}/storyboard/episode-index-runs", func(w http.ResponseWriter, r *http.Request) {
		rev, basis, e := generationRevision(w, r)
		if e != nil {
			fail(w, e)
			return
		}
		p, e := h.Service.ForActor(actor(r)).WithBillingBasis(basis).RetryEpisodeIndex(r.Context(), r.PathValue("id"), rev)
		if e != nil {
			fail(w, e)
			return
		}
		h.Launch(p.ID)
		write(w, 202, h.Service.ForActor(actor(r)).StoryboardIndex(p))
	})
	mux.HandleFunc("POST /api/projects/{id}/storyboard/episodes/{episodeId}/runs", func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if _, e := readJSONBody(w, r, &raw); e != nil {
			fail(w, e)
			return
		}
		var envelope map[string]json.RawMessage
		if e := json.Unmarshal(raw, &envelope); e != nil {
			fail(w, e)
			return
		}
		var basis string
		if value, ok := envelope["billingBasis"]; ok {
			if e := json.Unmarshal(value, &basis); e != nil {
				fail(w, e)
				return
			}
			delete(envelope, "billingBasis")
		}
		raw, _ = json.Marshal(envelope)
		var in app.EpisodeRunInput
		if e := app.DecodeStoryboard(raw, "EpisodeRunInput", &in); e != nil {
			fail(w, e)
			return
		}
		p, e := h.Service.ForActor(actor(r)).WithBillingBasis(basis).StartEpisode(r.Context(), r.PathValue("id"), r.PathValue("episodeId"), in)
		if e != nil {
			fail(w, e)
			return
		}
		h.Launch(p.ID)
		v, e := h.Service.ForActor(actor(r)).StoryboardEpisode(p, r.PathValue("episodeId"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 202, v)
	})
	mux.HandleFunc("POST /api/projects/{id}/storyboard/changes", func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if _, e := readJSONBody(w, r, &raw); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Service.ForActor(actor(r)).ChangeStoryboard(r.Context(), r.PathValue("id"), raw)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
}
