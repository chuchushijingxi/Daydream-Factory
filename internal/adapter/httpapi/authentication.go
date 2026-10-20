package httpapi

import (
	"context"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"net/http"
	"net/url"
	"time"
)

const sessionCookie = "drama_workspace_session"
const operationsSessionCookie = "drama_operations_session"

func cookieForAudience(audience string) string {
	if audience == accounts.Operations {
		return operationsSessionCookie
	}
	return sessionCookie
}

// 平台选择器只选定一份凭证，绝不授予权限。
func requestAudience(r *http.Request) (string, bool, error) {
	headers, headerPresent := r.Header[http.CanonicalHeaderKey("X-Drama-Audience")]
	values, queryErr := url.ParseQuery(r.URL.RawQuery)
	query, queryPresent := values["audience"]
	invalid := fault.New("INVALID_SESSION_AUDIENCE", "登录平台选择无效")
	if queryErr != nil {
		return "", true, invalid
	}
	if headerPresent && (len(headers) != 1 || !accounts.ValidAudience(headers[0])) {
		return "", true, invalid
	}
	if queryPresent && (len(query) != 1 || !accounts.ValidAudience(query[0])) {
		return "", true, invalid
	}
	if headerPresent && queryPresent && headers[0] != query[0] {
		return "", true, invalid
	}
	if headerPresent {
		return headers[0], true, nil
	}
	if queryPresent {
		return query[0], true, nil
	}
	return accounts.Workspace, false, nil
}

type actorKey struct{}
type tokenKey struct{}

func sessionToken(r *http.Request) string { v, _ := r.Context().Value(tokenKey{}).(string); return v }

func actor(r *http.Request) ports.Actor {
	a, _ := r.Context().Value(actorKey{}).(ports.Actor)
	return a
}

type Route struct {
	Pattern string
	Access  string
}

// routes is the only registration capability given to modules. The default is protected.
type routes struct {
	mux      *http.ServeMux
	policies map[string]string
	access   string
}

func (r *routes) HandleFunc(pattern string, f func(http.ResponseWriter, *http.Request)) {
	r.Handle(pattern, http.HandlerFunc(f))
}
func (r *routes) Handle(pattern string, h http.Handler) {
	access := r.access
	if access == "" {
		access = "user"
	}
	r.policies[pattern] = access
	r.mux.Handle(pattern, h)
}
func (r *routes) operations() *routes {
	return &routes{mux: r.mux, policies: r.policies, access: "operator"}
}
func (r *routes) public() *routes { return &routes{mux: r.mux, policies: r.policies, access: "public"} }
func (h *Handler) Routes() []Route {
	out := []Route{}
	for p, a := range h.routePolicies {
		out = append(out, Route{p, a})
	}
	return out
}
func (h *Handler) authenticated(next http.Handler, mode string, w http.ResponseWriter, r *http.Request) {
	if mode == "public" {
		if h.rejectReadOnly(w, r) {
			return
		}
		next.ServeHTTP(w, r)
		return
	}
	if h.Authenticator == nil {
		fail(w, fault.New("AUTH_UNAVAILABLE", "账号服务尚未配置"))
		return
	}
	audience, _, e := requestAudience(r)
	if e != nil {
		fail(w, e)
		return
	}
	c, e := r.Cookie(cookieForAudience(audience))
	if e != nil {
		fail(w, fault.New("AUTH_REQUIRED", "请先登录"))
		return
	}
	_, tokenAudience, _, e := accounts.SessionParts(c.Value)
	if e != nil {
		fail(w, e)
		return
	}
	if tokenAudience != audience {
		fail(w, fault.New("SESSION_AUDIENCE_MISMATCH", "会话不属于当前平台"))
		return
	}
	a, e := h.Authenticator.Authenticate(r.Context(), c.Value)
	if e != nil {
		fail(w, e)
		return
	}
	if e = a.Require(); e != nil {
		fail(w, e)
		return
	}
	if h.Runtime != nil {
		allowed, err := h.Runtime.Allow(r.Context(), "http-account:"+a.UserID, h.RequestsPerMinute, time.Minute)
		if err != nil {
			fail(w, err)
			return
		}
		if !allowed {
			w.Header().Set("Retry-After", "60")
			fail(w, fault.New("AUTH_RATE_LIMITED", "请求过于频繁"))
			return
		}
	}
	if mode == "operator" {
		if audience != accounts.Operations {
			fail(w, fault.New("SESSION_AUDIENCE_MISMATCH", "运营接口需要运营平台会话"))
			return
		}
		if e = a.RequireOperator(); e != nil {
			fail(w, e)
			return
		}
	}
	if h.rejectReadOnly(w, r) {
		return
	}
	requestCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	r = r.WithContext(context.WithValue(context.WithValue(requestCtx, actorKey{}, a), tokenKey{}, c.Value))
	// 短请求只认证一次；长写入在后续每个分块写出前重查会话撤销。
	next.ServeHTTP(&sessionWriter{ResponseWriter: w, auth: h.Authenticator, token: c.Value, ctx: r.Context(), cancel: cancel, checked: time.Now(), actor: a, operator: mode == "operator"}, r)
}

type sessionWriter struct {
	cancel context.CancelFunc
	http.ResponseWriter
	auth     ports.SessionAuthenticator
	token    string
	ctx      context.Context
	checked  time.Time
	revoked  error
	actor    ports.Actor
	operator bool
}

func (w *sessionWriter) check() error {
	if w.revoked != nil {
		return w.revoked
	}
	if time.Since(w.checked) >= 2*time.Second {
		current, err := w.auth.Authenticate(w.ctx, w.token)
		w.revoked = err
		if w.revoked == nil {
			w.revoked = current.Require()
		}
		if w.revoked == nil && w.actor.UserID != "" && current.UserID != w.actor.UserID {
			w.revoked = fault.New("SESSION_REPLACED", "会话已变化")
		}
		if w.revoked == nil && w.operator {
			w.revoked = current.RequireOperator()
		}
		w.checked = time.Now()
		if w.revoked != nil && w.cancel != nil {
			w.cancel()
		}
	}
	return w.revoked
}
func (w *sessionWriter) Write(b []byte) (int, error) {
	n := 0
	for len(b) > 0 {
		if e := w.check(); e != nil {
			return n, e
		}
		size := len(b)
		if size > 64<<10 {
			size = 64 << 10
		}
		k, e := w.ResponseWriter.Write(b[:size])
		n += k
		b = b[k:]
		if e != nil {
			return n, e
		}
		if k == 0 {
			return n, http.ErrAbortHandler
		}
	}
	return n, nil
}
func (w *sessionWriter) Flush() {
	if w.check() == nil {
		if f, ok := w.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
	}
}
func (w *sessionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (h *Handler) rejectReadOnly(w http.ResponseWriter, r *http.Request) bool {
	if h.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
		write(w, http.StatusForbidden, map[string]string{"code": "SERVICE_READ_ONLY", "message": "当前服务只读，不能上传、保存、启动或取消任务。"})
		return true
	}
	return false
}
