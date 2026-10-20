package ports

import (
	"context"

	d "drama-agent/internal/domain/project"
)

// EvidenceDocument 是不可变的索引输入；PostgreSQL 始终是原文权威，
// 向量命中只负责定位候选段落。
type EvidenceDocument struct {
	ProjectID string
	Source    d.ScriptSource
}

type EvidenceHit struct {
	SpanID     string  `json:"spanId"`
	ChunkIndex int     `json:"chunkIndex"`
	Score      float64 `json:"score"`
}

type EvidenceIndex interface {
	Ensure(context.Context, EvidenceDocument) error
	Search(context.Context, EvidenceDocument, string, int, int) ([]EvidenceHit, bool, error)
}

// Embedder 是本地学习推理，独立于项目的付费分析模型；
// 身份包含权重与实现版本。
type Embedder interface {
	Identity() string
	Dimension() int
	Embed(context.Context, []string, bool) ([][]float32, error)
}
