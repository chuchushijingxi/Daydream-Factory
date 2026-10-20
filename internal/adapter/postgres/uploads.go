package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/upload"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateUploadedFile(ctx context.Context, file d.File) error {
	if s.owner != nil && (*s.owner == "" || file.OwnerID != *s.owner) {
		return fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	if !d.ValidID(file.ID) || !file.Original.Valid(file.ID) || file.Original.ObjectKey != d.ObjectKey(file.ID, "original") || file.Filename == "" || file.CreatedAt.IsZero() {
		return fault.ErrInvalid
	}
	if file.Thumbnail != nil && (!file.Thumbnail.Blob.Valid(file.ID) || file.Thumbnail.Blob.ObjectKey != d.ObjectKey(file.ID, "thumbnail.webp") || file.Thumbnail.Blob.ContentType != "image/webp" || file.Thumbnail.Width < 1 || file.Thumbnail.Height < 1) {
		return fault.ErrInvalid
	}
	raw, err := json.Marshal(struct {
		d.File
		OwnerID string `json:"ownerId,omitempty"`
	}{File: file})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.uploaded_files(id,payload,owner_id) VALUES($1,$2,NULLIF($3,''))`, file.ID, raw, file.OwnerID)
	return err
}

func (s *Store) UploadedFile(ctx context.Context, id string) (d.File, error) {
	var file d.File
	if s.owner != nil && *s.owner == "" {
		return file, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	var raw []byte
	var owner string
	err := s.pool.QueryRow(ctx, `SELECT payload,coalesce(owner_id,'') FROM drama_catalog_v1.uploaded_files WHERE id=$1 AND ($2::text IS NULL OR (owner_id=$2 AND $2<>''))`, id, s.owner).Scan(&raw, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return file, fault.ErrNotFound
	}
	if err != nil {
		return file, err
	}
	err = json.Unmarshal(raw, &file)
	file.OwnerID = owner
	return file, err
}
