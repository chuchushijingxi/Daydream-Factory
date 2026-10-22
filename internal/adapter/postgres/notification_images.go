package postgres

import (
	"context"
	app "drama-agent/internal/application/notifications"
	"drama-agent/internal/domain/fault"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Store) WithdrawNotification(ctx context.Context, author, id string, revision uint64) (app.Message, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return app.Message{}, e
	}
	defer tx.Rollback(ctx)
	v, e := scanNotification(tx.QueryRow(ctx, `SELECT `+notificationColumns+` FROM drama_catalog_v1.notification_messages WHERE kind='operator' AND id=$1 AND author_id=$2 AND $2<>'' FOR UPDATE`, id, author))
	if e != nil {
		return app.Message{}, e
	}
	needed, e := v.WithdrawalNeeded(revision)
	if e != nil {
		return app.Message{}, e
	}
	if !needed {
		return v, nil
	}
	v, e = scanNotification(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.notification_messages SET status='withdrawn',withdrawn_at=now(),revision=revision+1 WHERE kind='operator' AND id=$1 AND author_id=$2 AND revision=$3 AND status='published' RETURNING `+notificationColumns, id, author, revision))
	if e != nil {
		return app.Message{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return app.Message{}, noticeUnknown()
	}
	s.invalidateAfterCommit(ctx, "notifications")
	return v, nil
}

const notificationImageColumns = `id,content_type,width,height,octet_length(data),data,sha256`

func scanNotificationImage(row pgx.Row, messageID string) (app.Image, error) {
	var v app.Image
	e := row.Scan(&v.ID, &v.ContentType, &v.Width, &v.Height, &v.SizeBytes, &v.Data, &v.Digest)
	if errors.Is(e, pgx.ErrNoRows) {
		e = fault.ErrNotFound
	}
	if e == nil {
		v.URL = "/api/notifications/" + messageID + "/images/" + v.ID
		v.PreviewURL = "/api/operations/notifications/" + messageID + "/images/" + v.ID
	}
	return v, e
}
func (s *Store) SaveNotificationImage(ctx context.Context, author, messageID string, v app.Image) (app.Image, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return app.Image{}, e
	}
	defer tx.Rollback(ctx)
	m, e := scanNotification(tx.QueryRow(ctx, `SELECT `+notificationColumns+` FROM drama_catalog_v1.notification_messages WHERE kind='operator' AND id=$1 AND author_id=$2 AND $2<>'' FOR UPDATE`, messageID, author))
	if e != nil {
		return app.Image{}, e
	}
	if m.Status != "draft" {
		return app.Image{}, noticeConflict()
	}
	v, e = scanNotificationImage(tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.notification_images(id,message_id,content_type,width,height,data,sha256) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(message_id,sha256) DO UPDATE SET sha256=EXCLUDED.sha256 RETURNING `+notificationImageColumns, v.ID, messageID, v.ContentType, v.Width, v.Height, v.Data, v.Digest), messageID)
	if e != nil {
		return app.Image{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return app.Image{}, noticeUnknown()
	}
	return v, nil
}
func (s *Store) NotificationImage(ctx context.Context, user, messageID, imageID string) (app.Image, error) {
	return scanNotificationImage(s.pool.QueryRow(ctx, `SELECT `+notificationImageColumns+` FROM drama_catalog_v1.notification_images WHERE id=$2 AND message_id=$3 AND EXISTS(SELECT 1`+notificationRecipientJoin+`WHERE m.id=$3 AND `+notificationVisible+`)`, user, imageID, messageID), messageID)
}

func (s *Store) OperatorNotificationImage(ctx context.Context, author, messageID, imageID string) (app.Image, error) {
	return scanNotificationImage(s.pool.QueryRow(ctx, `SELECT `+notificationImageColumns+` FROM drama_catalog_v1.notification_images WHERE id=$1 AND message_id=$2 AND $3<>'' AND EXISTS(SELECT 1 FROM drama_catalog_v1.notification_messages m WHERE m.kind='operator' AND m.id=$2 AND m.author_id=$3)`, imageID, messageID, author), messageID)
}
