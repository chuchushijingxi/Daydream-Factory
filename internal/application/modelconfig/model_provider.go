package modelconfig

import (
	"context"
	"encoding/json"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
)

var _ ports.ModelProvider = (*Service)(nil)

// 启动与设置保存沿用同一配置锁，避免解析模型与写配置互相穿越。
func (c *Service) Acquire(ctx context.Context) (ports.Lease, error) {
	return c.Store.AcquireModelConfig(ctx)
}

func (c *Service) Options(ctx context.Context) (ports.ModelOptions, error) {
	v, err := c.publicView(ctx)
	if err != nil {
		return ports.ModelOptions{}, err
	}
	return optionsFromView(v), nil
}

func (c *Service) publicView(ctx context.Context) (ModelConfigView, error) {
	load := func(ctx context.Context) ([]byte, error) {
		cfg, err := c.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		v, err := c.view(cfg, nil)
		if err != nil {
			return nil, err
		}
		return json.Marshal(v)
	}
	var raw []byte
	var err error
	if c.Cache != nil {
		key := "models:public"
		if c.ReadOnly {
			key += "-readonly"
		}
		raw, err = c.Cache.ReadCached(ctx, key, 30*time.Second, load)
	} else {
		raw, err = load(ctx)
	}
	if err != nil {
		return ModelConfigView{}, err
	}
	var v ModelConfigView
	if err = json.Unmarshal(raw, &v); err != nil {
		return ModelConfigView{}, err
	}
	return v, nil
}

func optionsFromView(v ModelConfigView) ports.ModelOptions {
	out := ports.ModelOptions{Analysis: []ports.ModelOption{}, Image: []ports.ModelOption{}, Video: []ports.ModelOption{}}
	for _, conn := range v.Connections {
		for _, m := range conn.Models {
			profile := m.EnabledMediaProfile(m.MediaProfile)
			price := m.CreditPrice
			if profile != nil && price != nil && len(profile.Resolutions) > 0 {
				// Public price ranges include only available tiers. The persisted rule
				// and its original default remain untouched for billing/history.
				projected := *price
				projected.ResolutionPrices = map[string]int64{}
				for _, r := range profile.Resolutions {
					if rate, ok := price.AtResolution(r.ID, m.MediaProfile.DefaultResolution); ok {
						projected.ResolutionPrices[r.ID] = rate.UnitPrice
						if r.ID == profile.DefaultResolution {
							projected.UnitPrice = rate.UnitPrice
						}
					}
				}
				price = &projected
			}
			unit := map[string]string{"analysis": "characters-100", "image": "image", "video": "second"}[m.Category]
			free := m.CreditPrice != nil && m.CreditPrice.Free(time.Now().UTC())
			option := ports.ModelOption{MediaProfile: profile, BillingUnit: unit, Free: free, CreditPrice: price, PriceVersion: v.Revision, ID: m.ID, LogoURL: m.LogoURL, Name: m.Name, Description: m.Description, ConnectionName: conn.Name, Provider: m.Provider, ModelID: m.ModelID}
			out.Catalog = append(out.Catalog, option)
			switch m.Category {
			case "analysis":
				if m.Available {
					out.Analysis = append(out.Analysis, option)
				}
			case "image", "video":
				// 候选只登记媒体偏好，不构造供应商或创建任务。
				if (profile == nil || !profile.Retired && len(profile.Resolutions) > 0) && ((conn.Kind == "builtin" && m.Available) || (v.Editable && conn.CredentialConfigured && m.Enabled)) {
					if m.Category == "image" {
						out.Image = append(out.Image, option)
					} else {
						out.Video = append(out.Video, option)
					}
				}
			}
		}
	}
	return out
}

// MediaPrice resolves switches, raw prices and their revision in one snapshot.
func (c *Service) MediaPrice(ctx context.Context, id string) (ports.MediaPricing, error) {
	cfg, err := c.Snapshot(ctx)
	if err != nil {
		return ports.MediaPricing{}, err
	}
	for _, conn := range cfg.Connections {
		for _, m := range conn.Models {
			if m.ID != id {
				continue
			}
			if c.ReadOnly || !m.Enabled || (conn.Kind != "builtin" && len(conn.Secret) == 0) {
				return ports.MediaPricing{}, fault.ErrModelUnavailable
			}
			if m.CreditPrice == nil || !m.CreditPrice.Valid() {
				return ports.MediaPricing{}, fault.New("MODEL_PRICE_UNAVAILABLE", "模型尚未配置积分价格")
			}
			profile := md.OfficialMediaProfile(conn.ID, m.ModelID)
			if conn.Kind == "gateway" || profile == nil {
				return ports.MediaPricing{}, fault.New("MEDIA_PROFILE_UNAVAILABLE", "模型尚无已核实的媒体规格")
			}
			return ports.MediaPricing{Rule: *m.CreditPrice, Version: cfg.Revision, ConnectionID: conn.ID, Profile: m.EnabledMediaProfile(profile), PriceDefaultResolution: profile.DefaultResolution}, nil
		}
	}
	return ports.MediaPricing{}, fault.ErrModelUnavailable
}

