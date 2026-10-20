package modelconfig

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
)

type ModelConfigStore interface {
	ModelConfigSnapshot(context.Context) (md.ModelConfig, error)
	ModelConfigCommit(context.Context, md.ModelConfig, uint64) error
	AcquireModelConfig(context.Context) (ports.Lease, error)
}

type Service struct {
	Cache      ports.ReadCache
	Store      ModelConfigStore
	Projects   ports.ModelReferences
	Cipher     ports.CredentialCipher
	Factory    ports.ModelFactory
	ReadOnly   bool
	Logos      ports.ModelLogoStore
	Directory  ports.ModelDirectory
	MockRandom io.Reader
	Tasks      *taskapp.Service
}

type ModelEnableInput struct {
	ResolutionEnabled map[string]bool       `json:"resolutionEnabled"`
	MockBehavior      *md.MockBehavior      `json:"mockBehavior,omitempty"`
	CreditPrice       *credits.PriceRule    `json:"creditPrice"`
	ID                string                `json:"id"`
	LogoID            string                `json:"logoId,omitempty"`
	ModelID           string                `json:"modelId"`
	Alias             string                `json:"alias"`
	Description       string                `json:"description"`
	Category          string                `json:"category"`
	Enabled           *bool                 `json:"enabled"`
	ContextWindow     int                   `json:"contextWindow"`
	MaxOutputTokens   int                   `json:"maxOutputTokens"`
	Pricing           *billing.TokenPricing `json:"pricing"`
}

type ConnectionInput struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Kind     string             `json:"kind"`
	Protocol string             `json:"protocol"`
	BaseURL  string             `json:"baseUrl"`
	Models   []ModelEnableInput `json:"models"`
}

type ModelConfigInput struct {
	Revision   uint64          `json:"revision"`
	Connection ConnectionInput `json:"connection"`
	APIKey     *string         `json:"apiKey,omitempty"`
}

func Unavailable() error {
	return fault.New("MODEL_CONFIG_UNAVAILABLE", "模型配置存储或密钥加密资源不可用")
}

func connectionActive(conn md.ModelConnection, projects []ports.ProjectModelReference) bool {
	for _, p := range projects {
		for _, r := range p.Runs {
			if r.Status != "running" {
				continue
			}
			id := r.Model
			if id == "" {
				id = md.DeepSeekModelID
			}
			for _, m := range conn.Models {
				if m.ID == id {
					return true
				}
			}
		}
	}
	return false
}

func modelReferenced(id string, projects []ports.ProjectModelReference) bool {
	for _, p := range projects {
		if p.HasSettings && (p.LLMModel == id || p.ImageModel == id || p.VideoModel == id || (p.LLMModel == "" && id == md.DeepSeekModelID)) {
			return true
		}
		for _, r := range p.Runs {
			if r.Model == id || (r.Model == "" && id == md.DeepSeekModelID) {
				return true
			}
		}
	}
	return false
}

func (c *Service) Snapshot(ctx context.Context) (md.ModelConfig, error) {
	cfg, err := c.Store.ModelConfigSnapshot(ctx)
	if err != nil {
		return cfg, err
	}
	cfg = md.WithOfficialPresets(cfg)
	// 写入校验与 GET 共用程序目录；新官方 ID 不属于用户自建 ID。
	// 仅在明确保存时解析快照，GET 不为首次展示写配置。
	for i, conn := range cfg.Connections {
		resolved, e := md.ResolveOfficial(conn)
		if e != nil {
			return cfg, e
		}
		cfg.Connections[i] = resolved
	}
	return cfg, nil
}

