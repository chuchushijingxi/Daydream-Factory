package modelconfig

import (
	"context"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
)

type DiscoverModelsInput struct {
	Revision     uint64  `json:"revision"`
	ConnectionID string  `json:"connectionId"`
	BaseURL      string  `json:"baseUrl"`
	Protocol     string  `json:"protocol"`
	APIKey       *string `json:"apiKey,omitempty"`
}

type DiscoverModelsView struct {
	Models []ports.RemoteModel `json:"models"`
}

func (c *Service) discoverModels(ctx context.Context, in DiscoverModelsInput) (DiscoverModelsView, error) {
	if c.ReadOnly {
		return DiscoverModelsView{}, fault.New("SERVICE_READ_ONLY", "当前服务只读")
	}
	if c.Directory == nil || c.Cipher == nil {
		return DiscoverModelsView{}, Unavailable()
	}
	cfg, e := c.Store.ModelConfigSnapshot(ctx)
	if e != nil {
		return DiscoverModelsView{}, e
	}
	if cfg.Revision != in.Revision {
		return DiscoverModelsView{}, fault.ErrConflict
	}
	if e = md.ValidateModelURL(in.BaseURL); e != nil {
		return DiscoverModelsView{}, e
	}
	if in.Protocol != "openai-chat" && in.Protocol != "deepseek" {
		return DiscoverModelsView{}, fault.ErrInvalid
	}
	key := ""
	if in.ConnectionID != "" {
		found := false
		for _, conn := range cfg.Connections {
			if conn.ID != in.ConnectionID {
				continue
			}
			found = true
			if conn.Kind == "official" {
				return DiscoverModelsView{}, officialFixed()
			}
			if in.APIKey == nil {
				if conn.BaseURL != in.BaseURL || len(conn.Secret) == 0 {
					return DiscoverModelsView{}, fault.New("MODEL_KEY_REQUIRED", "请填写该地址的API Key后获取模型列表")
				}
				key, e = c.Cipher.Open(conn.Secret)
				if e != nil {
					return DiscoverModelsView{}, Unavailable()
				}
			}
		}
		if !found {
			return DiscoverModelsView{}, fault.ErrNotFound
		}
	}
	if in.APIKey != nil {
		if !validName(*in.APIKey, 4096) {
			return DiscoverModelsView{}, fault.New("MODEL_KEY_REQUIRED", "请填写有效API Key")
		}
		key = *in.APIKey
	}
	if key == "" {
		return DiscoverModelsView{}, fault.New("MODEL_KEY_REQUIRED", "请填写API Key后获取模型列表")
	}
	models, e := c.Directory(ctx, key, in.BaseURL, in.Protocol)
	if e != nil {
		return DiscoverModelsView{}, e
	}
	return DiscoverModelsView{Models: models}, nil
}
