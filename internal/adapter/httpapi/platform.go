package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"time"

	app "drama-agent/internal/application/platform"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/platform"
)

func (h *Handler) registerPlatform(root *routes) {
	ready := func(w http.ResponseWriter) bool {
		if h.Platform == nil {
			fail(w, fault.New("PLATFORM_SETTINGS_UNAVAILABLE", "平台设置服务不可用"))
			return false
		}
		return true
	}
	read := func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, err := h.Platform.Get(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	}
	root.public().HandleFunc("GET /api/platform-settings", read)
	ops := root.operations()
	ops.HandleFunc("GET /api/operations/platform-settings", read)
	ops.HandleFunc("POST /api/operations/platform-settings", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Revision           *uint64 `json:"revision"`
			Name               *string `json:"name"`
			SidebarDescription *string `json:"sidebarDescription"`
			LogoID             *string `json:"logoId"`
		}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		if in.Revision == nil || in.Name == nil || in.SidebarDescription == nil || in.LogoID == nil {
			fail(w, fault.New("PLATFORM_SETTINGS_INVALID", "请提交完整平台设置及版本"))
			return
		}
		v, err := h.Platform.Save(r.Context(), actor(r), app.Input{Revision: *in.Revision, Settings: d.Settings{Name: *in.Name, SidebarDescription: *in.SidebarDescription, LogoID: *in.LogoID}})
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("POST /api/operations/platform-logos", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			fail(w, fault.New("INVALID_PLATFORM_LOGO", "平台Logo最大2MiB"))
			return
		}
		v, err := h.Platform.UploadLogo(r.Context(), actor(r), raw)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 201, v)
	})
	root.public().HandleFunc("GET /api/platform-logos/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		id := r.PathValue("id")
		raw, err := h.Platform.ReadLogo(r.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", `"`+id+`"`)
		http.ServeContent(w, r, id+".png", time.Time{}, bytes.NewReader(raw))
	})
}
