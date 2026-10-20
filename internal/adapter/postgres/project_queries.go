package postgres

import (
	"context"
	"encoding/json"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

// ListSummaries 保持现有项目排序与删除可见性，只读取页面需要的列。
func (s *Store) ListSummaries(ctx context.Context) ([]ports.ProjectSummary, error) {
	if s.owner != nil && *s.owner == "" {
		return nil, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	owner := "system"
	if s.owner != nil {
		owner = *s.owner
	}
	return readProjection(ctx, s, "projects:"+owner, s.loadProjectSummaries)
}
func (s *Store) loadProjectSummaries(ctx context.Context) ([]ports.ProjectSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,revision,payload->>'title',payload#>>'{source,filename}',(payload->>'uploadedAt')::timestamptz,
 CASE WHEN payload->'runs'->-1 IS NULL OR payload->'runs'->-1='null'::jsonb THEN NULL ELSE jsonb_build_object('id',payload->'runs'->-1->>'id','contract',payload->'runs'->-1->>'contract','status',payload->'runs'->-1->>'status','stage',payload->'runs'->-1->>'stage') END,
 ARRAY(SELECT a.asset->>'imageFileId'
 FROM jsonb_array_elements(coalesce(nullif(payload->'assets','null'::jsonb),'[]'::jsonb)) WITH ORDINALITY a(asset,position)
 WHERE a.asset->>'kind'='character' AND coalesce(a.asset->>'imageFileId','')<>''
 ORDER BY a.position LIMIT 3)
 FROM drama_catalog_v1.projects WHERE payload->>'deletedAt' IS NULL AND ($1::text IS NULL OR (owner_id=$1 AND $1<>'')) ORDER BY updated_at DESC`, s.owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.ProjectSummary{}
	for rows.Next() {
		var p ports.ProjectSummary
		var run []byte
		if err = rows.Scan(&p.ID, &p.Revision, &p.Title, &p.Filename, &p.UploadedAt, &run, &p.PreviewImageFileIDs); err != nil {
			return nil, err
		}
		if run != nil {
			if err = json.Unmarshal(run, &p.LatestRun); err != nil {
				return nil, err
			}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) RunningProjectIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM drama_catalog_v1.projects WHERE payload->>'deletedAt' IS NULL AND payload->'runs'->-1->>'status'='running' ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) ModelReferences(ctx context.Context) ([]ports.ProjectModelReference, error) {
	rows, err := s.pool.Query(ctx, `SELECT jsonb_build_object(
 'hasSettings',coalesce(p.settings,'null'::jsonb)<>'null'::jsonb,
 'llmModel',p.settings->>'llmModel','imageModel',p.settings->>'imageModel','videoModel',p.settings->>'videoModel',
 'runs',coalesce((SELECT jsonb_agg(jsonb_build_object('model',r.model,'status',r.status))
 FROM jsonb_to_recordset(coalesce(nullif(p.runs,'null'::jsonb),'[]'::jsonb)) AS r(model jsonb,status jsonb)),'[]'::jsonb))
 FROM drama_catalog_v1.projects CROSS JOIN LATERAL jsonb_to_record(payload) AS p(settings jsonb,runs jsonb)
 ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.ProjectModelReference{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p ports.ProjectModelReference
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Only terminal operations whose actual debit still needs a refund are recovered.
// No body is loaded for unrelated projects and no new generation is started.
func (s *Store) UnsettledCreditProjectIDs(ctx context.Context) ([]string, error) {
	rows, e := s.pool.Query(ctx, `SELECT DISTINCT p.id FROM drama_catalog_v1.projects p CROSS JOIN LATERAL jsonb_array_elements(coalesce(nullif(p.payload->'runs','null'::jsonb),'[]'::jsonb)) r JOIN drama_catalog_v1.credit_ledger debit ON debit.operation_id='generation:'||(r#>>'{credit,id}')||':deduct' WHERE p.owner_id IS NOT NULL AND r->>'status' IN ('failed','cancelled') AND NOT EXISTS(SELECT 1 FROM drama_catalog_v1.credit_ledger refund WHERE refund.operation_id='generation:'||(r#>>'{credit,id}')||':refund') ORDER BY p.id LIMIT 100`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
