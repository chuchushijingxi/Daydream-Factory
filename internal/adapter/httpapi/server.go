package httpapi

import (
	"net"
	"net/http"
	"strings"
	"time"

	"drama-agent/internal/application/accounts"
	creditsapp "drama-agent/internal/application/credits"
	"drama-agent/internal/application/mailconfig"
	"drama-agent/internal/application/medialibrary"
	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/modelusage"
	"drama-agent/internal/application/notifications"
	paymentapp "drama-agent/internal/application/payment"
	"drama-agent/internal/application/platform"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	spaceapp "drama-agent/internal/application/spaces"
	uploadapp "drama-agent/internal/application/upload"
	d "drama-agent/internal/domain/project"
)

// Handler 只承担 HTTP 转换；任务生命周期由应用层 Workers 管理。
type Handler struct {
	Spaces                *spaceapp.Service
	Payments              *paymentapp.Service
	paymentNotifyMaxBytes int64
	MediaLibrary          *medialibrary.Service
	Runtime               ports.RuntimeState
	RequestsPerMinute     int
	IPRequestsPerMinute   int
	TrustedProxies        []*net.IPNet
	StreamHeartbeat       time.Duration
	StreamWriteTimeout    time.Duration
	registry              *routes
	Usage                 *modelusage.Service
	Authenticator         ports.SessionAuthenticator
	Accounts              *accounts.Service
	MailConfig            *mailconfig.Service
	Credits               *creditsapp.Service
	Gifts                 *creditsapp.Gifts
	Notifications         *notifications.Service
	Platform              *platform.Service
	routePolicies         map[string]string
	Service               *app.Service
	Configuration         *modelconfig.Service
	Files                 *uploadapp.Service
	LimitMicroCNY         int64
	*app.Workers
}

func New(service *app.Service, configuration *modelconfig.Service, demoDir string, limit int64) (*Handler, http.Handler) {
	h := &Handler{Service: service, Configuration: configuration, LimitMicroCNY: limit, Workers: app.NewWorkers(service)}
	mux := &routes{mux: http.NewServeMux(), policies: map[string]string{}}
	h.routePolicies = mux.policies
	h.registry = mux
	h.registerAccounts(mux)
	h.registerSpaces(mux)
	h.registerSpaceSharing(mux)
	h.registerOperationsTeams(mux)
	h.registerProfile(mux)
	h.registerCredits(mux)
	h.registerPayments(mux)
	h.registerChargeQuotes(mux)
	h.registerModelUsage(mux)
	h.registerMailConfig(mux)
	h.registerNotifications(mux)
	h.registerPlatform(mux)
	mux.public().HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]bool{"alive": true}) })
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		options, err := h.projectService(r).AvailableModels(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, map[string]any{"contract": d.CatalogExtractionContract, "model": "", "configured": !h.ReadOnly && len(options.Analysis) > 0, "readOnly": h.ReadOnly, "capabilities": map[string]bool{"storyboard": true, "assetEditing": true, "assetImages": service.Files != nil && h.Files != nil, "characterLooks": true, "projectDeletion": true, "projectStyle": len(service.Styles) > 0, "projectOutline": true, "projectSettings": true, "visualPrompts": true, "singleVisualPrompts": true, "modelConfiguration": configuration != nil && actor(r).Role == ports.RoleOperator, "projectMedia": service.MediaTasks != nil}})
	})
	h.registerModelConfig(mux)
	h.registerUploads(mux)
	h.registerAssetImages(mux)
	h.registerProjectMedia(mux)
	h.registerMediaLibrary(mux)
	h.registerProjectPermissions(mux)
	h.registerProjectFiles(mux)
	h.registerProjectCommands(mux)
	h.registerStoryboard(mux)
	mux.HandleFunc("GET /api/projects", h.list)
	mux.HandleFunc("POST /api/projects", h.create)
	mux.HandleFunc("GET /api/projects/{id}", h.project)
	mux.HandleFunc("GET /api/projects/{id}/events", h.projectEvents)
	mux.HandleFunc("POST /api/projects/{id}/delete", h.deleteProject)
	mux.HandleFunc("POST /api/projects/{id}/source", h.upload)
	mux.HandleFunc("GET /api/projects/{id}/source", h.readSource)
	mux.HandleFunc("GET /api/projects/{id}/source/file", h.originalScript)
	mux.HandleFunc("POST /api/projects/{id}/runs", h.start)
	mux.HandleFunc("POST /api/projects/{id}/cancel", h.cancel)
	mux.public().Handle("GET /demo/", staticShell(demoDir))
	mux.public().HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/demo/", http.StatusSeeOther) })
	return h, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostname, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			hostname = r.Host
		}
		ip := net.ParseIP(hostname)
		if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
			write(w, 403, map[string]string{"code": "INVALID_HOST"})
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", cacheControl(r.Method, r.URL.Path))
		// 同源变更保护本机配置的供应商凭证。
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+r.Host && r.Header.Get("Origin") != "https://"+r.Host {
			write(w, 403, map[string]string{"code": "CROSS_ORIGIN"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			write(w, 403, map[string]string{"code": "CROSS_ORIGIN"})
			return
		}
		_, pattern := mux.mux.Handler(r)
		// 未注册路由保持ServeMux的404/405，不把不存在的模拟入口变成鉴权入口。
		if pattern == "" {
			mux.mux.ServeHTTP(w, r)
			return
		}
		if h.Runtime != nil && len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			allowed, err := h.Runtime.Allow(r.Context(), "http:"+h.clientIP(r), h.IPRequestsPerMinute, time.Minute)
			if err != nil {
				fail(w, err)
				return
			}
			if !allowed {
				w.Header().Set("Retry-After", "60")
				write(w, 429, map[string]string{"code": "AUTH_RATE_LIMITED", "error": "请求过于频繁"})
				return
			}
		}
		h.authenticated(mux.mux, mux.policies[pattern], w, r)
	})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		FileID        string             `json:"fileId"`
		Title         string             `json:"title"`
		Settings      *d.ProjectSettings `json:"settings,omitempty"`
		StyleID       string             `json:"styleId,omitempty"`
		PromptVersion string             `json:"promptVersion,omitempty"`
		BillingBasis  string             `json:"billingBasis,omitempty"`
	}
	if _, err := readJSONBody(w, r, &input); err != nil {
		fail(w, err)
		return
	}
	choices := app.CreationChoices{Settings: input.Settings, StyleID: input.StyleID, PromptVersion: input.PromptVersion}
	p, err := h.projectService(r).WithBillingBasis(input.BillingBasis).CreateFromFile(r.Context(), input.Title, input.FileID, h.LimitMicroCNY, choices)
	if err != nil {
		fail(w, err)
		return
	}
	if p.LatestRun() != nil && p.LatestRun().Status == d.RunRunning {
		h.Launch(p.ID)
	}
	write(w, 201, view(p))
}

// immutableAssets 给带内容哈希的构建产物用：文件名随内容变化，可以永久缓存。
const immutableAssets = "public, max-age=31536000, immutable"

// cacheControl 只放行 /demo/app/assets/ 下的哈希产物；入口 HTML 与 API 必须每次取最新。
// 字体等静态资源默认 no-store 会导致每次刷新重下 20MB，首屏先用回退字体再切换（FOUT）。
func cacheControl(method, path string) string {
	if (method == http.MethodGet || method == http.MethodHead) && strings.HasPrefix(path, "/demo/app/assets/") {
		return immutableAssets
	}
	return "no-store"
}
