package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	app "drama-agent/internal/application/notifications"
	"drama-agent/internal/domain/fault"
	domain "drama-agent/internal/domain/notifications"
	"github.com/jackc/pgx/v5"
)

// systemNotifyTx 与空间事实、审计共用调用方事务。OperationID必须来自原业务动作，
// 同一动作对同一接收人只能投递一次；不同接收人不共用消息，防止跨用户读取。
// runtime_events 由消息INSERT触发器同事务追加，回滚不会留下未读提示。
func systemNotifyTx(ctx context.Context, tx pgx.Tx, in domain.SystemInput) (domain.Message, error) {
	input, err := in.Normalize()
	if err != nil {
		return domain.Message{}, err
	}
	raw, _ := json.Marshal([]string{input.OperationID, input.RecipientUserID})
	identity := sha256.Sum256(raw)
	id := "notice_system_" + hex.EncodeToString(identity[:])
	raw, err = json.Marshal(input)
	if err != nil {
		return domain.Message{}, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, id); err != nil {
		return domain.Message{}, err
	}
	old, err := scanNotification(tx.QueryRow(ctx, `SELECT `+notificationColumns+` FROM drama_catalog_v1.notification_messages WHERE id=$1 AND kind='system'`, id))
	if err == nil {
		var saved string
		if err = tx.QueryRow(ctx, `SELECT creation_digest FROM drama_catalog_v1.notification_messages WHERE id=$1`, id).Scan(&saved); err != nil {
			return domain.Message{}, err
		}
		if saved != digest {
			return domain.Message{}, fault.New("NOTIFICATION_COMMAND_CONFLICT", "相同系统通知编号的内容不一致")
		}
		return old, nil
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return domain.Message{}, err
	}
	var registered bool
	// 系统通知记录已经发生的事实；禁用账号不能让移出或运营换所有者的事务失败。
	// 读取仍走原enabled校验，恢复账号后才可查看自己的事实通知。
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.users WHERE id=$1)`, input.RecipientUserID).Scan(&registered); err != nil {
		return domain.Message{}, err
	}
	if !registered {
		return domain.Message{}, fault.New("NOTIFICATION_INVALID", "系统通知接收人不是已注册账号")
	}
	link, err := json.Marshal(input.Link)
	if err != nil {
		return domain.Message{}, err
	}
	message, err := scanNotification(tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.notification_messages(id,author_id,command_id,creation_digest,title,body,audience,recipient_user_id,status,revision,recipient_count,published_at,kind,link) VALUES($1,NULL,$2,$3,$4,$5,'user',$6,'published',1,1,now(),'system',$7) RETURNING `+notificationColumns, id, input.OperationID, digest, input.Title, input.Body, input.RecipientUserID, link))
	if err != nil {
		return domain.Message{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.notification_receipts(message_id,user_id) VALUES($1,$2)`, id, input.RecipientUserID); err != nil {
		return domain.Message{}, err
	}
	return message, nil
}

// SystemNotification 是独立业务用例的事务入口；需要原子提交其他事实时直接复用systemNotifyTx。
func (s *Store) SystemNotification(ctx context.Context, in app.SystemInput) (app.Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.Message{}, err
	}
	defer tx.Rollback(ctx)
	message, err := systemNotifyTx(ctx, tx, in)
	if err != nil {
		return app.Message{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return message, noticeUnknown()
	}
	s.invalidateAfterCommit(ctx, "notifications:"+in.RecipientUserID)
	return message, nil
}
