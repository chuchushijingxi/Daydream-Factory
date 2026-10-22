package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/spaces"
)

type Service struct {
	Store             Store
	Resolver          ports.SpaceResolver
	Limiter           ports.RateLimiter
	Now               func() time.Time
	Credits           ports.CreditLedger
	CreditQueries     ports.CreditQueries
	AllocationQueries ports.CreditAllocationQueries
	CreditUpdates     ports.CreditUpdates
	TeamUpdates       ports.TeamUpdates
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func invalid() error   { return fault.New("TEAM_INVALID", "团队参数无效") }
func forbidden() error { return fault.New("ROLE_FORBIDDEN", "没有执行该团队操作的权限") }
func validID(id string) bool {
	return id != "" && len(id) <= 128 && utf8.ValidString(id) && strings.TrimSpace(id) == id && !strings.ContainsFunc(id, unicode.IsControl)
}

func (s *Service) withTeam(ctx context.Context, a ports.Actor, id string, action d.Action, fn func(Tx, d.Team, Member) error) error {
	if err := a.Require(); err != nil {
		return err
	}
	if !validID(id) {
		return fault.ErrNotFound
	}
	return s.Store.SpaceTx(ctx, id, func(tx Tx) error {
		team, err := tx.Team(ctx)
		if err != nil {
			return err
		}
		if team.Disabled {
			return fault.ErrNotFound
		}
		member, err := tx.Member(ctx, a.UserID)
		if err != nil {
			return err
		}
		if !member.Role.Can(action) {
			return forbidden()
		}
		return fn(tx, team, member)
	})
}
func audit(ctx context.Context, tx Tx, a ports.Actor, team, action, target string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return tx.AppendAudit(ctx, AuditEvent{SpaceID: team, ActorID: a.UserID, ActorKind: "user", Action: action, Target: target, Detail: raw})
}
func operatorAudit(ctx context.Context, tx Tx, a ports.Actor, team, action, target string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return tx.AppendAudit(ctx, AuditEvent{SpaceID: team, ActorID: a.UserID, ActorKind: "operator", Action: action, Target: target, Detail: raw})
}
func (s *Service) requireManager(ctx context.Context, a ports.Actor, id string, action d.Action) (ports.SpaceScope, error) {
	if err := a.Require(); err != nil {
		return ports.SpaceScope{}, err
	}
	if s.Resolver == nil {
		return ports.SpaceScope{}, fault.New("SPACE_UNAVAILABLE", "空间权限服务尚未组装")
	}
	scope, err := s.Resolver.Resolve(ctx, a, id)
	if err != nil {
		return scope, err
	}
	if scope.Kind != d.TeamKind || !scope.Role.Can(action) {
		return ports.SpaceScope{}, forbidden()
	}
	return scope, nil
}
func (s *Service) List(ctx context.Context, a ports.Actor) ([]SpaceSummary, error) {
	if err := a.Require(); err != nil {
		return nil, err
	}
	return s.Store.MySpaces(ctx, a.UserID)
}
// WatchTeam 仅向当前团队成员开放；事件不含内容，权限由每次重读的 GET 决定。
func (s *Service) WatchTeam(ctx context.Context, a ports.Actor, id string) (<-chan struct{}, func(), error) {
	if err := s.TeamMember(ctx, a, id); err != nil {
		return nil, nil, err
	}
	if s.TeamUpdates == nil {
		return nil, nil, fault.New("TEAM_UPDATES_UNAVAILABLE", "团队实时同步暂不可用")
	}
	return s.TeamUpdates.WatchTeam(ctx, id)
}

// TeamMember 核对调用者仍是该团队成员；长连接每次发送前复核，移出后停止推送。
func (s *Service) TeamMember(ctx context.Context, a ports.Actor, id string) error {
	if err := a.Require(); err != nil {
		return err
	}
	if s.Resolver == nil {
		return fault.New("SPACE_UNAVAILABLE", "空间权限服务尚未组装")
	}
	scope, err := s.Resolver.Resolve(ctx, a, id)
	if err != nil {
		return err
	}
	if scope.Kind != d.TeamKind {
		return fault.ErrNotFound
	}
	return nil
}

func (s *Service) Detail(ctx context.Context, a ports.Actor, id string) (Detail, error) {
	if err := a.Require(); err != nil {
		return Detail{}, err
	}
	if s.Resolver == nil {
		return Detail{}, fault.New("SPACE_UNAVAILABLE", "空间权限服务尚未组装")
	}
	scope, err := s.Resolver.Resolve(ctx, a, id)
	if err != nil {
		return Detail{}, err
	}
	spaces, err := s.Store.MySpaces(ctx, a.UserID)
	if err != nil {
		return Detail{}, err
	}
	for _, space := range spaces {
		if space.ID == scope.SpaceID {
			out := Detail{SpaceSummary: space}
			if scope.Kind == d.TeamKind && scope.Role.Can(d.ManageSettings) {
				team, e := s.Store.TeamView(ctx, id)
				if e != nil {
					return Detail{}, e
				}
				out.Team = &team
			}
			return out, nil
		}
	}
	return Detail{}, fault.ErrNotFound
}
func validateDiscounts(items []ModelDiscount) error {
	seen := map[string]bool{}
	for _, item := range items {
		if !validID(item.ModelInstanceID) || item.Percent < 1 || item.Percent > 100 || seen[item.ModelInstanceID] {
			return invalid()
		}
		seen[item.ModelInstanceID] = true
	}
	return nil
}
func (s *Service) Open(ctx context.Context, a ports.Actor, in TeamInput) (TeamView, error) {
	if err := a.RequireOperator(); err != nil {
		return TeamView{}, err
	}
	name, err := d.NormalizeName(in.Name)
	if err != nil {
		return TeamView{}, err
	}
	at := s.now()
	if !validID(in.OwnerID) || in.MemberLimit < 1 || in.GrantAmount < 0 || in.GrantAmount > ports.MaxSafeInteger || !in.ExpiresAt.After(at) {
		return TeamView{}, invalid()
	}
	if err = validateDiscounts(in.ModelDiscounts); err != nil {
		return TeamView{}, err
	}
	team := d.Team{ID: identity.New("team"), Name: name, OwnerID: in.OwnerID, MemberLimit: in.MemberLimit, ExpiresAt: in.ExpiresAt, GrantAmount: in.GrantAmount, Policy: d.ReadAll, OpenedAt: at, Revision: 1}
	err = s.Store.SpaceTx(ctx, team.ID, func(tx Tx) error {
		user, e := tx.FindUser(ctx, in.OwnerID)
		if e != nil {
			return e
		}
		if !user.Enabled {
			return fault.New("TEAM_USER_UNAVAILABLE", "团队所有者必须是已启用的注册用户")
		}
		if e = tx.CreateTeam(ctx, team, in.ModelDiscounts); e != nil {
			return e
		}
		if e = tx.EnsureCreditAccount(ctx, team.ID); e != nil {
			return e
		}
		schedule := d.InitialGrant(at)
		if e = grantCredits(ctx, tx, team, schedule.Sequence); e != nil {
			return e
		}
		if e = tx.SaveGrantSchedule(ctx, schedule); e != nil {
			return e
		}
		return operatorAudit(ctx, tx, a, team.ID, "team_opened", user.ID, in)
	})
	if err != nil {
		return TeamView{}, err
	}
	return s.Store.TeamView(ctx, team.ID)
}
func (s *Service) OperatorList(ctx context.Context, a ports.Actor, q string, page, size int) (ports.Page[TeamView], error) {
	if err := a.RequireOperator(); err != nil {
		return ports.Page[TeamView]{}, err
	}
	if err := ports.CheckPage(page, size); err != nil {
		return ports.Page[TeamView]{}, err
	}
	if len(q) > 200 {
		return ports.Page[TeamView]{}, invalid()
	}
	return s.Store.TeamList(ctx, strings.TrimSpace(q), page, size)
}
func (s *Service) OperatorDetail(ctx context.Context, a ports.Actor, id string) (TeamView, error) {
	if err := a.RequireOperator(); err != nil {
		return TeamView{}, err
	}
	return s.Store.TeamView(ctx, id)
}
func (s *Service) Update(ctx context.Context, a ports.Actor, id string, in SettingsInput) (TeamView, error) {
	name, err := d.NormalizeName(in.Name)
	if err != nil {
		return TeamView{}, err
	}
	if !in.Policy.Valid() || in.Revision == 0 || in.Revision >= ports.MaxSafeInteger {
		return TeamView{}, invalid()
	}
	err = s.withTeam(ctx, a, id, d.ManageSettings, func(tx Tx, team d.Team, _ Member) error {
		if team.Revision != in.Revision {
			return fault.ErrConflict
		}
		previous := team
		team.Name, team.Policy, team.Revision = name, in.Policy, team.Revision+1
		if e := tx.SaveTeam(ctx, team, in.Revision); e != nil {
			return e
		}
		if previous.Name != team.Name {
			if e := audit(ctx, tx, a, id, "team_renamed", id, map[string]string{"name": name}); e != nil {
				return e
			}
		}
		if previous.Policy != team.Policy {
			return audit(ctx, tx, a, id, "visibility_changed", id, map[string]d.Policy{"policy": in.Policy})
		}
		return nil
	})
	if err != nil {
		return TeamView{}, err
	}
	return s.Store.TeamView(ctx, id)
}
func (s *Service) Members(ctx context.Context, a ports.Actor, id string) ([]Member, error) {
	scope, err := s.requireManager(ctx, a, id, d.ViewMembers)
	if err != nil {
		return nil, err
	}
	members, err := s.Store.SpaceMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	if scope.Role == d.Member {
		for i := range members {
			members[i].Email = ""
			members[i].JoinedAt = nil
		}
	}
	return members, nil
}
func (s *Service) Overview(ctx context.Context, a ports.Actor, id, period string) (Overview, error) {
	if _, err := s.requireManager(ctx, a, id, d.ViewOverview); err != nil {
		return Overview{}, err
	}
	if period != "7d" && period != "30d" && period != "contract" {
		return Overview{}, invalid()
	}
	return s.Store.SpaceOverview(ctx, id, period, s.now())
}
func (s *Service) Audit(ctx context.Context, a ports.Actor, id string, cursor int64, limit int) (AuditPage, error) {
	if _, err := s.requireManager(ctx, a, id, d.ViewAudit); err != nil {
		return AuditPage{}, err
	}
	if cursor < 0 || limit < 1 || limit > 100 {
		return AuditPage{}, invalid()
	}
	return s.Store.SpaceAudit(ctx, id, cursor, limit)
}
func isMissing(err error) bool { return errors.Is(err, fault.ErrNotFound) }
