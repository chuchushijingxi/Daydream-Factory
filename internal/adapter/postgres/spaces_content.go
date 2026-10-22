package postgres

import (
	"context"
	app "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/spaces"
	"encoding/json"
	"errors"
)

func (s *Store) TransferSpaceContent(ctx context.Context, id, actor string, in app.TransferInput) ([]app.TransferReceipt, error) {
	refs := in.Resources
	if in.All {
		rows, err := s.pool.Query(ctx, `SELECT 'project',id FROM drama_catalog_v1.projects WHERE space_id=$1 AND owner_id=$2 UNION ALL SELECT 'upload',id FROM drama_catalog_v1.uploaded_files WHERE space_id=$1 AND owner_id=$2 UNION ALL SELECT 'media',id FROM drama_catalog_v1.tasks WHERE space_id=$1 AND owner_id=$2 ORDER BY 1,2`, id, in.FromUserID)
		if err != nil {
			return nil, err
		}
		refs = []app.ResourceRef{}
		for rows.Next() {
			var v app.ResourceRef
			if err = rows.Scan(&v.Kind, &v.ID); err != nil {
				rows.Close()
				return nil, err
			}
			refs = append(refs, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	out := make([]app.TransferReceipt, 0, len(refs))
	for _, ref := range refs {
		receipt := app.TransferReceipt{ResourceRef: ref}
		err := s.SpaceTx(ctx, id, func(raw app.Tx) error {
			tx := raw.(*spaceTx)
			team, err := tx.Team(ctx)
			if err != nil {
				return err
			}
			if team.Disabled {
				return fault.ErrNotFound
			}
			member, err := tx.Member(ctx, actor)
			if err != nil {
				return err
			}
			if member.Role != d.Owner && member.Role != d.Admin {
				return fault.New("ROLE_FORBIDDEN", "没有团队管理权限")
			}
			if _, err = tx.Member(ctx, in.FromUserID); err == nil {
				return fault.New("TEAM_TRANSFER_SOURCE_ACTIVE", "仅可转交已离开成员的内容")
			} else if !errors.Is(err, fault.ErrNotFound) {
				return err
			}
			if _, err = tx.Member(ctx, in.ToUserID); err != nil {
				return err
			}
			user, err := tx.FindUser(ctx, in.ToUserID)
			if err != nil {
				return err
			}
			if !user.Enabled {
				return fault.New("ACCOUNT_DISABLED", "接收人账号已禁用")
			}
			var table string
			switch ref.Kind {
			case "project":
				table = "projects"
			case "upload":
				table = "uploaded_files"
			case "task", "media":
				table = "tasks"
			case "canvas", "agent_session":
				return fault.ErrNotFound
			default:
				return fault.ErrInvalid
			}
			// 表名只来自上面的封闭映射；转交只改负责人，创建人和原文保持原事实。
			query := `UPDATE drama_catalog_v1.` + table + ` SET owner_id=$4 WHERE id=$1 AND space_id=$2 AND owner_id=$3`
			if table == "projects" {
				query = `UPDATE drama_catalog_v1.projects SET owner_id=$4,revision=revision+1,payload=jsonb_set(payload,'{revision}',to_jsonb(revision+1)),updated_at=now() WHERE id=$1 AND space_id=$2 AND owner_id=$3`
			}
			if table == "tasks" {
				query = `UPDATE drama_catalog_v1.tasks SET owner_id=$4,revision=revision+1,updated_at=now() WHERE id=$1 AND space_id=$2 AND owner_id=$3`
			}
			tag, err := tx.tx.Exec(ctx, query, ref.ID, id, in.FromUserID, in.ToUserID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fault.ErrNotFound
			}
			detail, _ := json.Marshal(map[string]string{"kind": ref.Kind, "fromUserId": in.FromUserID, "toUserId": in.ToUserID})
			return tx.AppendAudit(ctx, app.AuditEvent{ActorID: actor, ActorKind: "user", Action: "content_transferred", Target: ref.ID, Detail: detail})
		})
		if err == nil {
			receipt.Transferred = true
		} else {
			var f *fault.Error
			switch {
			case errors.As(err, &f):
				receipt.ErrorCode = f.Code
			case errors.Is(err, fault.ErrNotFound):
				receipt.ErrorCode = "NOT_FOUND"
			case errors.Is(err, fault.ErrConflict):
				receipt.ErrorCode = "CONFLICT"
			case errors.Is(err, fault.ErrInvalid):
				receipt.ErrorCode = "INVALID_INPUT"
			default:
				return out, err
			}
		}
		out = append(out, receipt)
	}
	return out, nil
}
