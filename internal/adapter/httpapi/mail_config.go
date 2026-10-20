package httpapi

import (
	app "drama-agent/internal/application/mailconfig"
	"drama-agent/internal/domain/fault"
	"net/http"
)

func (h *Handler) registerMailConfig(mux *routes) {
	ready := func(w http.ResponseWriter) bool {
		if h.MailConfig == nil {
			fail(w, fault.New("MAIL_CONFIGURATION_UNAVAILABLE", "邮件配置服务不可用"))
			return false
		}
		return true
	}
	ops := mux.operations()
	ops.HandleFunc("GET /api/operations/smtp", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		value, err := h.MailConfig.Get(r.Context(), actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, value)
	})
	ops.HandleFunc("POST /api/operations/smtp", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		var input struct {
			Revision      *uint64 `json:"revision"`
			Enabled       *bool   `json:"enabled"`
			Address       *string `json:"address"`
			Username      *string `json:"username"`
			From          *string `json:"from"`
			TLS           *string `json:"tls"`
			Password      string  `json:"password,omitempty"`
			ClearPassword bool    `json:"clearPassword,omitempty"`
		}
		if err := readPayload(w, r, &input); err != nil {
			fail(w, err)
			return
		}
		if input.Revision == nil || input.Enabled == nil || input.Address == nil || input.Username == nil || input.From == nil || input.TLS == nil {
			fail(w, fault.New("MAIL_CONFIGURATION_INVALID", "请提交完整邮件配置及版本"))
			return
		}
		value, err := h.MailConfig.Save(r.Context(), actor(r), app.Input{Revision: *input.Revision, Settings: app.Settings{Enabled: *input.Enabled, Address: *input.Address, Username: *input.Username, From: *input.From, TLS: *input.TLS}, Password: input.Password, ClearPassword: input.ClearPassword})
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, value)
	})
}
