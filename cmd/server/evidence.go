package main

import (
	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/localembedding"
	"drama-agent/internal/adapter/milvusevidence"
	"drama-agent/internal/application/ports"
)

func configuredEvidence(cfg appconfig.Evidence) ports.EvidenceIndex {
	return milvusevidence.NewLazy(func() (ports.EvidenceIndex, error) {
		embedder, err := configuredEmbedder(cfg)
		if err != nil {
			return nil, err
		}
		return milvusevidence.New(cfg.Endpoint, cfg.Collection, embedder, cfg)
	})
}

// configuredEmbedder 在 Compose 中使用独立检索服务；主机直跑时调用本机 Python 与模型。
func configuredEmbedder(cfg appconfig.Evidence) (ports.Embedder, error) {
	if cfg.EmbeddingEndpoint != "" {
		return localembedding.NewRemote(cfg.EmbeddingEndpoint, cfg)
	}
	return localembedding.New(cfg.Python, cfg.ModelDir, cfg)
}
