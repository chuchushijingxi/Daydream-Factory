package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"drama-agent/internal/application/accounts"
	"drama-agent/internal/domain/fault"
)

func (h *Handler) registerProfile(mux *routes) {
	mux.HandleFunc("POST /api/auth/profile", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Username string `json:"username"`
		}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		v, err := h.Accounts.UpdateProfile(r.Context(), actor(r), in.Username)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("POST /api/auth/password", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			CurrentPassword string `json:"currentPassword"`
			Password        string `json:"password"`
		}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		if err := h.Accounts.ChangePassword(r.Context(), actor(r), sessionToken(r), in.CurrentPassword, in.Password); err != nil {
			fail(w, err)
			return
		}
		// Do not clear a Cookie here: a newer login response may already own it.
		write(w, 200, map[string]bool{"updated": true})
	})
	mux.HandleFunc("POST /api/auth/avatar", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, accounts.MaxAvatarBytes+(64<<10))
		if err := r.ParseMultipartForm(accounts.MaxAvatarBytes); err != nil {
			fail(w, fault.New("INVALID_AVATAR", "请选择5MB以内的图片"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		if len(r.MultipartForm.Value) != 0 || len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
			fail(w, fault.ErrInvalid)
			return
		}
		file, err := r.MultipartForm.File["file"][0].Open()
		if err != nil {
			fail(w, err)
			return
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, accounts.MaxAvatarBytes+1))
		if err != nil {
			fail(w, err)
			return
		}
		v, err := h.Accounts.SetAvatar(r.Context(), actor(r), raw)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("DELETE /api/auth/avatar", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.Accounts.RemoveAvatar(r.Context(), actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("GET /api/auth/avatar", func(w http.ResponseWriter, r *http.Request) {
		data, err := h.Accounts.Avatar(r.Context(), actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		http.ServeContent(w, r, "avatar.webp", time.Time{}, bytes.NewReader(data))
	})
}
