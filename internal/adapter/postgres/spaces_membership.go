package postgres

import (
	"context"
	app "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/fault"
	"errors"
	"github.com/jackc/pgx/v5"
)

const inviteColumns = `i.id,i.space_id,t.name,i.kind,coalesce(i.token_digest,''),coalesce(i.invitee_user_id,''),i.created_by,i.created_at,i.expires_at,i.revoked_at,i.status`
const inviteFrom = ` FROM drama_catalog_v1.team_invites i JOIN drama_catalog_v1.teams t ON t.space_id=i.space_id `

func scanSpaceInvite(row rowScanner) (app.Invite, error) {
	var v app.Invite
	err := row.Scan(&v.ID, &v.SpaceID, &v.TeamName, &v.Kind, &v.TokenDigest, &v.InviteeUserID, &v.CreatedBy, &v.CreatedAt, &v.ExpiresAt, &v.RevokedAt, &v.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return v, err
}
func (t *spaceTx) Invite(ctx context.Context, id string) (app.Invite, error) {
	return scanSpaceInvite(t.tx.QueryRow(ctx, `SELECT `+inviteColumns+inviteFrom+`WHERE i.space_id=$1 AND i.id=$2`, t.id, id))
}
func (s *Store) SpaceInvite(ctx context.Context, id string) (app.Invite, error) {
	return scanSpaceInvite(s.pool.QueryRow(ctx, `SELECT `+inviteColumns+inviteFrom+`WHERE i.id=$1`, id))
}
func (s *Store) InviteByDigest(ctx context.Context, digest string) (app.Invite, error) {
	return scanSpaceInvite(s.pool.QueryRow(ctx, `SELECT `+inviteColumns+inviteFrom+`WHERE i.token_digest=$1`, digest))
}
func (t *spaceTx) SaveInvite(ctx context.Context, v app.Invite) error {
	if v.SpaceID != t.id {
		return fault.ErrInvalid
	}
	old, err := t.Invite(ctx, v.ID)
	if err == nil {
		if old.Kind != v.Kind || old.InviteeUserID != v.InviteeUserID || old.TokenDigest != v.TokenDigest || old.CreatedBy != v.CreatedBy {
			return fault.ErrConflict
		}
		result, err := t.tx.Exec(ctx, `UPDATE drama_catalog_v1.team_invites SET status=$3,revoked_at=$4 WHERE space_id=$1 AND id=$2`, t.id, v.ID, v.Status, v.RevokedAt)
		if err == nil && result.RowsAffected() != 1 {
			return fault.ErrNotFound
		}
		return err
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return err
	}
	if v.Kind == "email" {
		if _, err = t.tx.Exec(ctx, `UPDATE drama_catalog_v1.team_invites SET status='expired' WHERE space_id=$1 AND invitee_user_id=$2 AND kind='email' AND status='pending' AND expires_at<=now()`, t.id, v.InviteeUserID); err != nil {
			return err
		}
	}
	_, err = t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.team_invites(id,space_id,kind,token_digest,invitee_user_id,created_by,created_at,expires_at,revoked_at,status) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10)`, v.ID, t.id, v.Kind, v.TokenDigest, v.InviteeUserID, v.CreatedBy, v.CreatedAt, v.ExpiresAt, v.RevokedAt, v.Status)
	return spaceWriteError(err)
}
func (s *Store) listSpaceInvites(ctx context.Context, where string, args ...any) ([]app.Invite, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+inviteColumns+inviteFrom+where+` ORDER BY i.created_at DESC,i.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.Invite{}
	for rows.Next() {
		v, e := scanSpaceInvite(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SpaceInvites(ctx context.Context, id string) ([]app.Invite, error) {
	return s.listSpaceInvites(ctx, `WHERE i.space_id=$1`, id)
}
func (s *Store) PendingInvites(ctx context.Context, user string) ([]app.Invite, error) {
	return s.listSpaceInvites(ctx, `WHERE i.invitee_user_id=$1 AND i.kind='email' AND i.status='pending' AND i.revoked_at IS NULL AND i.expires_at>now() AND NOT t.disabled AND t.expires_at>now()`, user)
}

const joinRequestColumns = `r.id,r.space_id,r.user_id,u.username,r.message,r.status,coalesce(r.decided_by,''),r.created_at,r.decided_at`
const joinRequestFrom = ` FROM drama_catalog_v1.team_join_requests r JOIN drama_catalog_v1.users u ON u.id=r.user_id `

func scanJoinRequest(row rowScanner) (app.JoinRequest, error) {
	var v app.JoinRequest
	err := row.Scan(&v.ID, &v.SpaceID, &v.UserID, &v.UserName, &v.Message, &v.Status, &v.DecidedBy, &v.CreatedAt, &v.DecidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return v, err
}
func (t *spaceTx) JoinRequest(ctx context.Context, id string) (app.JoinRequest, error) {
	return scanJoinRequest(t.tx.QueryRow(ctx, `SELECT `+joinRequestColumns+joinRequestFrom+`WHERE r.space_id=$1 AND r.id=$2`, t.id, id))
}

// SpaceJoinRequests 只列待处理申请；已批准或拒绝的记录留在审计与动态中，不再出现在待办列表。
func (s *Store) SpaceJoinRequests(ctx context.Context, id string) ([]app.JoinRequest, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+joinRequestColumns+joinRequestFrom+`WHERE r.space_id=$1 AND r.status='pending' ORDER BY r.created_at DESC,r.id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.JoinRequest{}
	for rows.Next() {
		v, e := scanJoinRequest(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (t *spaceTx) SaveJoinRequest(ctx context.Context, v app.JoinRequest) error {
	if v.SpaceID != t.id {
		return fault.ErrInvalid
	}
	old, err := t.JoinRequest(ctx, v.ID)
	if err == nil {
		if old.UserID != v.UserID || old.Message != v.Message {
			return fault.ErrConflict
		}
		result, e := t.tx.Exec(ctx, `UPDATE drama_catalog_v1.team_join_requests SET status=$3,decided_by=NULLIF($4,''),decided_at=$5 WHERE space_id=$1 AND id=$2`, t.id, v.ID, v.Status, v.DecidedBy, v.DecidedAt)
		if e == nil && result.RowsAffected() != 1 {
			return fault.ErrNotFound
		}
		return e
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return err
	}
	_, err = t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.team_join_requests(id,space_id,user_id,message,status,decided_by,created_at,decided_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8)`, v.ID, t.id, v.UserID, v.Message, v.Status, v.DecidedBy, v.CreatedAt, v.DecidedAt)
	return spaceWriteError(err)
}
func scanOwnershipTransfer(row rowScanner) (app.OwnershipTransfer, error) {
	var v app.OwnershipTransfer
	err := row.Scan(&v.ID, &v.SpaceID, &v.FromOwnerID, &v.ToUserID, &v.Status, &v.CreatedAt, &v.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	if v.Status != "accepted" {
		v.AcceptedAt = nil
	}
	return v, err
}
func (t *spaceTx) OwnershipTransfer(ctx context.Context, id string) (app.OwnershipTransfer, error) {
	return scanOwnershipTransfer(t.tx.QueryRow(ctx, `SELECT id,space_id,from_user_id,to_user_id,status,created_at,decided_at FROM drama_catalog_v1.team_ownership_transfers WHERE space_id=$1 AND id=$2`, t.id, id))
}

// 调用方已持有团队行锁；查询和后续取消、审计、新建同属同一提交边界。
func (t *spaceTx) PendingOwnershipTransfers(ctx context.Context) ([]app.OwnershipTransfer, error) {
	rows, err := t.tx.Query(ctx, `SELECT id,space_id,from_user_id,to_user_id,status,created_at,decided_at FROM drama_catalog_v1.team_ownership_transfers WHERE space_id=$1 AND status='pending' ORDER BY created_at,id`, t.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.OwnershipTransfer{}
	for rows.Next() {
		v, e := scanOwnershipTransfer(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (t *spaceTx) SaveOwnershipTransfer(ctx context.Context, v app.OwnershipTransfer) error {
	if v.SpaceID != t.id {
		return fault.ErrInvalid
	}
	old, err := t.OwnershipTransfer(ctx, v.ID)
	if err == nil {
		if old.FromOwnerID != v.FromOwnerID || old.ToUserID != v.ToUserID {
			return fault.ErrConflict
		}
		result, e := t.tx.Exec(ctx, `UPDATE drama_catalog_v1.team_ownership_transfers SET status=$3,decided_at=CASE WHEN $3='cancelled' THEN clock_timestamp() ELSE $4 END WHERE space_id=$1 AND id=$2`, t.id, v.ID, v.Status, v.AcceptedAt)
		if e == nil && result.RowsAffected() != 1 {
			return fault.ErrNotFound
		}
		return e
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return err
	}
	_, err = t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.team_ownership_transfers(id,space_id,from_user_id,to_user_id,status,created_at,decided_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, v.ID, t.id, v.FromOwnerID, v.ToUserID, v.Status, v.CreatedAt, v.AcceptedAt)
	return spaceWriteError(err)
}
