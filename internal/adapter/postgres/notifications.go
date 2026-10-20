package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	app "drama-agent/internal/application/notifications"
	"drama-agent/internal/domain/fault"
	"github.com/jackc/pgx/v5"
)

const notificationColumns = `id,title,body,audience,coalesce(recipient_user_id,''),status,revision,recipient_count,created_at,published_at,withdrawn_at,author_id,(SELECT username FROM drama_catalog_v1.users u WHERE u.id=author_id),coalesce((SELECT username FROM drama_catalog_v1.users u WHERE u.id=recipient_user_id),''),coalesce((SELECT email FROM drama_catalog_v1.users u WHERE u.id=recipient_user_id),'')`

// 全员广播对之后启用的账号同样可见；回执保留每用户已读事实与定向投递，不冻结广播受众。
// 全部接收人读取面共用这一谓词。
const notificationVisible = `m.status='published' AND $1<>'' AND EXISTS(SELECT 1 FROM drama_catalog_v1.users u WHERE u.id=$1 AND u.enabled) AND (m.audience='all' OR r.user_id IS NOT NULL)`
const notificationRecipientJoin = ` FROM drama_catalog_v1.notification_messages m LEFT JOIN drama_catalog_v1.notification_receipts r ON r.message_id=m.id AND r.user_id=$1 `