func (c *Service) writeGuard(ctx context.Context, rev uint64) (md.ModelConfig, []ports.ProjectModelReference, ports.Lease, error) {
	if c.ReadOnly {
		return md.ModelConfig{}, nil, ports.Lease{}, fault.New("SERVICE_READ_ONLY", "当前服务只读")
	}
	if c.Cipher == nil || c.Factory == nil {
		return md.ModelConfig{}, nil, ports.Lease{}, Unavailable()
	}
	release, e := c.Store.AcquireModelConfig(ctx)
	if e != nil {
		return md.ModelConfig{}, nil, ports.Lease{}, e
	}
	cfg, e := c.Snapshot(ctx)
	if e == nil && cfg.Revision != rev {
		e = fault.ErrConflict
	}
	var ps []ports.ProjectModelReference
	if e == nil {
		ps, e = c.Projects.ModelReferences(ctx)
	}
	if e != nil {
		release.Release()
		return cfg, nil, ports.Lease{}, e
	}
	return cfg, ps, release, nil
}

func validName(s string, n int) bool {
	return strings.TrimSpace(s) == s && s != "" && len(s) <= n && !strings.ContainsAny(s, "\r\n\x00")
}

func (c *Service) update(ctx context.Context, in ModelConfigInput) (ModelConfigView, error) {
	cfg, projects, release, e := c.writeGuard(ctx, in.Revision)
	if e != nil {
		return ModelConfigView{}, e
	}
	defer release.Release()
	ctx = release.Context
	v := in.Connection
	if !validName(v.Name, 120) || (v.Kind != "official" && v.Kind != "gateway" && v.Kind != "builtin") || (v.Kind == "gateway" && v.Protocol != "deepseek" && v.Protocol != "openai-chat") || len(v.Models) > 100 {
		return ModelConfigView{}, fault.ErrInvalid
	}
	if v.Kind != "builtin" {
		if e = md.ValidateModelURL(v.BaseURL); e != nil {
			return ModelConfigView{}, e
		}
	} else if in.APIKey != nil {
		return ModelConfigView{}, builtinFixed()
	}
	old := md.ModelConnection{}
	index := -1
	for i, conn := range cfg.Connections {
		if conn.ID == v.ID {
			old, index = conn, i
			break
		}
	}
	if v.ID != "" && index < 0 {
		return ModelConfigView{}, fault.ErrNotFound
	}
	if connectionActive(old, projects) {
		return ModelConfigView{}, fault.New("RUN_ACTIVE", "任务正在使用该连接，结束后可修改配置")
	}
	if v.Kind != old.Kind && index >= 0 {
		if old.Kind == "builtin" {
			return ModelConfigView{}, builtinFixed()
		}
		return ModelConfigView{}, officialFixed()
	}
	if v.Kind == "official" || v.Kind == "builtin" {
		fixed, ok := md.OfficialPreset(v.ID)
		if index < 0 || !ok || v.Kind != fixed.Kind || v.Name != fixed.Name || v.BaseURL != fixed.BaseURL || v.Protocol != fixed.Protocol || len(v.Models) != len(fixed.Models) {
			if v.Kind == "builtin" {
				return ModelConfigView{}, builtinFixed()
			}
			return ModelConfigView{}, officialFixed()
		}
		for _, input := range v.Models {
			matched := false
			for _, m := range fixed.Models {
				if m.ID == input.ID && m.ModelID == input.ModelID && m.Category == input.Category && m.ContextWindow == input.ContextWindow && m.MaxOutputTokens == input.MaxOutputTokens && reflect.DeepEqual(m.Pricing, input.Pricing) {
					matched = true
				}
			}
			if !matched {
				if v.Kind == "builtin" {
					return ModelConfigView{}, builtinFixed()
				}
				return ModelConfigView{}, officialFixed()
			}
		}
	}
	id := v.ID
	if id == "" {
		id = identity.New("connection")
	}
	next := md.ModelConnection{ID: id, Name: v.Name, Kind: v.Kind, Protocol: v.Protocol, BaseURL: v.BaseURL, Secret: old.Secret, Models: []md.ConfiguredModel{}}
	if len(old.Secret) > 0 && old.BaseURL != v.BaseURL && in.APIKey == nil {
		return ModelConfigView{}, fault.New("MODEL_KEY_REQUIRED", "更换 API 地址时请重新填写该地址的 API Key")
	}
	if in.APIKey != nil {
		if !validName(*in.APIKey, 4096) {
			return ModelConfigView{}, fault.New("MODEL_KEY_REQUIRED", "API Key 不能为空、包含换行或两端空白")
		}
		next.Secret, e = c.Cipher.Seal(*in.APIKey)
		if e != nil {
			return ModelConfigView{}, Unavailable()
		}
	}
	seen := map[string]bool{}
	kept := map[string]bool{}
	for _, input := range v.Models {
		if v.Kind == "builtin" && v.ID == "mock" {
			if input.MockBehavior == nil || !input.MockBehavior.Valid() {
				return ModelConfigView{}, fault.ErrInvalid
			}
		} else if input.MockBehavior != nil {
			return ModelConfigView{}, fault.ErrInvalid
		}
		input.Description = strings.TrimSpace(input.Description)
		if !utf8.ValidString(input.Description) || utf8.RuneCountInString(input.Description) > 200 || strings.ContainsFunc(input.Description, unicode.IsControl) {
			return ModelConfigView{}, fault.ErrInvalid
		}
		if input.Enabled == nil || !validName(input.ModelID, 200) || (input.Alias != "" && !validName(input.Alias, 120)) || (input.Category != "analysis" && input.Category != "image" && input.Category != "video") {
			return ModelConfigView{}, fault.ErrInvalid
		}
		key := input.Category + ":" + input.ModelID
		if seen[key] {
			return ModelConfigView{}, fault.ErrInvalid
		}
		seen[key] = true
		modelID := input.ID
		if modelID != "" {
			found := false
			for _, m := range old.Models {
				if m.ID == modelID {
					found = true
					if (m.ModelID != input.ModelID || m.Category != input.Category) && modelReferenced(modelID, projects) {
						return ModelConfigView{}, fault.New("MODEL_IN_USE", "模型已被项目引用，不能更改实际模型或类别")
					}
				}
			}
			if !found || kept[modelID] {
				return ModelConfigView{}, fault.ErrInvalid
			}
		} else {
			modelID = identity.New("model")
		}
		if input.LogoID != "" {
			if !validLogoID(input.LogoID) || c.Logos == nil {
				return ModelConfigView{}, invalidLogo()
			}
			if _, err := c.Logos.ModelLogo(ctx, input.LogoID); err != nil {
				if errors.Is(err, fault.ErrNotFound) {
					return ModelConfigView{}, invalidLogo()
				}
				return ModelConfigView{}, err
			}
		}
		kept[modelID] = true
		if input.ContextWindow < 0 || input.ContextWindow > 10000000 || input.MaxOutputTokens < 0 || input.MaxOutputTokens > input.ContextWindow || (input.Pricing != nil && !input.Pricing.Valid()) {
			return ModelConfigView{}, fault.New("INVALID_MODEL_PROFILE", "请输入有效容量和正数文本单价，缓存单价不能高于输入单价")
		}
		if input.Category != "analysis" && (input.ContextWindow != 0 || input.MaxOutputTokens != 0 || input.Pricing != nil) {
			return ModelConfigView{}, fault.ErrInvalid
		}
		if *input.Enabled && len(next.Secret) == 0 && v.Kind != "builtin" {
			return ModelConfigView{}, fault.New("MODEL_KEY_REQUIRED", "启用模型前请先配置 API Key")
		}
		if input.CreditPrice != nil && (!input.CreditPrice.Valid() || !validResolutionPrices(v.Kind, v.ID, input.ModelID, input.Category, input.CreditPrice)) {
			return ModelConfigView{}, fault.New("CREDIT_INVALID_AMOUNT", "积分价格或限免时段无效")
		}
		profile := md.OfficialMediaProfile(v.ID, input.ModelID)
		if v.Kind == "gateway" {
			profile = nil
		}
		for resolution, enabled := range input.ResolutionEnabled {
			if profile == nil || !profile.HasResolution(resolution) {
				return ModelConfigView{}, fault.New("MEDIA_OPTIONS_INVALID", "档位开关必须属于当前模型支持的分辨率")
			}
			if enabled {
				if input.CreditPrice == nil {
					return ModelConfigView{}, fault.New("MODEL_PRICE_UNAVAILABLE", "开启档位前请配置该档积分价格")
				}
				if _, ok := input.CreditPrice.AtResolution(resolution, profile.DefaultResolution); !ok {
					return ModelConfigView{}, fault.New("MODEL_PRICE_UNAVAILABLE", "开启档位前请配置该档积分价格")
				}
			}
		}
		next.Models = append(next.Models, md.ConfiguredModel{ResolutionEnabled: input.ResolutionEnabled, MockBehavior: input.MockBehavior, CreditPrice: input.CreditPrice, ID: modelID, LogoID: input.LogoID, ModelID: input.ModelID, Alias: input.Alias, Description: input.Description, Category: input.Category, Enabled: *input.Enabled, ContextWindow: input.ContextWindow, MaxOutputTokens: input.MaxOutputTokens, Pricing: input.Pricing})
	}
	for _, m := range old.Models {
		if !kept[m.ID] && modelReferenced(m.ID, projects) {
			return ModelConfigView{}, fault.New("MODEL_IN_USE", "模型已被项目引用，不能移除；可以关闭该模型")
		}
	}
	if index < 0 {
		cfg.Connections = append(cfg.Connections, next)
	} else {
		cfg.Connections[index] = next
	}
	cfg.Revision++
	if e = c.Store.ModelConfigCommit(ctx, cfg, in.Revision); e != nil {
		return ModelConfigView{}, e
	}
	return c.view(cfg, projects)
}

