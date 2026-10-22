package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"github.com/jackc/pgx/v5"
)

var _ ports.ProjectMediaReader = (*Store)(nil)

// 媒体校验不读取原件或模型调用正文。只在查询结果中省略两者，
// 原PG事实保留；其余范围、来源、任务状态及定稿事实仍交给原领域规则判断。
func (s *Store) MediaProject(ctx context.Context, id string) (d.Project, error) {
	if s.space != nil && *s.space == "" {
		return d.Project{}, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	var raw []byte
	var space, owner, creator string
	err := s.pool.QueryRow(ctx, `SELECT jsonb_set(payload-'original','{runs}',(
 SELECT coalesce(jsonb_agg(r-'calls' ORDER BY at),'[]'::jsonb)
 FROM jsonb_array_elements(coalesce(nullif(payload->'runs','null'::jsonb),'[]'::jsonb)) WITH ORDINALITY AS runs(r,at)
 )),space_id,owner_id,creator_id FROM drama_catalog_v1.projects
 WHERE id=$1 AND ($2::text IS NULL OR (space_id=$2 AND $2<>''))`, id, s.space).Scan(&raw, &space, &owner, &creator)
	if errors.Is(err, pgx.ErrNoRows) {
		return d.Project{}, fault.ErrNotFound
	}
	if err != nil {
		return d.Project{}, err
	}
	var p d.Project
	err = json.Unmarshal(raw, &p)
	p.SpaceID, p.OwnerID, p.CreatorID = space, owner, creator
	return p, err
}