func scanNotification(row pgx.Row) (app.Message, error) {
	var v app.Message
	e := row.Scan(&v.ID, &v.Title, &v.Body, &v.Audience, &v.RecipientUserID, &v.Status, &v.Revision, &v.RecipientCount, &v.CreatedAt, &v.PublishedAt, &v.WithdrawnAt, &v.AuthorID, &v.AuthorName, &v.RecipientName, &v.RecipientEmail)
	if errors.Is(e, pgx.ErrNoRows) {
		e = fault.ErrNotFound
	}
	return v, e
}
func noticeConflict() error {
	return fault.New("NOTIFICATION_CONFLICT", "通知已被修改或发布，请读取最新内容")
}
func noticeUnknown() error {
	return fault.New("NOTIFICATION_COMMIT_UNKNOWN", "通知写入结果未确认，请读取原通知核对，勿重复发布")
}
func (s *Store) NotificationUnread(ctx context.Context, user string) (int64, error) {
	return readProjection(ctx, s, "notifications:"+user, func(c context.Context) (int64, error) { return s.loadNotificationUnread(c, user) })
}
func (s *Store) loadNotificationUnread(ctx context.Context, user string) (int64, error) {
	var n int64
	e := s.pool.QueryRow(ctx, `SELECT count(*)`+notificationRecipientJoin+`WHERE `+notificationVisible+` AND r.read_at IS NULL`, user).Scan(&n)
	return n, e
}
func (s *Store) NotificationList(ctx context.Context, user string, q app.Query) (app.Page[app.StoredItem], error) {
	result := app.Page[app.StoredItem]{Items: []app.StoredItem{}, Page: q.Page, PageSize: q.PageSize}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return result, e
	}
	defer tx.Rollback(ctx)
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE $2='all' OR ($2='read' AND r.read_at IS NOT NULL) OR ($2='unread' AND r.read_at IS NULL)),count(*) FILTER(WHERE r.read_at IS NULL)`+notificationRecipientJoin+`WHERE `+notificationVisible, user, q.Status).Scan(&result.Total, &result.Unread)
	if e != nil {
		return result, e
	}
	rows, e := tx.Query(ctx, `SELECT m.id,m.title,m.published_at,r.read_at,m.body`+notificationRecipientJoin+`WHERE `+notificationVisible+` AND ($2='all' OR ($2='read' AND r.read_at IS NOT NULL) OR ($2='unread' AND r.read_at IS NULL)) ORDER BY m.published_at DESC,m.id DESC LIMIT $3 OFFSET $4`, user, q.Status, q.PageSize, (q.Page-1)*q.PageSize)
	if e != nil {
		return result, e
	}
	defer rows.Close()
	for rows.Next() {
		var v app.StoredItem
		if e = rows.Scan(&v.ID, &v.Title, &v.PublishedAt, &v.ReadAt, &v.Body); e != nil {
			return result, e
		}
		result.Items = append(result.Items, v)
	}
	if e = rows.Err(); e != nil {
		return result, e
	}
	return result, tx.Commit(ctx)
}
func (s *Store) NotificationDetail(ctx context.Context, user, id string) (app.Detail, error) {
	var v app.Detail
	e := s.pool.QueryRow(ctx, `SELECT m.id,m.title,m.published_at,r.read_at,m.body`+notificationRecipientJoin+`WHERE `+notificationVisible+` AND m.id=$2`, user, id).Scan(&v.ID, &v.Title, &v.PublishedAt, &v.ReadAt, &v.Body)
	if errors.Is(e, pgx.ErrNoRows) {
		e = fault.ErrNotFound
	}
	return v, e
}
func (s *Store) NotificationRead(ctx context.Context, user, id string) (app.ReadReceipt, error) {
	var v app.ReadReceipt
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(ctx)
	var allowed string
	e = tx.QueryRow(ctx, `SELECT m.id`+notificationRecipientJoin+`WHERE `+notificationVisible+` AND m.id=$2 FOR SHARE OF m`, user, id).Scan(&allowed)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, fault.ErrNotFound
	}
	if e != nil {
		return v, e
	}
	e = tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.notification_receipts(message_id,user_id,read_at) VALUES($2,$1,now()) ON CONFLICT(message_id,user_id) DO UPDATE SET read_at=coalesce(notification_receipts.read_at,EXCLUDED.read_at) RETURNING message_id,read_at`, user, id).Scan(&v.ID, &v.ReadAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = fault.ErrNotFound
	}
	if e != nil {
		return v, e
	}
	if e = tx.Commit(ctx); e != nil {
		return v, noticeUnknown()
	}
	s.invalidateAfterCommit(ctx, "notifications:"+user)
	return v, nil
}
func (s *Store) NotificationsReadAll(ctx context.Context, user string) (app.ReadAllReceipt, error) {
	var v app.ReadAllReceipt
	// 以语句快照为截断点；并发到达的更晚投递保持未读。
	e := s.pool.QueryRow(ctx, `WITH unread AS MATERIALIZED (SELECT m.id`+notificationRecipientJoin+`WHERE `+notificationVisible+` AND r.read_at IS NULL ORDER BY m.id FOR SHARE OF m), marked AS (INSERT INTO drama_catalog_v1.notification_receipts(message_id,user_id,read_at) SELECT id,$1,now() FROM unread WHERE true ON CONFLICT(message_id,user_id) DO UPDATE SET read_at=EXCLUDED.read_at WHERE notification_receipts.read_at IS NULL RETURNING message_id) SELECT (SELECT count(*) FROM marked),0::bigint`, user).Scan(&v.Updated, &v.Unread)
	if e == nil {
		s.invalidateAfterCommit(ctx, "notifications:"+user)
	}
	return v, e
}
func (s *Store) OperatorNotifications(ctx context.Context, author string, q app.Query) (app.Page[app.Message], error) {
	result := app.Page[app.Message]{Items: []app.Message{}, Page: q.Page, PageSize: q.PageSize}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return result, e
	}
	defer tx.Rollback(ctx)
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.notification_messages WHERE author_id=$1 AND $1<>''`, author).Scan(&result.Total); e != nil {
		return result, e
	}
	rows, e := tx.Query(ctx, `SELECT `+notificationColumns+` FROM drama_catalog_v1.notification_messages WHERE author_id=$1 AND $1<>'' ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, author, q.PageSize, (q.Page-1)*q.PageSize)
	if e != nil {
		return result, e
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanNotification(rows)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, v)
	}
	if e = rows.Err(); e != nil {
		return result, e
	}
	return result, tx.Commit(ctx)
}
func (s *Store) OperatorNotification(ctx context.Context, author, id string) (app.Message, error) {
	return scanNotification(s.pool.QueryRow(ctx, `SELECT `+notificationColumns+` FROM drama_catalog_v1.notification_messages WHERE author_id=$1 AND id=$2 AND $1<>''`, author, id))
}
func (s *Store) CreateNotification(ctx context.Context, author, id string, in app.Input) (app.Message, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return app.Message{}, e
	}
	defer tx.Rollback(ctx)
	// 只对同一作者的命令串行，保证重试时内容比对稳定。
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('notification:'||$1||':'||$2,0))`, author, in.CommandID); e != nil {
		return app.Message{}, e
	}
	old, e := scanNotification(tx.QueryRow(ctx, `SELECT `+notificationColumns+` FROM drama_catalog_v1.notification_messages WHERE author_id=$1 AND command_id=$2`, author, in.CommandID))
	if e == nil {
		var digest string
		if e = tx.QueryRow(ctx, `SELECT creation_digest FROM drama_catalog_v1.notification_messages WHERE author_id=$1 AND command_id=$2`, author, in.CommandID).Scan(&digest); e != nil {
			return app.Message{}, e
		}
		if digest != notificationDigest(in) {
			return app.Message{}, fault.New("NOTIFICATION_COMMAND_CONFLICT", "相同创建编号的通知内容不一致")
		}
		return old, nil
	}
	if !errors.Is(e, fault.ErrNotFound) {
		return app.Message{}, e
	}
	if in.Audience == "user" {
		var valid bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.users WHERE id=$1 AND enabled)`, in.RecipientUserID).Scan(&valid); e != nil {
			return app.Message{}, e
		}
		if !valid {
			return app.Message{}, fault.New("NOTIFICATION_INVALID", "接收人不是有效启用账号")
		}
	}
	v, e := scanNotification(tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.notification_messages(id,author_id,command_id,title,body,audience,recipient_user_id,creation_digest) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8) RETURNING `+notificationColumns, id, author, in.CommandID, in.Title, in.Body, in.Audience, in.RecipientUserID, notificationDigest(in)))
	if e != nil {
		return app.Message{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return app.Message{}, noticeUnknown()
	}
	return v, nil
}
func (s *Store) UpdateNotification(ctx context.Context, author, id string, in app.Input) (app.Message, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return app.Message{}, e
	}
	defer tx.Rollback(ctx)
	old, e := scanNotification(tx.QueryRow(ctx, `SELECT `+notificationColumns+` FROM drama_catalog_v1.notification_messages WHERE id=$1 AND author_id=$2 AND $2<>'' FOR UPDATE`, id, author))
	if e != nil {
		return app.Message{}, e
	}
	if e = old.RequireDraft(in.Revision); e != nil {
		return app.Message{}, e
	}
	if in.Audience == "user" {
		var valid bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.users WHERE id=$1 AND enabled)`, in.RecipientUserID).Scan(&valid); e != nil {
			return app.Message{}, e
		}
		if !valid {
			return app.Message{}, fault.New("NOTIFICATION_INVALID", "接收人不是有效启用账号")
		}
	}
	if len(in.ImageIDs) > 0 {
		var n int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.notification_images WHERE message_id=$1 AND id=ANY($2)`, id, in.ImageIDs).Scan(&n); e != nil {
			return app.Message{}, e
		}
		if n != len(in.ImageIDs) {
			return app.Message{}, fault.New("NOTIFICATION_IMAGE_INVALID", "图片不属于本通知")
		}
	}
	v, e := scanNotification(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.notification_messages SET title=$4,body=$5,audience=$6,recipient_user_id=NULLIF($7,''),revision=revision+1 WHERE id=$1 AND author_id=$2 AND revision=$3 AND status='draft' RETURNING `+notificationColumns, id, author, in.Revision, in.Title, in.Body, in.Audience, in.RecipientUserID))
	if e != nil {
		return app.Message{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return app.Message{}, noticeUnknown()
	}
	return v, nil
}

