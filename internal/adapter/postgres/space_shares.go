package postgres

import (
	"context"
	"errors"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/spaces"
	"github.com/jackc/pgx/v5"
)

// SharingResource locks the actual container in the same team transaction. A
// canvas is deliberately absent until its owning module supplies real storage.
func (t *spaceTx) SharingResource(ctx context.Context, kind, id string) (d.Resource, error) {
	r := d.Resource{Kind: kind, ID: id}
	if kind != d.ProjectResource {
		return r, fault.ErrNotFound
	}
	err := t.tx.QueryRow(ctx, `SELECT space_id,owner_id FROM drama_catalog_v1.projects WHERE id=$1 AND space_id=$2 AND payload->>'deletedAt' IS NULL FOR UPDATE`, id, t.id).Scan(&r.SpaceID, &r.OwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return r, err
}

func resourceShares(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, space, kind, id string) ([]d.Share, error) {
	rows, err := q.Query(ctx, `SELECT grantee,permission FROM drama_catalog_v1.space_shares WHERE space_id=$1 AND resource_kind=$2 AND resource_id=$3 ORDER BY grantee`, space, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []d.Share{}
	for rows.Next() {
		var v d.Share
		if err = rows.Scan(&v.Grantee, &v.Permission); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (t *spaceTx) ResourceShares(ctx context.Context, kind, id string) ([]d.Share, error) {
	return resourceShares(ctx, t.tx, t.id, kind, id)
}
func (t *spaceTx) SaveResourceShare(ctx context.Context, kind, id string, share d.Share, createdBy string) error {
	if kind != d.ProjectResource || id == "" || share.Grantee == "" || (share.Permission != d.Read && share.Permission != d.Edit) || createdBy == "" {
		return fault.ErrInvalid
	}
	_, err := t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.space_shares(space_id,resource_kind,resource_id,grantee,permission,created_by) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(space_id,resource_kind,resource_id,grantee) DO UPDATE SET permission=EXCLUDED.permission,created_by=EXCLUDED.created_by`, t.id, kind, id, share.Grantee, share.Permission, createdBy)
	return err
}
func (t *spaceTx) DeleteResourceShare(ctx context.Context, kind, id, grantee string) error {
	_, err := t.tx.Exec(ctx, `DELETE FROM drama_catalog_v1.space_shares WHERE space_id=$1 AND resource_kind=$2 AND resource_id=$3 AND grantee=$4`, t.id, kind, id, grantee)
	return err
}

func (s *Store) ProjectEditorName(ctx context.Context, user string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT username FROM drama_catalog_v1.users WHERE id=$1`, user).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return name, err
}
