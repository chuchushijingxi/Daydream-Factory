package modelconfig

import (
	"net"
	"net/url"
	"strings"

	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
)

const DeepSeekModelID = "deepseek-flash"

const DeepSeekProModelID = "deepseek-v4-pro"

const DeepSeekBaseURL = "https://api.deepseek.com"

// Secret 仅保存密文，不进入公开视图或项目；别名不改变稳定模型 ID。
type ModelConfig struct {
	Revision    uint64
	Connections []ModelConnection
}

type ModelConnection struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Kind     string            `json:"kind"`
	Protocol string            `json:"protocol"`
	BaseURL  string            `json:"baseUrl"`
	Secret   []byte            `json:"secret"`
	Models   []ConfiguredModel `json:"models"`
}

type ConfiguredModel struct {
	ResolutionEnabled map[string]bool       `json:"resolutionEnabled"`
	MockBehavior      *MockBehavior         `json:"mockBehavior"`
	CreditPrice       *credits.PriceRule    `json:"creditPrice"`
	ID                string                `json:"id"`
	LogoID            string                `json:"logoId"`
	ModelID           string                `json:"modelId"`
	Alias             string                `json:"alias"`
	Description       string                `json:"description"`
	Category          string                `json:"category"`
	Enabled           bool                  `json:"enabled"`
	ContextWindow     int                   `json:"contextWindow"`
	MaxOutputTokens   int                   `json:"maxOutputTokens"`
	Pricing           *billing.TokenPricing `json:"pricing"`
}

func (m ConfiguredModel) DisplayName() string {
	if m.Alias != "" {
		return m.Alias
	}
	return m.DefaultName()
}

func (m ConfiguredModel) DefaultName() string {
	switch m.ModelID {
	case MockImageModelID:
		return "Mock Image"
	case MockVideoModelID:
		return "Mock Video"
	case DeepSeekModelID:
		return "DeepSeek Flash"
	case DeepSeekProModelID:
		return "DeepSeek Pro"
	}
	if name := volcengineModelName(m.ModelID); name != "" {
		return name
	}
	if name := klingModelName(m.ModelID, m.Category); name != "" {
		return name
	}
	if name := alibabaModelName(m.ModelID); name != "" {
		return name
	}
	if name := googleModelName(m.ModelID); name != "" {
		return name
	}
	if name := openaiModelName(m.ModelID); name != "" {
		return name
	}
	return m.ModelID
}

func (m ConfiguredModel) Executable() bool {
	return m.Category == "analysis" && m.ContextWindow > 0 && m.MaxOutputTokens > 0 && m.Pricing != nil && m.Pricing.Valid()
}

func (c ModelConfig) HasCredentials() bool {
	for _, v := range c.Connections {
		if len(v.Secret) > 0 {
			return true
		}
	}
	return false
}

func DefaultModelConfig() ModelConfig {
	flash, pro := PricingForModel(DeepSeekModelID), PricingForModel(DeepSeekProModelID)
	return ModelConfig{Revision: 1, Connections: []ModelConnection{{ID: "deepseek", Name: "DeepSeek", Kind: "official", Protocol: "deepseek", BaseURL: DeepSeekBaseURL, Models: []ConfiguredModel{
		{ID: DeepSeekModelID, ModelID: DeepSeekModelID, Category: "analysis", ContextWindow: 1000000, MaxOutputTokens: 393216, Pricing: &flash},
		{ID: DeepSeekProModelID, ModelID: DeepSeekProModelID, Category: "analysis", ContextWindow: 1000000, MaxOutputTokens: 393216, Pricing: &pro},
	}}, volcenginePreset(), klingPreset(), alibabaPreset(), googlePreset(), openaiPreset(), mockPreset()}}
}

func IsDeepSeekModel(id string) bool { return id == DeepSeekModelID || id == DeepSeekProModelID }

func ValidateModelURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || len(value) > 2048 || strings.TrimSpace(value) != value || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fault.New("INVALID_MODEL_URL", "请输入完整的 HTTPS API 地址，不包含密钥、查询参数或片段")
	}
	local := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		local = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return fault.New("INVALID_MODEL_URL", "API 地址须使用 HTTPS；本地测试可使用回环 HTTP 地址")
	}
	return nil
}

// 官方适配、端点及模型元数据由程序维护；用户只管理凭证和展示覆盖。
func OfficialPreset(id string) (ModelConnection, bool) {
	for _, c := range DefaultModelConfig().Connections {
		if c.ID == id {
			return c, true
		}
	}
	return ModelConnection{}, false
}

func ResolveOfficial(c ModelConnection) (ModelConnection, error) {
	if c.Kind != "official" && c.Kind != "builtin" {
		return c, nil
	}
	preset, ok := OfficialPreset(c.ID)
	if !ok || preset.Kind != c.Kind {
		return c, fault.New("MODEL_CONFIG_UNAVAILABLE", "官方适配未接入")
	}
	preset.Secret = c.Secret
	for i := range preset.Models {
		for _, old := range c.Models {
			if preset.Models[i].ID == old.ID {
				preset.Models[i].Alias, preset.Models[i].LogoID, preset.Models[i].Enabled = old.Alias, old.LogoID, old.Enabled
				preset.Models[i].CreditPrice = old.CreditPrice
				preset.Models[i].ResolutionEnabled = old.ResolutionEnabled
				preset.Models[i].Description = old.Description
				if c.ID == "mock" && old.MockBehavior != nil {
					behavior := *old.MockBehavior
					preset.Models[i].MockBehavior = &behavior
				}
			}
		}
	}
	return preset, nil
}

// 新官方目录在 GET 投影中展示，读取不修改存储；凭证为空、开关默认关闭。
// 只有明确 CAS 保存才持久化配置。
func WithOfficialPresets(cfg ModelConfig) ModelConfig {
	out := ModelConfig{Revision: cfg.Revision, Connections: append([]ModelConnection(nil), cfg.Connections...)}
	for _, preset := range DefaultModelConfig().Connections {
		found := false
		for _, saved := range cfg.Connections {
			if saved.ID == preset.ID {
				found = true
				break
			}
		}
		if !found {
			out.Connections = append(out.Connections, preset)
		}
	}
	return out
}
