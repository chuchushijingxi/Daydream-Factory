package postgres

import (
	"context"
	"errors"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"

	"github.com/jackc/pgx/v5"
)

var _ ports.SpaceResolver = (*Store)(nil)
var _ ports.SpaceAccess = (*Store)(nil)

// 每请求读取当前成员和生命周期事实；运营身份没有内容访问豁免。
func (s *Store) Resolve(ctx context.Context, a ports.Actor, spaceID string) (ports.SpaceScope, error) {
	if err := a.Require(); err != nil {
		return ports.SpaceScope{}, err
	}
	if spaceID == "" {
		spaceID = a.UserID
	}
	return ports.ResolveSpaceOnce(ctx, a, spaceID, func() (ports.SpaceScope, error) {

		var enabled bool
		var kind spaces.Kind
		var role spaces.Role
		var policy spaces.Policy
		var expiry *time.Time
		var disabled bool
		var balance int64
		err := s.pool.QueryRow(ctx, `SELECT u.enabled,s.kind,
   CASE WHEN s.kind='personal' THEN 'owner' ELSE m.role END,
   CASE WHEN s.kind='personal' THEN 'isolated' ELSE t.policy END,t.expires_at,coalesce(t.disabled,false),coalesce(c.balance,0)
 FROM drama_catalog_v1.spaces s
 JOIN drama_catalog_v1.users u ON u.id=$2
   LEFT JOIN drama_catalog_v1.teams t ON t.space_id=s.id
 LEFT JOIN drama_catalog_v1.credit_accounts c ON c.account_id=s.id
   LEFT JOIN drama_catalog_v1.team_members m ON m.space_id=s.id AND m.user_id=$2
   WHERE s.id=$1 AND ((s.kind='personal' AND s.id=$2) OR (s.kind='team' AND m.user_id IS NOT NULL AND NOT t.disabled))`, spaceID, a.UserID).Scan(&enabled, &kind, &role, &policy, &expiry, &disabled, &balance)
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.SpaceScope{}, fault.ErrNotFound
		}
		if err != nil {
			return ports.SpaceScope{}, err
		}
		if !enabled {
			return ports.SpaceScope{}, fault.New("ACCOUNT_DISABLED", "账号已禁用")
		}
		status := spaces.Active
		if kind == spaces.TeamKind && expiry != nil {
			status = (spaces.Team{ExpiresAt: *expiry, Disabled: disabled}).Status(time.Now().UTC())
		}
		return ports.SpaceScope{SpaceID: spaceID, Kind: kind, UserID: a.UserID, Role: role, Policy: policy, Status: status, ReadOnly: kind == spaces.TeamKind && status == spaces.Expired && balance == 0}, nil
	})
}

func (s *Store) Access(ctx context.Context, a ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	// 空资源不能回落到个人空间，防止把缺失事实误当作有效资源。
	if r.SpaceID == "" {
		return spaces.None, ports.SpaceScope{}, fault.ErrNotFound
	}
	scope, err := s.Resolve(ctx, a, r.SpaceID)
	if err != nil {
		return spaces.None, ports.SpaceScope{}, err
	}
	var shares []spaces.Share
	if r.Kind == spaces.ProjectResource || r.Kind == spaces.CanvasResource {
		shares, err = resourceShares(ctx, s.pool, r.SpaceID, r.Kind, r.ID)
		if err != nil {
			return spaces.None, ports.SpaceScope{}, err
		}
	}
	return spaces.Decide(scope, r, shares), scope, nil
}
