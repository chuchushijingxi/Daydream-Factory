package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	app "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/fault"
	n "drama-agent/internal/domain/notifications"
	d "drama-agent/internal/domain/spaces"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type spaceTx struct {
	store *Store
	tx    pgx.Tx
	id    string
}

var _ app.Store = (*Store)(nil)
var _ app.Tx = (*spaceTx)(nil)

// 每个成员变更在同一团队行锁内重新读取角色与人数，防止并发批准超过上限。
func (s *Store) SpaceTx(ctx context.Context, id string, apply func(app.Tx) error) error {
	if id == "" || apply == nil {
		return fault.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var locked string
	err = tx.QueryRow(ctx, `SELECT space_id FROM drama_catalog_v1.teams WHERE space_id=$1 FOR UPDATE`, id).Scan(&locked)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err = apply(&spaceTx{store: s, tx: tx, id: id}); err != nil {
		return spaceWriteError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fault.New("SPACE_COMMIT_UNKNOWN", "团队操作提交结果未确认，请刷新核对")
	}
	return nil
}
func spaceWriteError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		switch pg.ConstraintName {
		case "team_pending_email_invite_uidx":
			return fault.New("TEAM_INVITE_EXISTS", "该用户已有待处理邀请")
		case "team_pending_request_uidx":
			return fault.New("JOIN_REQUEST_EXISTS", "已有待处理的加入申请")
		default:
			return fault.New("SPACE_CONFLICT", "团队信息已变化，请刷新重试")
		}
	}
	return err
}

const teamColumns = `space_id,name,owner_id,member_limit,expires_at,disabled,grant_amount,policy,opened_at,revision`

func scanTeam(row rowScanner) (d.Team, error) {
	var team d.Team
	err := row.Scan(&team.ID, &team.Name, &team.OwnerID, &team.MemberLimit, &team.ExpiresAt, &team.Disabled, &team.GrantAmount, &team.Policy, &team.OpenedAt, &team.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return team, err
}
func (t *spaceTx) Team(ctx context.Context) (d.Team, error) {
	return scanTeam(t.tx.QueryRow(ctx, `SELECT `+teamColumns+` FROM drama_catalog_v1.teams WHERE space_id=$1`, t.id))
}
func (t *spaceTx) CreateTeam(ctx context.Context, team d.Team, discounts []app.ModelDiscount) error {
	if team.ID != t.id || team.OwnerID == "" || team.Revision != 1 {
		return fault.ErrInvalid
	}
	if _, err := t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.spaces(id,kind,created_at) VALUES($1,'team',$2)`, t.id, team.OpenedAt); err != nil {
		return err
	}
	_, err := t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.teams(space_id,name,owner_id,member_limit,expires_at,disabled,grant_amount,policy,opened_at,next_grant_at,revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, team.ID, team.Name, team.OwnerID, team.MemberLimit, team.ExpiresAt, team.Disabled, team.GrantAmount, team.Policy, team.OpenedAt, team.OpenedAt.Add(30*24*time.Hour), team.Revision)
	if err != nil {
		return err
	}
	if err = t.AddMember(ctx, team.OwnerID, d.Owner, team.OpenedAt); err != nil {
		return err
	}
	return t.SetTeamDiscounts(ctx, discounts)
}
func (t *spaceTx) SaveTeam(ctx context.Context, team d.Team, expected uint64) error {
	if team.ID != t.id || team.Revision != expected+1 {
		return fault.ErrInvalid
	}
	old, err := t.Team(ctx)
	if err != nil {
		return err
	}
	result, err := t.tx.Exec(ctx, `UPDATE drama_catalog_v1.teams SET name=$2,owner_id=$3,member_limit=$4,expires_at=$5,disabled=$6,grant_amount=$7,policy=$8,revision=$9 WHERE space_id=$1 AND revision=$10`, t.id, team.Name, team.OwnerID, team.MemberLimit, team.ExpiresAt, team.Disabled, team.GrantAmount, team.Policy, team.Revision, expected)
	if err == nil && result.RowsAffected() != 1 {
		return fault.New("SPACE_CONFLICT", "团队信息已变化，请刷新重试")
	}
	if err != nil {
		return err
	}
	// 运营换所有者会使旧提议失效，不能让无权确认的 pending 永久占用唯一槽位。
	if old.OwnerID != team.OwnerID {
		_, err = t.tx.Exec(ctx, `UPDATE drama_catalog_v1.team_ownership_transfers SET status='cancelled',decided_at=now() WHERE space_id=$1 AND status='pending' AND from_user_id<>$2`, t.id, team.OwnerID)
	}
	return err
}
func (t *spaceTx) SetTeamDiscounts(ctx context.Context, discounts []app.ModelDiscount) error {
	if _, err := t.tx.Exec(ctx, `DELETE FROM drama_catalog_v1.team_model_discounts WHERE space_id=$1`, t.id); err != nil {
		return err
	}
	for _, v := range discounts {
		if _, err := t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.team_model_discounts(space_id,model_instance_id,percent) VALUES($1,$2,$3)`, t.id, v.ModelInstanceID, v.Percent); err != nil {
			return err
		}
	}
	return nil
}

