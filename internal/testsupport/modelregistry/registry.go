// Package modelregistry 只供免费测试注入模型端口；生产使用模型配置上下文。
package modelregistry

import (
	"context"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
)

type Registry struct {
	Model        ports.Model
	Models       map[string]ports.Model
	ModelPrices  map[string]billing.TokenPricing
	DefaultModel string
	OptionsValue ports.ModelOptions
}

func (r *Registry) Acquire(ctx context.Context) (ports.Lease, error) {
	return ports.Lease{Context: ctx, Release: func() {}}, ctx.Err()
}
func (r *Registry) Resolve(ctx context.Context, id string) (ports.ResolvedModel, error) {
	if err := ctx.Err(); err != nil {
		return ports.ResolvedModel{}, err
	}
	if id == "" {
		id = r.DefaultModel
		if id == "" {
			id = md.DeepSeekModelID
		}
	}
	model := r.Models[id]
	if r.Models == nil && id == md.DeepSeekModelID {
		model = r.Model
	}
	if model == nil {
		return ports.ResolvedModel{}, fault.ErrModelUnavailable
	}
	price := md.PricingForModel(id)
	if r.ModelPrices != nil {
		price = r.ModelPrices[id]
	}
	return ports.ResolvedModel{ID: id, Model: model, Pricing: price}, nil
}
func (r *Registry) Options(context.Context) (ports.ModelOptions, error) {
	out := r.OptionsValue
	if out.Analysis == nil {
		out.Analysis = []ports.ModelOption{}
		if r.Model != nil {
			out.Analysis = append(out.Analysis, ports.ModelOption{ID: md.DeepSeekModelID, Name: "DeepSeek Flash"})
		}
	}
	if out.Image == nil {
		out.Image = []ports.ModelOption{}
	}
	if out.Video == nil {
		out.Video = []ports.ModelOption{}
	}
	return out, nil
}