func (s *Store) PublishNotification(ctx context.Context, author, id string, revision uint64) (app.Message, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return app.Message{}, e
	}
	defer tx.Rollback(ctx)
	v, e := scanNotification(tx.QueryRow(ctx, `SELECT `+notificationColumns+` FROM drama_catalog_v1.notification_messages WHERE id=$1 AND author_id=$2 AND $2<>'' FOR UPDATE`, id, author))
	if e != nil {
		return app.Message{}, e
	}
	needed, e := v.PublicationNeeded(revision)
	if e != nil {
		return app.Message{}, e
	}
	if !needed {
		return v, nil
	}
	tag, e := tx.Exec(ctx, `INSERT INTO drama_catalog_v1.notification_receipts(message_id,user_id) SELECT $1,id FROM drama_catalog_v1.users WHERE enabled AND ($2='all' OR id=$3)`, id, v.Audience, v.RecipientUserID)
	if e != nil {
		return app.Message{}, e
	}
	if v.Audience == "user" && tag.RowsAffected() != 1 {
		return app.Message{}, fault.New("NOTIFICATION_INVALID", "接收人不是有效启用账号")
	}
	v, e = scanNotification(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.notification_messages SET status='published',revision=revision+1,published_at=now(),recipient_count=$4 WHERE id=$1 AND author_id=$2 AND revision=$3 AND status='draft' RETURNING `+notificationColumns, id, author, revision, tag.RowsAffected()))
	if e != nil {
		return app.Message{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return app.Message{}, noticeUnknown()
	}
	s.invalidateAfterCommit(ctx, "notifications")
	return v, nil
}

func notificationDigest(in app.Input) string {
	raw, _ := json.Marshal(struct{ Title, Body, Audience, Recipient string }{in.Title, in.Body, in.Audience, in.RecipientUserID})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *Store) NotificationCommand(ctx context.Context, author, command string) (app.Message, error) {
	return scanNotification(s.pool.QueryRow(ctx, `SELECT `+notificationColumns+` FROM drama_catalog_v1.notification_messages WHERE author_id=$1 AND command_id=$2 AND $1<>''`, author, command))
}
