package milvusevidence

import (
	"context"
	"sync"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

// Lazy 将检索资源初始化限定在视觉任务内；普通服务启动不读取权重。
// 成功后共用同一索引，失败不缓存，以便补齐资源后的新任务重新初始化。
type Lazy struct {
	mu      sync.Mutex
	factory func() (ports.EvidenceIndex, error)
	index   ports.EvidenceIndex
}

func NewLazy(factory func() (ports.EvidenceIndex, error)) *Lazy {
	return &Lazy{factory: factory}
}

func (s *Lazy) load(ctx context.Context) (ports.EvidenceIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.index != nil {
		return s.index, nil
	}
	if s.factory == nil {
		return nil, fault.New("EVIDENCE_UNAVAILABLE", "原文检索资源尚未配置")
	}
	index, err := s.factory()
	if err != nil {
		return nil, fault.New("EVIDENCE_UNAVAILABLE", "原文检索资源初始化失败："+err.Error())
	}
	if index == nil {
		return nil, fault.New("EVIDENCE_UNAVAILABLE", "原文检索资源初始化未返回索引")
	}
	s.index = index
	return index, nil
}

func (s *Lazy) Ensure(ctx context.Context, doc ports.EvidenceDocument) error {
	index, err := s.load(ctx)
	if err != nil {
		return err
	}
	return index.Ensure(ctx, doc)
}

func (s *Lazy) Search(ctx context.Context, doc ports.EvidenceDocument, query string, limit, offset int) ([]ports.EvidenceHit, bool, error) {
	index, err := s.load(ctx)
	if err != nil {
		return nil, false, err
	}
	return index.Search(ctx, doc, query, limit, offset)
}