func (c *Service) delete(ctx context.Context, id string, rev uint64) (ModelConfigView, error) {
	cfg, ps, release, e := c.writeGuard(ctx, rev)
	if e != nil {
		return ModelConfigView{}, e
	}
	defer release.Release()
	ctx = release.Context
	index := -1
	for i, conn := range cfg.Connections {
		if conn.ID == id {
			index = i
			if conn.Kind == "builtin" {
				return ModelConfigView{}, builtinFixed()
			}
			if conn.Kind == "official" {
				return ModelConfigView{}, officialFixed()
			}
			if connectionActive(conn, ps) {
				return ModelConfigView{}, fault.New("RUN_ACTIVE", "任务正在使用该连接")
			}
			for _, m := range conn.Models {
				if modelReferenced(m.ID, ps) {
					return ModelConfigView{}, fault.New("MODEL_IN_USE", "连接中的模型已被项目引用，不能删除；可以关闭模型")
				}
			}
		}
	}
	if index < 0 {
		return ModelConfigView{}, fault.ErrNotFound
	}
	cfg.Connections = append(cfg.Connections[:index], cfg.Connections[index+1:]...)
	cfg.Revision++
	if e = c.Store.ModelConfigCommit(ctx, cfg, rev); e != nil {
		return ModelConfigView{}, e
	}
	return c.view(cfg, ps)
}

func officialFixed() error {
	return fault.New("OFFICIAL_CONNECTION_FIXED", "官方服务与模型目录由程序维护，可修改凭证、别名、描述、Logo、开关和积分价格")
}

func builtinFixed() error {
	return fault.New("BUILTIN_CONNECTION_FIXED", "本地测试服务与模型目录由程序维护，可修改别名、描述、Logo、开关和积分价格")
}

func validResolutionPrices(kind, connection, model, category string, r *credits.PriceRule) bool {
	if len(r.ResolutionPrices) == 0 {
		return true
	}
	if category == "analysis" || kind == "gateway" {
		return false
	}
	profile := md.OfficialMediaProfile(connection, model)
	for id := range r.ResolutionPrices {
		if !profile.HasResolution(id) {
			return false
		}
	}
	return true
}
