package model

import (
	"context"
	"drama-agent/internal/adapter/appconfig"
	"fmt"

	"drama-agent/internal/application/ports"
	md "drama-agent/internal/domain/modelconfig"
)

// 协议决定供应商适配器；展示名/别名绝不参与路由。
// 应用拥有 Model 端口，不感知供应商 SDK。
func NewConfigured(ctx context.Context, key, endpoint, protocol string, profile md.ConfiguredModel, settings ...appconfig.Models) (ports.Model, error) {
	switch protocol {
	case "deepseek", "openai-chat":
		return newDeepSeekConfigured(ctx, key, endpoint, protocol, profile, settings...)
	case md.VolcengineProtocol:
		return NewVolcengine(ctx, key, endpoint, profile, settings...)
	case md.GoogleProtocol:
		return NewGemini(ctx, key, endpoint, profile, settings...)
	default:
		return nil, fmt.Errorf("unsupported model protocol")
	}
}
