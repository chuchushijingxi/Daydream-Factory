package postgres

import (
	"context"

	"drama-agent/internal/application/ports"
)

func (s *Store) PendingImageFills(ctx context.Context, after string, limit int) ([]ports.MediaImageFill, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.owner_id,p.id,t.id
 FROM drama_catalog_v1.tasks t
 JOIN drama_catalog_v1.projects p ON p.id=substring(t.scope_id from 9) AND p.owner_id=t.owner_id
 JOIN LATERAL jsonb_array_elements(coalesce(nullif(p.payload->'assets','null'::jsonb),'[]'::jsonb)) a ON a->>'id'=t.input_snapshot#>>'{callerContext,request,target,assetId}'
 LEFT JOIN LATERAL jsonb_array_elements(coalesce(nullif(a->'looks','null'::jsonb),'[]'::jsonb)) l ON l->>'id'=t.input_snapshot#>>'{callerContext,request,target,lookId}'
 WHERE t.id>$1 AND t.status='succeeded' AND t.input_snapshot#>>'{callerContext,fillMissingImage}'='true'
 AND p.payload->>'deletedAt' IS NULL
 AND CASE WHEN coalesce(t.input_snapshot#>>'{callerContext,request,target,lookId}','')=''
 THEN coalesce(a->>'imageFileId','')='' ELSE l IS NOT NULL AND coalesce(l->>'imageFileId','')='' END
 ORDER BY t.id LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ports.MediaImageFill{}
	for rows.Next() {
		var item ports.MediaImageFill
		if err := rows.Scan(&item.OwnerID, &item.ProjectID, &item.TaskID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
