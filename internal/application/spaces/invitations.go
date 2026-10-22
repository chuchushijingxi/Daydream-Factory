package spaces

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	n "drama-agent/internal/domain/notifications"
	d "drama-agent/internal/domain/spaces"
)

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func linkToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
func validToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == token
}
func (s *Service) CreateInviteLink(ctx context.Context, a ports.Actor, id string) (InviteLink, error) {
	token, err := linkToken()
	if err != nil {
		return InviteLink{}, err
	}
	var out Invite
	err = s.withTeam(ctx, a, id, d.InviteMembers, func(tx Tx, team d.Team, _ Member) error {
		at := s.now()
		if e := team.RequireActive(at); e != nil {
			return e
		}
		count, e := tx.MemberCount(ctx)
		if e != nil {
			return e
		}
		if e = team.RequireCapacity(count); e != nil {
			return e
		}
		out = Invite{ID: identity.New("invite"), SpaceID: id, TeamName: team.Name, Kind: "link", TokenDigest: tokenDigest(token), CreatedBy: a.UserID, CreatedAt: at, ExpiresAt: at.Add(7 * 24 * time.Hour), Status: "pending"}
		if e = tx.SaveInvite(ctx, out); e != nil {
			return e
		}
		return audit(ctx, tx, a, id, "invite_link_created", out.ID, nil)
	})
	if err != nil {
		return InviteLink{}, err
	}
	return InviteLink{Invite: out, Token: token}, nil
}
func (s *Service) RevokeInviteLink(ctx context.Context, a ports.Actor, id, inviteID string) error {
	return s.withTeam(ctx, a, id, d.InviteMembers, func(tx Tx, _ d.Team, _ Member) error {
		invite, err := tx.Invite(ctx, inviteID)
		if err != nil {
			return err
		}
		if invite.SpaceID != id || invite.Kind != "link" {
			return fault.ErrNotFound
		}
		if invite.RevokedAt != nil {
			return nil
		}
		at := s.now()
		invite.RevokedAt = &at
		if err = tx.SaveInvite(ctx, invite); err != nil {
			return err
		}
		return audit(ctx, tx, a, id, "invite_link_revoked", invite.ID, nil)
	})
}
func (s *Service) InviteEmail(ctx context.Context, a ports.Actor, id, email string) (Invite, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 {
		return Invite{}, invalid()
	}
	var out Invite
	err = s.withTeam(ctx, a, id, d.InviteMembers, func(tx Tx, team d.Team, _ Member) error {
		at := s.now()
		if e := team.RequireActive(at); e != nil {
			return e
		}
		count, e := tx.MemberCount(ctx)
		if e != nil {
			return e
		}
		if e = team.RequireCapacity(count); e != nil {
			return e
		}
		user, e := tx.FindUser(ctx, email)
		if e != nil {
			return e
		}
		if !user.Enabled {
			return fault.New("TEAM_USER_UNAVAILABLE", "只能邀请已启用的注册用户")
		}
		if _, e = tx.Member(ctx, user.ID); e == nil {
			return fault.New("TEAM_ALREADY_MEMBER", "该用户已经是团队成员")
		} else if !isMissing(e) {
			return e
		}
		out = Invite{ID: identity.New("invite"), SpaceID: id, TeamName: team.Name, Kind: "email", InviteeUserID: user.ID, CreatedBy: a.UserID, CreatedAt: at, ExpiresAt: at.Add(7 * 24 * time.Hour), Status: "pending"}
		if e = tx.SaveInvite(ctx, out); e != nil {
			return e
		}
		if e = audit(ctx, tx, a, id, "email_invited", user.ID, map[string]string{"inviteId": out.ID}); e != nil {
			return e
		}
		return notify(ctx, tx, user.ID, "team-invite:"+out.ID, "你收到团队邀请", n.EscapeMarkdownText(team.Name)+"：邀请你加入团队，请在7天内确认。", n.Link{Type: "invite", SpaceID: id, TargetID: out.ID})
	})
	return out, err
}
func (s *Service) Invites(ctx context.Context, a ports.Actor, id string) ([]Invite, error) {
	if _, err := s.requireManager(ctx, a, id, d.InviteMembers); err != nil {
		return nil, err
	}
	items, err := s.Store.SpaceInvites(ctx, id)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Status == "pending" && !s.now().Before(items[i].ExpiresAt) {
			items[i].Status = "expired"
		}
	}
	return items, nil
}
func (s *Service) PendingInvites(ctx context.Context, a ports.Actor) ([]Invite, error) {
	if err := a.Require(); err != nil {
		return nil, err
	}
	return s.Store.PendingInvites(ctx, a.UserID)
}
func (s *Service) PreviewLink(ctx context.Context, a ports.Actor, token string) (InvitePreview, error) {
	if err := a.Require(); err != nil {
		return InvitePreview{}, err
	}
	if !validToken(token) {
		return InvitePreview{}, fault.ErrNotFound
	}
	invite, err := s.Store.InviteByDigest(ctx, tokenDigest(token))
	if err != nil {
		return InvitePreview{}, err
	}
	if invite.Kind != "link" {
		return InvitePreview{}, fault.ErrNotFound
	}
	if err = d.RequireInvite(invite.Status, invite.ExpiresAt, invite.RevokedAt, s.now()); err != nil {
		return InvitePreview{}, err
	}
	name, err := s.Store.LookupTeam(ctx, invite.SpaceID)
	if err != nil {
		return InvitePreview{}, err
	}
	return InvitePreview{ID: invite.ID, SpaceID: invite.SpaceID, TeamName: name, ExpiresAt: invite.ExpiresAt}, nil
}
func (s *Service) AcceptLink(ctx context.Context, a ports.Actor, token string) (SpaceSummary, error) {
	if err := a.Require(); err != nil {
		return SpaceSummary{}, err
	}
	if !validToken(token) {
		return SpaceSummary{}, fault.ErrNotFound
	}
	invite, err := s.Store.InviteByDigest(ctx, tokenDigest(token))
	if err != nil {
		return SpaceSummary{}, err
	}
	if invite.Kind != "link" {
		return SpaceSummary{}, fault.ErrNotFound
	}
	return s.accept(ctx, a, invite)
}
func (s *Service) AcceptInvite(ctx context.Context, a ports.Actor, id string) (SpaceSummary, error) {
	if err := a.Require(); err != nil {
		return SpaceSummary{}, err
	}
	invite, err := s.Store.SpaceInvite(ctx, id)
	if err != nil {
		return SpaceSummary{}, err
	}
	if invite.Kind != "email" || invite.InviteeUserID != a.UserID {
		return SpaceSummary{}, fault.ErrNotFound
	}
	return s.accept(ctx, a, invite)
}
func (s *Service) accept(ctx context.Context, a ports.Actor, known Invite) (SpaceSummary, error) {
	var out SpaceSummary
	// 链接允许多人使用，离开后也可能再次接受同一链接；每次加入是独立的通知事实。
	joinEventID := identity.New("team_join")
	err := s.Store.SpaceTx(ctx, known.SpaceID, func(tx Tx) error {
		team, err := tx.Team(ctx)
		if err != nil {
			return err
		}
		at := s.now()
		if err = team.RequireActive(at); err != nil {
			return err
		}
		invite, err := tx.Invite(ctx, known.ID)
		if err != nil {
			return err
		}
		if invite.SpaceID != team.ID || invite.Kind != known.Kind || (invite.Kind == "email" && invite.InviteeUserID != a.UserID) {
			return fault.ErrNotFound
		}
		if err = d.RequireInvite(invite.Status, invite.ExpiresAt, invite.RevokedAt, at); err != nil {
			return err
		}
		if _, err = tx.Member(ctx, a.UserID); err == nil {
			return fault.New("TEAM_ALREADY_MEMBER", "你已经是团队成员")
		} else if !isMissing(err) {
			return err
		}
		user, err := tx.FindUser(ctx, a.UserID)
		if err != nil {
			return err
		}
		if !user.Enabled {
			return fault.New("TEAM_USER_UNAVAILABLE", "账号已停用")
		}
		count, err := tx.MemberCount(ctx)
		if err != nil {
			return err
		}
		if err = team.RequireCapacity(count); err != nil {
			return err
		}
		if err = tx.AddMember(ctx, a.UserID, d.Member, at); err != nil {
			return err
		}
		if invite.Kind == "email" {
			invite.Status = "accepted"
			if err = tx.SaveInvite(ctx, invite); err != nil {
				return err
			}
		}
		if err = audit(ctx, tx, a, team.ID, "member_joined", a.UserID, map[string]string{"inviteId": invite.ID, "eventId": joinEventID}); err != nil {
			return err
		}
		if err = notifyManagers(ctx, tx, "team-joined:"+joinEventID, "新成员已加入团队", n.EscapeMarkdownText(team.Name)+"："+n.EscapeMarkdownText(user.Name)+"已接受邀请并加入团队。", n.Link{Type: "team", SpaceID: team.ID}); err != nil {
			return err
		}
		out = summary(team, d.Member, count+1, at)
		return nil
	})
	return out, err
}
func summary(team d.Team, role d.Role, count int, at time.Time) SpaceSummary {
	return SpaceSummary{ID: team.ID, Kind: d.TeamKind, Name: team.Name, Role: role, MemberCount: count, MemberLimit: team.MemberLimit, ExpiresAt: &team.ExpiresAt, Status: team.Status(at), ReadOnly: false}
}
func (s *Service) DeclineInvite(ctx context.Context, a ports.Actor, id string) error {
	if err := a.Require(); err != nil {
		return err
	}
	known, err := s.Store.SpaceInvite(ctx, id)
	if err != nil {
		return err
	}
	if known.Kind != "email" || known.InviteeUserID != a.UserID {
		return fault.ErrNotFound
	}
	return s.Store.SpaceTx(ctx, known.SpaceID, func(tx Tx) error {
		invite, err := tx.Invite(ctx, id)
		if err != nil {
			return err
		}
		if invite.Kind != "email" || invite.InviteeUserID != a.UserID {
			return fault.ErrNotFound
		}
		if err = d.RequireInvite(invite.Status, invite.ExpiresAt, invite.RevokedAt, s.now()); err != nil {
			return err
		}
		invite.Status = "declined"
		if err = tx.SaveInvite(ctx, invite); err != nil {
			return err
		}
		return audit(ctx, tx, a, known.SpaceID, "invite_declined", a.UserID, map[string]string{"inviteId": id})
	})
}
func (s *Service) limited(ctx context.Context, user, kind string) error {
	if s.Limiter == nil {
		return fault.New("RUNTIME_UNAVAILABLE", "团队申请限流服务尚未组装")
	}
	allowed, err := s.Limiter.Allow(ctx, "team:"+kind+":"+user, 10, time.Minute)
	if err != nil {
		return err
	}
	if !allowed {
		return fault.New("RATE_LIMITED", "请求过于频繁，请稍后重试")
	}
	return nil
}
func (s *Service) Lookup(ctx context.Context, a ports.Actor, id string) (string, error) {
	if err := a.Require(); err != nil {
		return "", err
	}
	if err := s.limited(ctx, a.UserID, "lookup"); err != nil {
		return "", err
	}
	if !validID(id) {
		return "", fault.New("TEAM_UNAVAILABLE", "团队不存在或不可申请")
	}
	name, err := s.Store.LookupTeam(ctx, id)
	if isMissing(err) {
		return "", fault.New("TEAM_UNAVAILABLE", "团队不存在或不可申请")
	}
	return name, err
}
func (s *Service) RequestJoin(ctx context.Context, a ports.Actor, id, message string) (JoinRequest, error) {
	if err := a.Require(); err != nil {
		return JoinRequest{}, err
	}
	if !validID(id) || !utf8.ValidString(message) || utf8.RuneCountInString(message) > 200 || strings.ContainsRune(message, 0) {
		return JoinRequest{}, invalid()
	}
	if err := s.limited(ctx, a.UserID, "join"); err != nil {
		return JoinRequest{}, err
	}
	var out JoinRequest
	err := s.Store.SpaceTx(ctx, id, func(tx Tx) error {
		team, err := tx.Team(ctx)
		if err != nil {
			return err
		}
		if err = team.RequireActive(s.now()); err != nil {
			return err
		}
		if _, err = tx.Member(ctx, a.UserID); err == nil {
			return fault.New("TEAM_ALREADY_MEMBER", "你已经是团队成员")
		} else if !isMissing(err) {
			return err
		}
		user, err := tx.FindUser(ctx, a.UserID)
		if err != nil {
			return err
		}
		if !user.Enabled {
			return fault.New("TEAM_USER_UNAVAILABLE", "账号已停用")
		}
		out = JoinRequest{ID: identity.New("join"), SpaceID: id, UserID: a.UserID, UserName: user.Name, Message: message, Status: "pending", CreatedAt: s.now()}
		if err = tx.SaveJoinRequest(ctx, out); err != nil {
			return err
		}
		if err = audit(ctx, tx, a, id, "join_requested", a.UserID, map[string]string{"requestId": out.ID}); err != nil {
			return err
		}
		return notifyManagers(ctx, tx, "team-join-request:"+out.ID, "收到团队加入申请", n.EscapeMarkdownText(team.Name)+"："+n.EscapeMarkdownText(user.Name)+"申请加入团队。", n.Link{Type: "join_request", SpaceID: id, TargetID: out.ID})
	})
	return out, err
}
func (s *Service) JoinRequests(ctx context.Context, a ports.Actor, id string) ([]JoinRequest, error) {
	if _, err := s.requireManager(ctx, a, id, d.DecideJoinRequest); err != nil {
		return nil, err
	}
	return s.Store.SpaceJoinRequests(ctx, id)
}
func (s *Service) DecideJoin(ctx context.Context, a ports.Actor, id, requestID string, approve bool) error {
	return s.withTeam(ctx, a, id, d.DecideJoinRequest, func(tx Tx, team d.Team, _ Member) error {
		request, err := tx.JoinRequest(ctx, requestID)
		if err != nil {
			return err
		}
		if request.SpaceID != id {
			return fault.ErrNotFound
		}
		if err = d.RequirePendingRequest(request.Status); err != nil {
			return err
		}
		at := s.now()
		if approve {
			if err = team.RequireActive(at); err != nil {
				return err
			}
			if _, err = tx.Member(ctx, request.UserID); err == nil {
				return fault.New("TEAM_ALREADY_MEMBER", "申请人已经是团队成员")
			} else if !isMissing(err) {
				return err
			}
			user, e := tx.FindUser(ctx, request.UserID)
			if e != nil {
				return e
			}
			if !user.Enabled {
				return fault.New("TEAM_USER_UNAVAILABLE", "申请人账号已停用")
			}
			count, e := tx.MemberCount(ctx)
			if e != nil {
				return e
			}
			if e = team.RequireCapacity(count); e != nil {
				return e
			}
			if e = tx.AddMember(ctx, request.UserID, d.Member, at); e != nil {
				return e
			}
			request.Status = "approved"
		} else {
			request.Status = "rejected"
		}
		request.DecidedBy = a.UserID
		request.DecidedAt = &at
		if err = tx.SaveJoinRequest(ctx, request); err != nil {
			return err
		}
		if err = audit(ctx, tx, a, id, "join_request_"+request.Status, request.UserID, map[string]string{"requestId": request.ID}); err != nil {
			return err
		}
		body := n.EscapeMarkdownText(team.Name) + "：你的加入申请已被拒绝。"
		if approve {
			body = n.EscapeMarkdownText(team.Name) + "：你的加入申请已通过。"
		}
		return notify(ctx, tx, request.UserID, "team-join-result:"+request.ID, "团队加入申请已处理", body, n.Link{Type: "team", SpaceID: id})
	})
}
