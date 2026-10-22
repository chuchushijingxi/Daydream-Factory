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

var _ ports.ProjectReader = (*Store)(nil)

// 只读页面保留调用数、状态、Usage、费用与未知预留，省略原件及调用正文。
// 原 PG payload 和完整 Snapshot 不变，投影禁止用于提交或恢复。
func (s *Store) ReadProject(ctx context.Context, id string) (d.Project, error) {
	if s.space != nil && *s.space == "" {
		return d.Project{}, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	var raw []byte
	var space, owner, creator string
	err := s.pool.QueryRow(ctx, `SELECT jsonb_set(payload-'original','{runs}',(
 SELECT coalesce(jsonb_agg(jsonb_set(r,'{calls}',(
   SELECT coalesce(jsonb_agg(c-ARRAY['request','response','text','reasoning',
     'providerState','actions','before','after'] ORDER BY cat),'[]'::jsonb)
   FROM jsonb_array_elements(coalesce(nullif(r->'calls','null'::jsonb),'[]'::jsonb))
     WITH ORDINALITY AS calls(c,cat)
 )) ORDER BY at),'[]'::jsonb)
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
