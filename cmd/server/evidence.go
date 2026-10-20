package main

import (
	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/localembedding"
	"drama-agent/internal/adapter/milvusevidence"
	"drama-agent/internal/application/ports"
)

func configuredEvidence(cfg appconfig.Evidence) ports.EvidenceIndex {
	return milvusevidence.NewLazy(func() (ports.EvidenceIndex, error) {
		embedder, err := localembedding.New(cfg.Python, cfg.ModelDir, cfg)
		if err != nil {
			return nil, err
		}
		return milvusevidence.New(cfg.Endpoint, cfg.Collection, embedder, cfg)
	})
}
