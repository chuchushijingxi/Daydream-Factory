package postgres

import (
	"context"
	"encoding/json"
	"log"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
)

func (s *Store) ModelConfigSnapshot(ctx context.Context) (md.ModelConfig, error) {
	var cfg md.ModelConfig
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT revision,connections FROM drama_catalog_v1.model_config WHERE id='workspace'`).Scan(&cfg.Revision, &raw)
	if err == nil {
		err = json.Unmarshal(raw, &cfg.Connections)
	}

	if err != nil {
		log.Printf("model configuration read failed: %v", err)
		return cfg, fault.New("MODEL_CONFIG_UNAVAILABLE", "模型配置存储未就绪，请检查服务迁移")
	}
	return cfg, nil
}

func (s *Store) ModelConfigCommit(ctx context.Context, cfg md.ModelConfig, expected uint64) error {
	if cfg.Revision != expected+1 {
		return fault.ErrInvalid
	}
	raw, err := json.Marshal(cfg.Connections)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `UPDATE drama_catalog_v1.model_config SET revision=$1,connections=$2 WHERE id='workspace' AND revision=$3`, cfg.Revision, raw, expected)

	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fault.ErrConflict
	}
	s.invalidateAfterCommit(ctx, "models")
	return nil
}

func (s *Store) AcquireModelConfig(ctx context.Context) (ports.Lease, error) {
	r, err := s.runtimeState()
	if err != nil {
		return ports.Lease{}, err
	}
	lease, _, err := r.Lock(ctx, "model-config", true)
	return lease, err
}
