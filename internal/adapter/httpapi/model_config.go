package httpapi

import (
	"io"
	"net/http"

	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/domain/fault"
)

func (h *Handler) registerModelConfig(root *routes) {
	mux := root.operations()
	unavailable := func(w http.ResponseWriter) {
		fail(w, fault.New("MODEL_CONFIG_UNAVAILABLE", "当前服务尚未接入模型配置"))
	}
	mux.HandleFunc("GET /api/model-config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if h.Configuration == nil {
			unavailable(w)
			return
		}
		view, err := h.Configuration.ForOperator(actor(r)).View(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, view)
	})
	mux.HandleFunc("POST /api/model-config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if h.Configuration == nil {
			unavailable(w)
			return
		}
		var input modelconfig.ModelConfigInput
		if err := readCommand(w, r, &input); err != nil {
			fail(w, err)
			return
		}
		view, err := h.Configuration.ForOperator(actor(r)).Update(r.Context(), input)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, view)
	})
	mux.HandleFunc("POST /api/model-config/models/discover", func(w http.ResponseWriter, r *http.Request) {
		if h.Configuration == nil {
			unavailable(w)
			return
		}
		var input modelconfig.DiscoverModelsInput
		if err := readCommand(w, r, &input); err != nil {
			fail(w, err)
			return
		}
		v, err := h.Configuration.ForOperator(actor(r)).DiscoverModels(r.Context(), input)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("POST /api/model-logos", func(w http.ResponseWriter, r *http.Request) {
		if h.Configuration == nil {
			unavailable(w)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			fail(w, fault.New("INVALID_MODEL_LOGO", "Logo图片最大2MB"))
			return
		}
		v, err := h.Configuration.ForOperator(actor(r)).UploadLogo(r.Context(), raw)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 201, v)
	})
	root.HandleFunc("GET /api/model-logos/{id}", func(w http.ResponseWriter, r *http.Request) {
		if h.Configuration == nil {
			unavailable(w)
			return
		}
		raw, err := h.Configuration.ForActor(actor(r)).ReadLogo(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(200)
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("POST /api/model-config/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		if h.Configuration == nil {
			unavailable(w)
			return
		}
		var in struct {
			Revision uint64 `json:"revision"`
		}
		if err := readCommand(w, r, &in); err != nil {
			fail(w, err)
			return
		}
		v, err := h.Configuration.ForOperator(actor(r)).Delete(r.Context(), r.PathValue("id"), in.Revision)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		write(w, 200, v)
	})

}
