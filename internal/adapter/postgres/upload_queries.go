package postgres

import (
	"context"
	"strings"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

var _ ports.UploadedFileQueries = (*Store)(nil)

// The selector only returns upload metadata after SQL visibility filtering. A
// readable container may resolve its own file references through a separate path.
func (s *Store) ListUploadedFiles(ctx context.Context, scope ports.SpaceScope, q ports.UploadedFileQuery) ([]ports.UploadedFileItem, error) {
	if scope.SpaceID == "" || scope.UserID == "" {
		return nil, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	filter, err := q.Filter.Normalize()
	if err != nil {
		return nil, err
	}
	if q.Limit < 1 || q.Limit > 101 {
		return nil, fault.ErrInvalid
	}
	var at *time.Time
	id := ""
	if q.After != nil {
		at = &q.After.At
		id = q.After.ID
	}
	pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q.Search) + "%"
	access := spaceAccessSQL("v", "f.space_id", "f.owner_id", "'upload'", "0")
	rows, err := s.pool.Query(ctx, `WITH `+visibleScopeSQL("$1", "$2")+`
 SELECT f.id,f.space_id,f.owner_id,f.payload->>'filename',f.payload#>>'{original,contentType}',(f.payload#>>'{original,sizeBytes}')::bigint,
 coalesce(f.payload->'thumbnail','null'::jsonb)<>'null'::jsonb,f.created_at,f.creator_id,coalesce(u.username,''),false,access.effective
 FROM drama_catalog_v1.uploaded_files f JOIN visible_scope v ON v.space_id=f.space_id
 LEFT JOIN drama_catalog_v1.users u ON u.id=f.creator_id CROSS JOIN LATERAL(SELECT `+access+` AS effective)access
 WHERE access.effective>0 AND ($3='all' OR ($3='mine' AND f.creator_id=v.user_id))
 AND ($4='' OR f.payload->>'filename' ILIKE $5)
 AND ($6::timestamptz IS NULL OR (f.created_at,f.id)<($6,$7))
 ORDER BY f.created_at DESC,f.id DESC LIMIT $8`, scope.SpaceID, scope.UserID, filter, q.Search, pattern, at, id, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.UploadedFileItem{}
	for rows.Next() {
		var v ports.UploadedFileItem
		if err = rows.Scan(&v.ID, &v.SpaceID, &v.OwnerID, &v.Filename, &v.ContentType, &v.SizeBytes, &v.HasThumbnail, &v.CreatedAt, &v.CreatorID, &v.CreatorName, &v.SharedWithMe, &v.EffectiveAccess); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
