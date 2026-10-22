package httpapi

import (
	u "drama-agent/internal/application/modelusage"
	"drama-agent/internal/domain/fault"
	"net/http"
	"strings"
	"time"
)

func (h *Handler) registerModelUsage(mux *routes) {
	mux.operations().HandleFunc("GET /api/operations/model-usage", func(w http.ResponseWriter, r *http.Request) {
		if h.Usage == nil {
			fail(w, fault.New("AUTH_UNAVAILABLE", "使用记录查询尚未配置"))
			return
		}
		p, n, e := page(r)
		if e != nil {
			fail(w, e)
			return
		}
		q := r.URL.Query()
		filter := u.Filter{View: q.Get("view"), Search: strings.TrimSpace(q.Get("q")), UserID: q.Get("userId"), SpaceID: q.Get("spaceId"), TaskID: q.Get("taskId"), RequestID: q.Get("requestId"), ModelID: q.Get("modelId"), ConnectionID: q.Get("connectionId"), Operation: q.Get("operation"), Status: q.Get("status"), Sort: q.Get("sort"), Page: p, PageSize: n}
		if filter.View == "" {
			filter.View = "tasks"
		}
		if filter.Sort == "" {
			filter.Sort = "desc"
		}
		for key, target := range map[string]**time.Time{"from": &filter.From, "until": &filter.Until} {
			if v := q.Get(key); v != "" {
				t, e := time.Parse(time.RFC3339Nano, v)
				if e != nil {
					fail(w, fault.ErrInvalid)
					return
				}
				*target = &t
			}
		}
		v, e := h.Usage.Query(r.Context(), actor(r), filter)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
}
