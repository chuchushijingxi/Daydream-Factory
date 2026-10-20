package model

import (
	"context"
	"drama-agent/internal/adapter/appconfig"
	"fmt"

	md "drama-agent/internal/domain/modelconfig"

	"github.com/cloudwego/eino-ext/components/model/ark"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// Volcengine 使用原生 Eino Ark 适配器及其厂商专用 token 预算；
// 请求捕获、取消与响应记账仍走共享路径。
type Volcengine struct{ *chatModel }

func NewVolcengine(ctx context.Context, key, endpoint string, profile md.ConfiguredModel, settings ...appconfig.Models) (*Volcengine, error) {
	if key == "" || profile.ModelID == "" || !profile.Executable() {
		return nil, fmt.Errorf("model configuration is incomplete")
	}
	noRetries := 0 // 持久化且独立记账的重试上限由应用层拥有。
	component, err := ark.NewChatModel(ctx, &ark.ChatModelConfig{
		APIKey: key, Model: profile.ModelID, BaseURL: endpoint,
		HTTPClient: newModelClient(settings...), RetryTimes: &noRetries,
		Thinking: &arkmodel.Thinking{Type: arkmodel.ThinkingTypeEnabled},
	})
	if err != nil {
		return nil, fmt.Errorf("model configuration failed")
	}
	return &Volcengine{chatModel: &chatModel{component: component, key: key, window: profile.ContextWindow, output: profile.MaxOutputTokens, outputOption: ark.WithMaxCompletionTokens}}, nil
}
