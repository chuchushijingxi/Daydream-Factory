package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/spaces"
	"github.com/jackc/pgx/v5"
)

const teamViewColumns = `t.space_id,t.name,t.owner_id,u.username,u.email,
 (SELECT count(*) FROM drama_catalog_v1.team_members m WHERE m.space_id=t.space_id),
 t.member_limit,t.expires_at,t.disabled,t.grant_amount,t.policy,t.opened_at,t.next_grant_at,t.revision,
 coalesce(c.balance,0),coalesce((SELECT jsonb_agg(jsonb_build_object('modelInstanceId',model_instance_id,'percent',percent) ORDER BY model_instance_id) FROM drama_catalog_v1.team_model_discounts WHERE space_id=t.space_id),'[]'::jsonb)`
const teamViewFrom = ` FROM drama_catalog_v1.teams t JOIN drama_catalog_v1.users u ON u.id=t.owner_id LEFT JOIN drama_catalog_v1.credit_accounts c ON c.account_id=t.space_id `

func scanTeamView(row rowScanner) (app.TeamView, error) {
	var v app.TeamView
	var raw []byte
	err := row.Scan(&v.ID, &v.Name, &v.OwnerID, &v.OwnerName, &v.OwnerEmail, &v.MemberCount, &v.MemberLimit, &v.ExpiresAt, &v.Disabled, &v.GrantAmount, &v.Policy, &v.OpenedAt, &v.NextGrantAt, &v.Revision, &v.Balance, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, fault.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(raw, &v.ModelDiscounts); err != nil {
		return v, err
	}
	v.Status = (d.Team{ExpiresAt: v.ExpiresAt, Disabled: v.Disabled}).Status(time.Now().UTC())
	return v, nil
}
func (s *Store) TeamView(ctx context.Context, id string) (app.TeamView, error) {
	return scanTeamView(s.pool.QueryRow(ctx, `SELECT `+teamViewColumns+teamViewFrom+`WHERE t.space_id=$1`, id))
}
func (s *Store) TeamList(ctx context.Context, search string, page, size int) (ports.Page[app.TeamView], error) {
	out := ports.Page[app.TeamView]{Items: []app.TeamView{}, Page: page, PageSize: size}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	const match = `WHERE $1='' OR strpos(lower(t.name),lower($1))>0 OR strpos(lower(u.username),lower($1))>0 OR strpos(lower(u.email),lower($1))>0 OR t.owner_id=$1`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+teamViewFrom+match, search).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT `+teamViewColumns+teamViewFrom+match+` ORDER BY t.opened_at DESC,t.space_id DESC LIMIT $2 OFFSET $3`, search, size, (page-1)*size)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanTeamView(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (s *Store) MySpaces(ctx context.Context, user string) ([]app.SpaceSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.kind,'个人空间','owner',1,1,NULL::timestamptz,'active',false FROM drama_catalog_v1.spaces s WHERE s.id=$1 AND s.kind='personal'
 UNION ALL SELECT t.space_id,'team',t.name,m.role,(SELECT count(*) FROM drama_catalog_v1.team_members all_members WHERE all_members.space_id=t.space_id),t.member_limit,t.expires_at,
 CASE WHEN t.disabled THEN 'disabled' WHEN t.expires_at<=now() THEN 'expired' ELSE 'active' END,(NOT t.disabled AND t.expires_at<=now() AND coalesce((SELECT balance FROM drama_catalog_v1.credit_accounts WHERE account_id=t.space_id),0)=0)
 FROM drama_catalog_v1.team_members m JOIN drama_catalog_v1.teams t ON t.space_id=m.space_id WHERE m.user_id=$1 ORDER BY 2,3,1`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.SpaceSummary{}
	for rows.Next() {
		var v app.SpaceSummary
		if err = rows.Scan(&v.ID, &v.Kind, &v.Name, &v.Role, &v.MemberCount, &v.MemberLimit, &v.ExpiresAt, &v.Status, &v.ReadOnly); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) LookupTeam(ctx context.Context, id string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM drama_catalog_v1.teams WHERE space_id=$1 AND NOT disabled AND expires_at>now()`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return name, err
}
// SpaceAudit 合并有结果的前置记录：申请被批准/拒绝、邮箱邀请被接受/拒绝后只展示结果那一行；
// 原始审计行保留在表中，未处理的申请与邀请仍单独展示。
func (s *Store) SpaceAudit(ctx context.Context, id string, cursor int64, limit int) (app.AuditPage, error) {
	out := app.AuditPage{Items: []app.AuditEvent{}}
	rows, err := s.pool.Query(ctx, `SELECT e.id,e.space_id,e.actor_id,e.actor_kind,e.action,e.target,e.detail,e.created_at FROM drama_catalog_v1.team_audit_events e
 WHERE e.space_id=$1 AND ($2::bigint=0 OR e.id<$2)
 AND NOT (e.action='join_requested' AND EXISTS(SELECT 1 FROM drama_catalog_v1.team_audit_events f WHERE f.space_id=e.space_id AND f.action IN ('join_request_approved','join_request_rejected') AND f.detail->>'requestId'=e.detail->>'requestId'))
 AND NOT (e.action='email_invited' AND EXISTS(SELECT 1 FROM drama_catalog_v1.team_audit_events f WHERE f.space_id=e.space_id AND f.action IN ('member_joined','invite_declined') AND f.detail->>'inviteId'=e.detail->>'inviteId'))
 ORDER BY e.id DESC LIMIT $3`, id, cursor, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v app.AuditEvent
		if err = rows.Scan(&v.ID, &v.SpaceID, &v.ActorID, &v.ActorKind, &v.Action, &v.Target, &v.Detail, &v.CreatedAt); err != nil {
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.NextCursor = out.Items[len(out.Items)-1].ID
	}
	return out, rows.Err()
}

// 筛选尚未投递的指定北京时间日期，防止已提醒团队占满有界扫描窗口。
func (s *Store) DueTeamReminders(ctx context.Context, at time.Time, limit int) ([]d.Team, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+teamColumns+` FROM drama_catalog_v1.teams t WHERE NOT disabled AND (
 (t.expires_at<=$1 AND coalesce((SELECT balance FROM drama_catalog_v1.credit_accounts WHERE account_id=t.space_id),0)=0 AND NOT EXISTS(SELECT 1 FROM drama_catalog_v1.team_reminders r WHERE r.space_id=t.space_id AND r.kind='read_only' AND r.period_key=((extract(epoch FROM t.expires_at)*1000000)::bigint)::text)) OR EXISTS(SELECT 1 FROM (VALUES(7,'expires_7d'),(1,'expires_1d'),(0,'expires_today')) v(days,kind)
 WHERE (t.expires_at AT TIME ZONE 'Asia/Shanghai')::date-($1::timestamptz AT TIME ZONE 'Asia/Shanghai')::date=v.days
 AND NOT EXISTS(SELECT 1 FROM drama_catalog_v1.team_reminders r WHERE r.space_id=t.space_id AND r.kind=v.kind AND r.period_key=((extract(epoch FROM t.expires_at)*1000000)::bigint)::text))
 ) ORDER BY expires_at,space_id LIMIT $2`, at, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []d.Team{}
	for rows.Next() {
		v, e := scanTeam(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SpaceOverview(ctx context.Context, id, span string, at time.Time) (app.Overview, error) {
	team, err := s.TeamView(ctx, id)
	out := app.Overview{Team: team, Range: span, End: at, Daily: []app.DailyConsumption{}, Members: []app.MemberStatistics{}, Recent: []app.AuditEvent{}}
	if err != nil {
		return out, err
	}
	switch span {
	case "7d":
		out.Start = at.AddDate(0, 0, -7)
	case "30d":
		out.Start = at.AddDate(0, 0, -30)
	case "contract":
		out.Start = team.OpenedAt
	default:
		return out, fault.ErrInvalid
	}
	balances, err := s.CreditBalances(ctx, id)
	if err != nil {
		return out, err
	}
	out.Team.Balance = balances.Wallet.Balance
	out.Allocated = balances.Allocated
	out.Unallocated, err = balances.Unallocated()
	if err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD'),sum(CASE kind WHEN 'deduct' THEN amount WHEN 'refund' THEN -amount ELSE 0 END)::bigint FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND created_at>=$2 AND created_at<=$3 AND kind IN ('deduct','refund') GROUP BY 1 ORDER BY 1`, id, out.Start, out.End)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v app.DailyConsumption
		if err = rows.Scan(&v.Date, &v.Credits); err != nil {
			rows.Close()
			return out, err
		}
		out.Daily = append(out.Daily, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// 成员历史包含离开者；创作数量来自不可变原创建人，不以当前负责人覆盖历史。
	rows, err = s.pool.Query(ctx, `WITH charges AS (
 SELECT DISTINCT r#>>'{credit,id}' id,r#>>'{credit,actorId}' actor FROM drama_catalog_v1.projects p CROSS JOIN LATERAL jsonb_array_elements(coalesce(nullif(p.payload->'runs','null'::jsonb),'[]'::jsonb)) r WHERE p.space_id=$1 AND r#>>'{credit,accountId}'=$1 AND r#>>'{credit,actorId}' IS NOT NULL
 UNION SELECT DISTINCT input_snapshot#>>'{callerContext,credit,id}',input_snapshot#>>'{callerContext,credit,actorId}' FROM drama_catalog_v1.tasks WHERE space_id=$1 AND input_snapshot#>>'{callerContext,credit,accountId}'=$1 AND input_snapshot#>>'{callerContext,credit,actorId}' IS NOT NULL
 ),participants AS (
 SELECT user_id FROM drama_catalog_v1.team_members WHERE space_id=$1 UNION SELECT creator_id FROM drama_catalog_v1.tasks WHERE space_id=$1 UNION SELECT creator_id FROM drama_catalog_v1.projects WHERE space_id=$1 UNION SELECT actor FROM charges
 ),media AS (
 SELECT t.creator_id,count(*) FILTER(WHERE f->>'contentType' LIKE 'image/%') images,count(*) FILTER(WHERE f->>'contentType' LIKE 'video/%') videos FROM drama_catalog_v1.tasks t CROSS JOIN LATERAL jsonb_array_elements(coalesce(t.payload#>'{result,files}','[]'::jsonb)) f WHERE t.space_id=$1 AND t.status='succeeded' AND t.created_at>=$2 AND t.created_at<=$3 GROUP BY t.creator_id
 ),charged AS (
 SELECT q.actor,sum(l.amount-coalesce((SELECT sum(r.amount) FROM drama_catalog_v1.credit_ledger r WHERE r.original_deduction_id=l.id AND r.kind='refund'),0))::bigint credits FROM charges q JOIN drama_catalog_v1.credit_ledger l ON l.operation_id='generation:'||q.id||':deduct' AND l.account_id=$1 AND l.kind='deduct' WHERE l.created_at>=$2 AND l.created_at<=$3 GROUP BY q.actor
 )SELECT p.user_id,u.username,coalesce(c.credits,0),coalesce(m.images,0),coalesce(m.videos,0),(SELECT count(*) FROM drama_catalog_v1.projects projects WHERE projects.space_id=$1 AND projects.creator_id=p.user_id AND projects.created_at>=$2 AND projects.created_at<=$3 AND projects.payload->>'deletedAt' IS NULL),coalesce(a.balance,0) FROM participants p JOIN drama_catalog_v1.users u ON u.id=p.user_id LEFT JOIN media m ON m.creator_id=p.user_id LEFT JOIN charged c ON c.actor=p.user_id LEFT JOIN drama_catalog_v1.credit_allocations a ON a.account_id=$1 AND a.holder_id=p.user_id AND a.open ORDER BY u.username,p.user_id`, id, out.Start, out.End)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v app.MemberStatistics
		if err = rows.Scan(&v.UserID, &v.Name, &v.Credits, &v.Images, &v.Videos, &v.Projects, &v.HolderBalance); err != nil {
			rows.Close()
			return out, err
		}
		out.Members = append(out.Members, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	audit, err := s.SpaceAudit(ctx, id, 0, 10)
	out.Recent = audit.Items
	return out, err
}
