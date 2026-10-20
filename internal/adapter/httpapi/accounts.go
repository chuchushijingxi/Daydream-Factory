package httpapi

import (
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"net/http"
	"strconv"
	"time"
)

func page(r *http.Request) (int, int, error) {
	p, n := 1, 20
	var e error
	if v := r.URL.Query().Get("page"); v != "" {
		p, e = strconv.Atoi(v)
		if e != nil {
			return 0, 0, fault.ErrInvalid
		}
	}
	if v := r.URL.Query().Get("pageSize"); v != "" {
		n, e = strconv.Atoi(v)
		if e != nil {
			return 0, 0, fault.ErrInvalid
		}
	}
	return p, n, ports.CheckPage(p, n)
}
func (h *Handler) registerAccounts(mux *routes) {
	public := mux.public()
	ops := mux.operations()
	ready := func(w http.ResponseWriter) bool {
		if h.Accounts == nil {
			fail(w, fault.New("AUTH_UNAVAILABLE", "账号服务尚未配置"))
			return false
		}
		return true
	}
	public.HandleFunc("GET /api/auth/options", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Accounts.Store.RegistrationRequired(r.Context())
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"registrationRequiresCode": v, "passwordLogin": true, "codeLogin": true})
	})
	public.HandleFunc("POST /api/auth/email-code", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Email   string `json:"email"`
			Purpose string `json:"purpose"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		if e := h.Accounts.SendCode(r.Context(), in.Email, in.Purpose); e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"submitted": true})
	})
	public.HandleFunc("POST /api/auth/register", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Email             string `json:"email"`
			Password          string `json:"password"`
			Code              string `json:"code"`
			AuthorizationCode string `json:"authorizationCode,omitempty"`
			Username          string `json:"username"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Accounts.Register(r.Context(), in.Email, in.Username, in.Password, in.Code, in.AuthorizationCode)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 201, v)
	})
	public.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password,omitempty"`
			Code     string `json:"code,omitempty"`
			Audience string `json:"audience"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		selected, explicit, e := requestAudience(r)
		if e != nil {
			fail(w, e)
			return
		}
		if explicit && selected != in.Audience {
			fail(w, fault.New("INVALID_SESSION_AUDIENCE", "登录平台选择不一致"))
			return
		}
		v, e := h.Accounts.Login(r.Context(), in.Email, in.Password, in.Code, in.Audience)
		if e != nil {
			fail(w, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieForAudience(in.Audience), Value: v.Token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, Expires: v.ExpiresAt, MaxAge: int(time.Until(v.ExpiresAt).Seconds())})
		write(w, 200, map[string]any{"user": v.User})
	})
	public.HandleFunc("POST /api/auth/reset-password", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Code     string `json:"code"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		if e := h.Accounts.ResetPassword(r.Context(), in.Email, in.Code, in.Password); e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"updated": true})
	})
	mux.HandleFunc("GET /api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Accounts.Profile(r.Context(), actor(r))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		e := h.Accounts.Logout(r.Context(), actor(r), sessionToken(r))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"loggedOut": true})
	})
	ops.HandleFunc("GET /api/operations/users", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		p, n, e := page(r)
		if e != nil {
			fail(w, e)
			return
		}
		v, e := h.Accounts.Users(r.Context(), actor(r), r.URL.Query().Get("q"), p, n)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("POST /api/operations/users/{userId}/status", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Enabled *bool `json:"enabled"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		if in.Enabled == nil {
			fail(w, fault.ErrInvalid)
			return
		}
		v, e := h.Accounts.SetEnabled(r.Context(), actor(r), r.PathValue("userId"), *in.Enabled)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("GET /api/operations/registration", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Accounts.Store.RegistrationRequired(r.Context())
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"registrationRequiresCode": v})
	})
	ops.HandleFunc("POST /api/operations/registration", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Required *bool `json:"registrationRequiresCode"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		if in.Required == nil {
			fail(w, fault.ErrInvalid)
			return
		}
		if e := h.Accounts.SetRegistration(r.Context(), actor(r), *in.Required); e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"registrationRequiresCode": *in.Required})
	})
	ops.HandleFunc("GET /api/operations/authorization-codes", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		p, n, e := page(r)
		if e != nil {
			fail(w, e)
			return
		}
		v, e := h.Accounts.Codes(r.Context(), actor(r), p, n)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("POST /api/operations/authorization-codes", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			ExpiresAt time.Time `json:"expiresAt"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, raw, e := h.Accounts.CreateCode(r.Context(), actor(r), in.ExpiresAt)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 201, map[string]any{"authorizationCode": v, "code": raw})
	})
	ops.HandleFunc("POST /api/operations/authorization-codes/{codeId}/disable", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct{}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		if e := h.Accounts.DisableCode(r.Context(), actor(r), r.PathValue("codeId")); e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"disabled": true})
	})
}
