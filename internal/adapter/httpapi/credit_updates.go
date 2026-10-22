package httpapi

import (
	"net/http"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

func (h *Handler) creditEvents(w http.ResponseWriter, r *http.Request) {
	// ServeMux GET patterns also match HEAD; never open a stream for HEAD.
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.Credits == nil {
		fail(w, fault.New("CREDIT_UPDATES_UNAVAILABLE", "积分实时同步暂不可用"))
		return
	}
	var updates <-chan struct{}
	var closeWatch func()
	var err error
	if usesSpaceCredits(r) {
		if h.Spaces == nil {
			fail(w, fault.New("SPACE_UNAVAILABLE", "空间积分服务尚未配置"))
			return
		}
		updates, closeWatch, err = h.Spaces.WatchCredits(r.Context(), actor(r))
	} else {
		updates, closeWatch, err = h.Credits.Watch(r.Context(), actor(r), r.PathValue("userId"))
	}
	if err != nil {
		fail(w, err)
		return
	}
	defer closeWatch()
	if usesSpaceCredits(r) {
		h.serveInvalidationEvents(w, r, updates, "credits", func() error {
			// 长连接每轮使用新解析范围，移出或停用后不继续发送团队事件。
			ctx := ports.WithRequestedSpace(r.Context(), ports.RequestedSpace(r.Context()))
			_, err := h.Spaces.CreditScope(ctx, actor(r))
			return err
		})
		return
	}
	h.serveInvalidationEvents(w, r, updates, "credits")
}