const memberColumns = `m.user_id,u.username,u.email,m.role,m.joined_at`
const memberFrom = ` FROM drama_catalog_v1.team_members m JOIN drama_catalog_v1.users u ON u.id=m.user_id `

func scanMember(row rowScanner) (app.Member, error) {
	var v app.Member
	err := row.Scan(&v.UserID, &v.Name, &v.Email, &v.Role, &v.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return v, err
}
func (t *spaceTx) Member(ctx context.Context, user string) (app.Member, error) {
	return scanMember(t.tx.QueryRow(ctx, `SELECT `+memberColumns+memberFrom+`WHERE m.space_id=$1 AND m.user_id=$2`, t.id, user))
}
func spaceMembers(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, id string) ([]app.Member, error) {
	rows, err := q.Query(ctx, `SELECT `+memberColumns+memberFrom+`WHERE m.space_id=$1 ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,m.joined_at,m.user_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.Member{}
	for rows.Next() {
		v, e := scanMember(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (t *spaceTx) Members(ctx context.Context) ([]app.Member, error) {
	return spaceMembers(ctx, t.tx, t.id)
}
func (s *Store) SpaceMembers(ctx context.Context, id string) ([]app.Member, error) {
	return spaceMembers(ctx, s.pool, id)
}
func (t *spaceTx) MemberCount(ctx context.Context) (int, error) {
	var n int
	err := t.tx.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.team_members WHERE space_id=$1`, t.id).Scan(&n)
	return n, err
}
func (t *spaceTx) AddMember(ctx context.Context, user string, role d.Role, at time.Time) error {
	_, err := t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.team_members(space_id,user_id,role,joined_at) VALUES($1,$2,$3,$4)`, t.id, user, role, at)
	if err == nil && role.HoldsCredits() {
		err = ensureHolderOpenTx(ctx, t.tx, t.id, user)
	}
	return err
}
func (t *spaceTx) RemoveMember(ctx context.Context, user string) error {
	result, err := t.tx.Exec(ctx, `DELETE FROM drama_catalog_v1.team_members WHERE space_id=$1 AND user_id=$2`, t.id, user)
	if err == nil && result.RowsAffected() != 1 {
		return fault.ErrNotFound
	}
	if err != nil {
		return err
	}
	// 目标离开后不能再确认旧提议；释放pending槽位，让所有者可转给其他现成员。
	_, err = t.tx.Exec(ctx, `UPDATE drama_catalog_v1.team_ownership_transfers SET status='cancelled',decided_at=now() WHERE space_id=$1 AND to_user_id=$2 AND status='pending'`, t.id, user)
	return err
}
func (t *spaceTx) SetMemberRole(ctx context.Context, user string, role d.Role) error {
	result, err := t.tx.Exec(ctx, `UPDATE drama_catalog_v1.team_members SET role=$3 WHERE space_id=$1 AND user_id=$2`, t.id, user, role)
	if err == nil && result.RowsAffected() != 1 {
		return fault.ErrNotFound
	}
	if err == nil && role.HoldsCredits() {
		err = ensureHolderOpenTx(ctx, t.tx, t.id, user)
	}
	return err
}
func scanSpaceUser(row rowScanner) (app.User, error) {
	var v app.User
	err := row.Scan(&v.ID, &v.Name, &v.Email, &v.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return v, err
}

const spaceUserQuery = `SELECT id,username,email,enabled FROM drama_catalog_v1.users WHERE id=$1 OR email=$2 ORDER BY (id=$1) DESC LIMIT 1 FOR SHARE`

func (t *spaceTx) FindUser(ctx context.Context, lookup string) (app.User, error) {
	return scanSpaceUser(t.tx.QueryRow(ctx, spaceUserQuery, lookup, strings.ToLower(lookup)))
}

func (t *spaceTx) AppendAudit(ctx context.Context, v app.AuditEvent) error {
	if v.SpaceID != "" && v.SpaceID != t.id {
		return fault.ErrInvalid
	}
	detail := v.Detail
	if len(detail) == 0 {
		detail = []byte(`{}`)
	}
	_, err := t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.team_audit_events(space_id,actor_id,actor_kind,action,target,detail) VALUES($1,$2,$3,$4,$5,$6)`, t.id, v.ActorID, v.ActorKind, v.Action, v.Target, detail)
	return err
}
func (t *spaceTx) SystemNotify(ctx context.Context, v n.SystemInput) error {
	_, err := systemNotifyTx(ctx, t.tx, v)
	return err
}
func (t *spaceTx) RecordReminder(ctx context.Context, kind, period string) (bool, error) {
	result, err := t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.team_reminders(space_id,kind,period_key) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, t.id, kind, period)
	return err == nil && result.RowsAffected() == 1, err
}
