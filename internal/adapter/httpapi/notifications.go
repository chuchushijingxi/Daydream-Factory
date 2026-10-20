package httpapi

import (
	"bytes"
	app "drama-agent/internal/application/notifications"
	"drama-agent/internal/domain/fault"
	"io"
	"net/http"
	"strconv"
	"time"
)

func notificationQuery(r *http.Request) (app.Query, error) {
	q := app.Query{Page: 1, PageSize: 20, Status: "all"}
	var e error
	if v := r.URL.Query().Get("page"); v != "" {
		q.Page, e = strconv.Atoi(v)
		if e != nil {
			return q, fault.ErrInvalid
		}
	}
	if v := r.URL.Query().Get("pageSize"); v != "" {
		q.PageSize, e = strconv.Atoi(v)
		if e != nil {
			return q, fault.ErrInvalid
		}
	}
	if v := r.URL.Query().Get("status"); v != "" {
		q.Status = v
	}
	return q, nil
}
func (h *Handler) registerNotifications(mux *routes) {
	ready := func(w http.ResponseWriter) bool {
		w.Header().Set("Cache-Control", "private, no-store")
		if h.Notifications == nil {
			fail(w, fault.New("NOTIFICATION_UNAVAILABLE", "站内信服务不可用"))
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/notifications/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !ready(w) {
			return
		}
		updates, closeWatch, e := h.Notifications.Watch(r.Context(), actor(r))
		if e != nil {
			fail(w, e)
			return
		}
		defer closeWatch()
		h.serveInvalidationEvents(w, r, updates, "notifications")
	})
	mux.HandleFunc("GET /api/notifications/{id}/images/{imageId}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Notifications.Image(r.Context(), actor(r), r.PathValue("id"), r.PathValue("imageId"))
		if e != nil {
			fail(w, e)
			return
		}
		w.Header().Set("Content-Type", v.ContentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "notification-image", time.Time{}, bytes.NewReader(v.Data))
	})
	mux.HandleFunc("GET /api/notifications", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		q, e := notificationQuery(r)
		if e != nil {
			fail(w, e)
			return
		}
		v, e := h.Notifications.List(r.Context(), actor(r), q)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("GET /api/notifications/unread", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Notifications.Unread(r.Context(), actor(r))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]int64{"unread": v})
	})
	mux.HandleFunc("GET /api/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Notifications.Detail(r.Context(), actor(r), r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("POST /api/notifications/{id}/read", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Notifications.Read(r.Context(), actor(r), r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("POST /api/notifications/read-all", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Notifications.ReadAll(r.Context(), actor(r))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops := mux.operations()
	ops.HandleFunc("GET /api/operations/notifications/{id}/images/{imageId}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Notifications.OperatorImage(r.Context(), actor(r), r.PathValue("id"), r.PathValue("imageId"))
		if e != nil {
			fail(w, e)
			return
		}
		w.Header().Set("Content-Type", v.ContentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "notification-image", time.Time{}, bytes.NewReader(v.Data))
	})
	ops.HandleFunc("POST /api/operations/notifications/{id}/images", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, app.MaxImageBytes)
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			fail(w, fault.New("NOTIFICATION_IMAGE_INVALID", "图片必须在5MiB以内"))
			return
		}
		v, e := h.Notifications.UploadImage(r.Context(), actor(r), r.PathValue("id"), raw)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 201, v)
	})
	ops.HandleFunc("POST /api/operations/notifications/{id}/withdraw", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Revision uint64 `json:"revision"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Notifications.Withdraw(r.Context(), actor(r), r.PathValue("id"), in.Revision)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("GET /api/operations/notification-commands/{commandId}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Notifications.Command(r.Context(), actor(r), r.PathValue("commandId"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("GET /api/operations/notifications", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		q, e := notificationQuery(r)
		if e != nil {
			fail(w, e)
			return
		}
		v, e := h.Notifications.OperatorList(r.Context(), actor(r), q)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("GET /api/operations/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Notifications.OperatorDetail(r.Context(), actor(r), r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("POST /api/operations/notifications", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in app.Input
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Notifications.Create(r.Context(), actor(r), in)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 201, v)
	})
	ops.HandleFunc("POST /api/operations/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in app.Input
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Notifications.Update(r.Context(), actor(r), r.PathValue("id"), in)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("POST /api/operations/notifications/{id}/publish", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Revision uint64 `json:"revision"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Notifications.Publish(r.Context(), actor(r), r.PathValue("id"), in.Revision)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
}
