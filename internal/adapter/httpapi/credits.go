package httpapi

import (
	creditsapp "drama-agent/internal/application/credits"
	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"net/http"
	"time"
)

func (h *Handler) registerCredits(mux *routes) {
	ready := func(w http.ResponseWriter) bool {
		if h.Credits == nil {
			fail(w, fault.New("AUTH_UNAVAILABLE", "积分服务尚未配置"))
			return false
		}
		return true
	}
	balance := func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		if usesSpaceCredits(r) {
			if h.Spaces == nil {
				fail(w, fault.New("SPACE_UNAVAILABLE", "空间积分服务尚未配置"))
				return
			}
			v, e := h.Spaces.CreditWallet(r.Context(), actor(r))
			spaceResult(w, 200, v, e)
			return
		}
		v, e := h.Credits.Wallet(r.Context(), actor(r), r.PathValue("userId"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	}
	ledger := func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		p, n, e := page(r)
		if r.URL.Query().Get("pageSize") == "" {
			n = 10
		}
		if n > 50 {
			e = fault.ErrInvalid
		}
		if e != nil {
			fail(w, e)
			return
		}
		filter := d.HistoryFilter{Page: p, PageSize: n, Direction: r.URL.Query().Get("direction"), Category: r.URL.Query().Get("category")}
		if filter.Direction == "" {
			filter.Direction = "all"
		}
		if filter.Category == "" {
			filter.Category = "all"
		}
		for name, target := range map[string]**time.Time{"start": &filter.Start, "end": &filter.End} {
			if raw := r.URL.Query().Get(name); raw != "" {
				parsed, err := time.Parse(time.RFC3339Nano, raw)
				if err != nil {
					fail(w, fault.ErrInvalid)
					return
				}
				*target = &parsed
			}
		}
		if usesSpaceCredits(r) {
			if h.Spaces == nil {
				fail(w, fault.New("SPACE_UNAVAILABLE", "空间积分服务尚未配置"))
				return
			}
			// scope=holder 读本人名下流水（管理员的本人积分明细）；缺省为团队页使用的空间流水。
			scope := r.URL.Query().Get("scope")
			if scope != "" && scope != "holder" {
				fail(w, fault.ErrInvalid)
				return
			}
			v, e := h.Spaces.CreditHistory(r.Context(), actor(r), filter, scope == "holder")
			spaceResult(w, 200, v, e)
			return
		}
		v, e := h.Credits.History(r.Context(), actor(r), r.PathValue("userId"), filter)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	}
	mux.HandleFunc("GET /api/credits/balance", balance)
	mux.HandleFunc("GET /api/credits/ledger", ledger)
	mux.HandleFunc("GET /api/credits/ledger/recent", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, fault.ErrInvalid)
			return
		}
		if usesSpaceCredits(r) {
			if h.Spaces == nil {
				fail(w, fault.New("SPACE_UNAVAILABLE", "空间积分服务尚未配置"))
				return
			}
			v, err := h.Spaces.RecentCreditEntries(r.Context(), actor(r))
			spaceResult(w, 200, v, err)
			return
		}
		items, err := h.Credits.RecentEntries(r.Context(), actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items})
	})
	mux.HandleFunc("GET /api/credits/events", h.creditEvents)
	ops := mux.operations()
	ops.HandleFunc("GET /api/operations/users/{userId}/credits/balance", balance)
	ops.HandleFunc("GET /api/operations/users/{userId}/credits/ledger", ledger)
	ops.HandleFunc("GET /api/operations/users/{userId}/credits/events", h.creditEvents)
	ops.HandleFunc("POST /api/operations/users/{userId}/credits", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Kind        string `json:"kind"`
			Amount      int64  `json:"amount"`
			OperationID string `json:"operationId"`
			Reason      string `json:"reason"`
			Category    string `json:"category,omitempty"`
		}
		if e := readPayload(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := h.Credits.Adjust(r.Context(), actor(r), r.PathValue("userId"), in.Kind, in.OperationID, in.Reason, in.Amount, in.Category)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("GET /api/operations/credit-operations/{operationId}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, e := h.Credits.Operation(r.Context(), actor(r), r.PathValue("operationId"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, v)
	})
	h.registerCreditGifts(mux)
}

func (h *Handler) registerCreditGifts(mux *routes) {
	ready := func(w http.ResponseWriter) bool {
		if h.Gifts == nil {
			fail(w, fault.New("CREDIT_POLICY_UNAVAILABLE", "积分赠送尚未配置"))
			return false
		}
		return true
	}
	mux.HandleFunc("POST /api/credits/daily-gift", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct{}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		if usesSpaceCredits(r) {
			if h.Spaces == nil {
				fail(w, fault.New("SPACE_UNAVAILABLE", "空间积分服务尚未配置"))
				return
			}
			if err := h.Spaces.RequirePersonalCredits(r.Context(), actor(r)); err != nil {
				fail(w, err)
				return
			}
		}
		v, err := h.Gifts.Daily(r.Context(), actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	ops := mux.operations()
	ops.HandleFunc("GET /api/operations/credit-policy", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		v, err := h.Gifts.Policy(r.Context(), actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	ops.HandleFunc("POST /api/operations/credit-policy", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var in struct {
			Revision         *uint64 `json:"revision"`
			RegistrationGift *int64  `json:"registrationGift"`
			DailyGift        *int64  `json:"dailyGift"`
		}
		if err := readPayload(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		if in.Revision == nil || in.RegistrationGift == nil || in.DailyGift == nil {
			fail(w, fault.ErrInvalid)
			return
		}
		v, err := h.Gifts.Save(r.Context(), actor(r), creditsapp.GiftPolicyInput{Revision: *in.Revision, RegistrationGift: *in.RegistrationGift, DailyGift: *in.DailyGift})
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
}

// 这里只选择应用入口；团队成员身份及金额可见范围由空间用例核验。
// 运营按用户查询始终使用明确的路径目标，不能被工作台空间选择改变。
func usesSpaceCredits(r *http.Request) bool {
	selected := ports.RequestedSpace(r.Context())
	return r.PathValue("userId") == "" && selected != "" && selected != actor(r).UserID
}
