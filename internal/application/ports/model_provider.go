package ports

import (
	"context"

	"drama-agent/internal/domain/billing"
)

// ResolvedModel 是本任务已解析的实际身份、模型与价格，凭证和配置实现不出此边界。
type ResolvedModel struct {
	ConnectionID   string
	RequestModelID string
	ID             string
	Model          Model
	Pricing        billing.TokenPricing
}

// ModelProvider 由应用拥有；项目只解析模型，配置上下文封装密钥、工厂和配置锁。
type ModelProvider interface {
	Acquire(context.Context) (Lease, error)
	Options(context.Context) (ModelOptions, error)
	Resolve(context.Context, string) (ResolvedModel, error)
}
