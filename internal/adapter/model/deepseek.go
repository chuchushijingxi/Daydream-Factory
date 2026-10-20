package model

import (
	"context"
	"drama-agent/internal/adapter/appconfig"
	"fmt"

	md "drama-agent/internal/domain/modelconfig"

	eino "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

type DeepSeek struct{ *chatModel }

// 该构造器使用已核实的官方 DeepSeek 档案，供测试装配。
func New(ctx context.Context, key, endpoint, modelID string) (*DeepSeek, error) {
	if !md.IsDeepSeekModel(modelID) {
		return nil, fmt.Errorf("unsupported DeepSeek model")
	}
	price := md.PricingForModel(modelID)
	return newDeepSeekConfigured(ctx, key, endpoint, "deepseek", md.ConfiguredModel{ModelID: modelID, Category: "analysis", ContextWindow: 1000000, MaxOutputTokens: 393216, Pricing: &price})
}

func newDeepSeekConfigured(ctx context.Context, key, endpoint, protocol string, profile md.ConfiguredModel, settings ...appconfig.Models) (*DeepSeek, error) {
	if key == "" || endpoint == "" || profile.ModelID == "" || !profile.Executable() {
		return nil, fmt.Errorf("model configuration is incomplete")
	}
	if protocol != "deepseek" && protocol != "openai-chat" {
		return nil, fmt.Errorf("unsupported model protocol")
	}
	client := newModelClient(settings...)
	conf := &eino.ChatModelConfig{APIKey: key, Model: profile.ModelID, BaseURL: endpoint, HTTPClient: client}
	if protocol == "deepseek" {
		// 使用同一 OpenAI 兼容协议组件，避免 DeepSeek SDK 附带未使用的
		// Ollama 服务依赖；思考开关仍只发送给 DeepSeek 连接。
		conf.ExtraFields = map[string]any{"thinking": map[string]string{"type": "enabled"}}
	}
	component, err := eino.NewChatModel(ctx, conf)
	if err != nil {
		return nil, fmt.Errorf("model configuration failed")
	}
	return &DeepSeek{chatModel: &chatModel{component: component, key: key, window: profile.ContextWindow, output: profile.MaxOutputTokens, outputOption: model.WithMaxTokens}}, nil
}
