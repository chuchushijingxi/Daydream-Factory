package httpapi

import (
	"net/http"
	"strconv"

	spaceapp "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
)

func spaceResult(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	write(w, status, value)
}
func (h *Handler) spaceRoute(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Spaces == nil {
			fail(w, fault.New("SPACE_UNAVAILABLE", "空间服务尚未配置"))
			return
		}
		f(w, r)
	}
}
func (h *Handler) registerSpaces(mux *routes) {
	handle := func(path string, f http.HandlerFunc) { mux.HandleFunc(path, h.spaceRoute(f)) }
	handle("GET /api/spaces", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.List(r.Context(), actor(r))
		spaceResult(w, 200, v, e)
	})
	handle("GET /api/spaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.Detail(r.Context(), actor(r), r.PathValue("id"))
		spaceResult(w, 200, v, e)
	})
	handle("PATCH /api/spaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in spaceapp.SettingsInput
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Spaces.Update(r.Context(), actor(r), r.PathValue("id"), in)
		spaceResult(w, 200, v, e)
	})
	handle("GET /api/spaces/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.Members(r.Context(), actor(r), r.PathValue("id"))
		spaceResult(w, 200, v, e)
	})
	handle("DELETE /api/spaces/{id}/members/{userId}", func(w http.ResponseWriter, r *http.Request) {
		e := h.Spaces.RemoveMember(r.Context(), actor(r), r.PathValue("id"), r.PathValue("userId"))
		spaceResult(w, 200, map[string]bool{"removed": true}, e)
	})
	handle("POST /api/spaces/{id}/members/{userId}/role", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Role spaces.Role `json:"role"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		e := h.Spaces.SetRole(r.Context(), actor(r), r.PathValue("id"), r.PathValue("userId"), in.Role)
		spaceResult(w, 200, map[string]bool{"updated": true}, e)
	})
	handle("POST /api/spaces/{id}/members/{userId}/allocation", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Kind string `json:"kind"`
			spaceapp.AllocationInput
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		switch in.Kind {
		case "allocate":
			v, e := h.Spaces.Allocate(r.Context(), actor(r), r.PathValue("id"), r.PathValue("userId"), in.AllocationInput)
			spaceResult(w, 200, v, e)
		case "reclaim":
			v, e := h.Spaces.Reclaim(r.Context(), actor(r), r.PathValue("id"), r.PathValue("userId"), in.AllocationInput)
			spaceResult(w, 200, v, e)
		default:
			fail(w, fault.ErrInvalid)
		}
	})
	handle("POST /api/spaces/{id}/credit-requests", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Needed int64 `json:"needed"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		e := h.Spaces.RequestCredits(r.Context(), actor(r), r.PathValue("id"), in.Needed)
		spaceResult(w, 200, map[string]bool{"requested": true}, e)
	})
	handle("POST /api/spaces/{id}/leave", func(w http.ResponseWriter, r *http.Request) {
		e := h.Spaces.Leave(r.Context(), actor(r), r.PathValue("id"))
		spaceResult(w, 200, map[string]bool{"left": true}, e)
	})
	handle("POST /api/spaces/{id}/ownership-transfers", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			UserID string `json:"userId"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Spaces.BeginOwnership(r.Context(), actor(r), r.PathValue("id"), in.UserID)
		spaceResult(w, 201, v, e)
	})
	handle("POST /api/spaces/{id}/ownership-transfers/{transferId}/accept", func(w http.ResponseWriter, r *http.Request) {
		e := h.Spaces.AcceptOwnership(r.Context(), actor(r), r.PathValue("id"), r.PathValue("transferId"))
		spaceResult(w, 200, map[string]bool{"accepted": true}, e)
	})
	handle("POST /api/spaces/{id}/invite-links", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.CreateInviteLink(r.Context(), actor(r), r.PathValue("id"))
		spaceResult(w, 201, v, e)
	})
	handle("DELETE /api/spaces/{id}/invite-links", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			InviteID string `json:"inviteId"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		e := h.Spaces.RevokeInviteLink(r.Context(), actor(r), r.PathValue("id"), in.InviteID)
		spaceResult(w, 200, map[string]bool{"revoked": true}, e)
	})
	handle("GET /api/spaces/{id}/invites", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.Invites(r.Context(), actor(r), r.PathValue("id"))
		spaceResult(w, 200, v, e)
	})
	handle("POST /api/spaces/{id}/invites", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Spaces.InviteEmail(r.Context(), actor(r), r.PathValue("id"), in.Email)
		spaceResult(w, 201, v, e)
	})
	handle("GET /api/invites/pending", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.PendingInvites(r.Context(), actor(r))
		spaceResult(w, 200, v, e)
	})
	handle("POST /api/invites/{inviteId}/accept", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.AcceptInvite(r.Context(), actor(r), r.PathValue("inviteId"))
		spaceResult(w, 200, v, e)
	})
	handle("POST /api/invites/{inviteId}/decline", func(w http.ResponseWriter, r *http.Request) {
		e := h.Spaces.DeclineInvite(r.Context(), actor(r), r.PathValue("inviteId"))
		spaceResult(w, 200, map[string]bool{"declined": true}, e)
	})
	handle("GET /api/invite-links/{token}", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.PreviewLink(r.Context(), actor(r), r.PathValue("token"))
		spaceResult(w, 200, v, e)
	})
	handle("POST /api/invite-links/{token}/accept", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.AcceptLink(r.Context(), actor(r), r.PathValue("token"))
		spaceResult(w, 200, v, e)
	})
	handle("GET /api/teams/lookup", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.Lookup(r.Context(), actor(r), r.URL.Query().Get("id"))
		spaceResult(w, 200, map[string]string{"name": v}, e)
	})
	handle("POST /api/spaces/{id}/join-requests", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Message string `json:"message"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Spaces.RequestJoin(r.Context(), actor(r), r.PathValue("id"), in.Message)
		spaceResult(w, 201, v, e)
	})
	handle("GET /api/spaces/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		// ServeMux GET patterns also match HEAD; never open a stream for HEAD.
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		id := r.PathValue("id")
		updates, closeWatch, e := h.Spaces.WatchTeam(r.Context(), actor(r), id)
		if e != nil {
			fail(w, e)
			return
		}
		defer closeWatch()
		h.serveInvalidationEvents(w, r, updates, "team", func() error {
			return h.Spaces.TeamMember(r.Context(), actor(r), id)
		})
	})
	handle("GET /api/spaces/{id}/join-requests", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Spaces.JoinRequests(r.Context(), actor(r), r.PathValue("id"))
		spaceResult(w, 200, v, e)
	})
	handle("POST /api/spaces/{id}/join-requests/{requestId}/approve", func(w http.ResponseWriter, r *http.Request) {
		e := h.Spaces.DecideJoin(r.Context(), actor(r), r.PathValue("id"), r.PathValue("requestId"), true)
		spaceResult(w, 200, map[string]bool{"approved": true}, e)
	})
	handle("POST /api/spaces/{id}/join-requests/{requestId}/reject", func(w http.ResponseWriter, r *http.Request) {
		e := h.Spaces.DecideJoin(r.Context(), actor(r), r.PathValue("id"), r.PathValue("requestId"), false)
		spaceResult(w, 200, map[string]bool{"rejected": true}, e)
	})
	handle("POST /api/spaces/{id}/transfers", func(w http.ResponseWriter, r *http.Request) {
		var in spaceapp.TransferInput
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Spaces.TransferContent(r.Context(), actor(r), r.PathValue("id"), in)
		spaceResult(w, 200, v, e)
	})
	handle("GET /api/spaces/{id}/overview", func(w http.ResponseWriter, r *http.Request) {
		span := r.URL.Query().Get("range")
		if span == "" {
			span = "30d"
		}
		v, e := h.Spaces.Overview(r.Context(), actor(r), r.PathValue("id"), span)
		spaceResult(w, 200, v, e)
	})
	handle("GET /api/spaces/{id}/audit", func(w http.ResponseWriter, r *http.Request) {
		var cursor int64
		limit := 30
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			v, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || v < 0 {
				fail(w, fault.ErrInvalid)
				return
			}
			cursor = v
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			v, e := strconv.Atoi(raw)
			if e != nil {
				fail(w, fault.ErrInvalid)
				return
			}
			limit = v
		}
		v, e := h.Spaces.Audit(r.Context(), actor(r), r.PathValue("id"), cursor, limit)
		spaceResult(w, 200, v, e)
	})
}
