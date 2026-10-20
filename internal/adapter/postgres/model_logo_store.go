package postgres

import (
	"context"
	"errors"

	"drama-agent/internal/domain/fault"

	"github.com/jackc/pgx/v5"
)

func (s *Store) SaveModelLogo(ctx context.Context, id string, png []byte) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.model_logos(id,png) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, png)
	return err
}

func (s *Store) ModelLogo(ctx context.Context, id string) ([]byte, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT png FROM drama_catalog_v1.model_logos WHERE id=$1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fault.ErrNotFound
	}
	return raw, err
}