func (c *Service) Resolve(ctx context.Context, id string) (ports.ResolvedModel, error) {
	if id == "" {
		return ports.ResolvedModel{}, fault.New("MODEL_NOT_SELECTED", "请先确认项目的分析模型，系统不会自动选择其他模型")
	}
	cfg, err := c.Snapshot(ctx)
	if err != nil {
		return ports.ResolvedModel{}, err
	}
	if c.ReadOnly {
		return ports.ResolvedModel{}, fault.ErrModelUnavailable
	}
	for _, conn := range cfg.Connections {
		for _, m := range conn.Models {
			if m.ID != id {
				continue
			}
			if !m.Enabled || !m.Executable() || len(conn.Secret) == 0 {
				return ports.ResolvedModel{}, fault.ErrModelUnavailable
			}
			if c.Cipher == nil || c.Factory == nil {
				return ports.ResolvedModel{}, Unavailable()
			}
			// 只解密任务所选连接；别名仅展示，真实请求 ID 和价格来自该稳定实例。
			key, err := c.Cipher.Open(conn.Secret)
			if err != nil {
				return ports.ResolvedModel{}, Unavailable()
			}
			model, err := c.Factory(ctx, key, conn.BaseURL, conn.Protocol, m)
			if err != nil {
				return ports.ResolvedModel{}, Unavailable()
			}
			if model == nil {
				return ports.ResolvedModel{}, fault.ErrModelUnavailable
			}
			return ports.ResolvedModel{ConnectionID: conn.ID, RequestModelID: m.ModelID, ID: m.ID, Model: model, Pricing: *m.Pricing}, nil
		}
	}
	return ports.ResolvedModel{}, fault.ErrModelUnavailable
}

func (c *Service) CreditPrice(ctx context.Context, id string) (credits.PriceRule, uint64, string, error) {
	cfg, e := c.Snapshot(ctx)
	if e != nil {
		return credits.PriceRule{}, 0, "", e
	}
	for _, conn := range cfg.Connections {
		for _, m := range conn.Models {
			if m.ID == id {
				if !m.Enabled {
					return credits.PriceRule{}, 0, "", fault.ErrModelUnavailable
				}
				if m.CreditPrice == nil || !m.CreditPrice.Valid() {
					return credits.PriceRule{}, 0, "", fault.New("MODEL_PRICE_UNAVAILABLE", "模型尚未配置积分价格")
				}
				return *m.CreditPrice, cfg.Revision, conn.ID, nil
			}
		}
	}
	return credits.PriceRule{}, 0, "", fault.ErrModelUnavailable
}

// UserOptions contains enabled, priced candidates only; internal model-reference
// projections retain the full catalog for historical identities.
func (c *Service) UserOptions(ctx context.Context) (ports.ModelOptions, error) {
	v, e := c.publicView(ctx)
	if e != nil {
		return ports.ModelOptions{}, e
	}
	options := optionsFromView(v)
	allowed := map[string]bool{}
	for _, conn := range v.Connections {
		for _, m := range conn.Models {
			// Candidates describe configured preferences, including priced Mock
			// models. The media start use case enforces supported billing before
			// submitting work; discovery must not silently hide configured prices.
			if m.Enabled && m.CreditPrice != nil && m.CreditPrice.Valid() && (conn.Kind == "builtin" || conn.CredentialConfigured) {
				p := m.EnabledMediaProfile(m.MediaProfile)
				allowed[m.ID] = p == nil || !p.Retired && len(p.Resolutions) > 0
			}
		}
	}
	filter := func(in []ports.ModelOption) []ports.ModelOption {
		out := []ports.ModelOption{}
		for _, v := range in {
			if allowed[v.ID] {
				out = append(out, v)
			}
		}
		return out
	}
	options.Catalog = filter(options.Catalog)
	options.Analysis = filter(options.Analysis)
	options.Image = filter(options.Image)
	options.Video = filter(options.Video)
	return options, nil
}
