package spaces

import (
	"context"
	"sort"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	n "drama-agent/internal/domain/notifications"
	d "drama-agent/internal/domain/spaces"
)

func notify(ctx context.Context, tx Tx, recipient, operation, title, body string, link n.Link) error {
	return tx.SystemNotify(ctx, n.SystemInput{OperationID: operation, RecipientUserID: recipient, Title: title, Body: body, Link: link})
}
func notifyManagers(ctx context.Context, tx Tx, operation, title, body string, link n.Link) error {
	members, err := tx.Members(ctx)
	if err != nil {
		return err
	}
	// 通知触发器会锁每位接收人的更新行；跨团队广播按同一顺序锁定，避免交叉死锁。
	sort.Slice(members, func(i, j int) bool { return members[i].UserID < members[j].UserID })
	for _, member := range members {
		if member.Role == d.Owner || member.Role == d.Admin {
			if err = notify(ctx, tx, member.UserID, operation, title, body, link); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) RemoveMember(ctx context.Context, a ports.Actor, id, user string) error {
	return s.withTeam(ctx, a, id, d.RemoveMembers, func(tx Tx, team d.Team, actor Member) error {
		target, err := tx.Member(ctx, user)
		if err != nil {
			return err
		}
		if !actor.Role.CanRemove(target.Role) {
			return forbidden()
		}
		if err = releaseHolder(ctx, tx, a, id, user); err != nil {
			return err
		}
		if err = tx.RemoveMember(ctx, user); err != nil {
			return err
		}
		if err = audit(ctx, tx, a, id, "member_removed", user, nil); err != nil {
			return err
		}
		return notify(ctx, tx, user, "team-member-removed:"+identity.New("event"), "你已被移出团队", n.EscapeMarkdownText(team.Name)+"：管理员已将你移出团队，原有内容保留在团队中。", n.Link{Type: "team", SpaceID: id})
	})
}
func (s *Service) Leave(ctx context.Context, a ports.Actor, id string) error {
	return s.withTeam(ctx, a, id, d.LeaveTeam, func(tx Tx, _ d.Team, _ Member) error {
		if err := releaseHolder(ctx, tx, a, id, a.UserID); err != nil {
			return err
		}
		if err := tx.RemoveMember(ctx, a.UserID); err != nil {
			return err
		}
		return audit(ctx, tx, a, id, "member_left", a.UserID, nil)
	})
}
func (s *Service) SetRole(ctx context.Context, a ports.Actor, id, user string, role d.Role) error {
	if role != d.Admin && role != d.Member {
		return invalid()
	}
	return s.withTeam(ctx, a, id, d.ManageRoles, func(tx Tx, team d.Team, _ Member) error {
		target, err := tx.Member(ctx, user)
		if err != nil {
			return err
		}
		if target.Role == d.Owner {
			return forbidden()
		}
		if target.Role == role {
			return nil
		}
		// 成员与管理员都持有名下分配额，升降级保留本人已分配的积分。
		if err = tx.SetMemberRole(ctx, user, role); err != nil {
			return err
		}
		if err = audit(ctx, tx, a, id, "member_role_changed", user, map[string]d.Role{"role": role}); err != nil {
			return err
		}
		return notify(ctx, tx, user, "team-role:"+identity.New("event"), "团队角色已变更", n.EscapeMarkdownText(team.Name)+"：你的团队角色已更新。", n.Link{Type: "team", SpaceID: id})
	})
}
func (s *Service) BeginOwnership(ctx context.Context, a ports.Actor, id, target string) (OwnershipTransfer, error) {
	if !validID(target) || target == a.UserID {
		return OwnershipTransfer{}, invalid()
	}
	var out OwnershipTransfer
	err := s.withTeam(ctx, a, id, d.TransferOwnership, func(tx Tx, team d.Team, _ Member) error {
		if _, err := tx.Member(ctx, target); err != nil {
			return err
		}
		user, err := tx.FindUser(ctx, target)
		if err != nil {
			return err
		}
		if !user.Enabled {
			return fault.New("TEAM_USER_UNAVAILABLE", "接收人账号已停用")
		}
		out = OwnershipTransfer{ID: identity.New("ownership"), SpaceID: id, FromOwnerID: team.OwnerID, ToUserID: target, Status: "pending", CreatedAt: s.now()}
		pending, err := tx.PendingOwnershipTransfers(ctx)
		if err != nil {
			return err
		}
		// 新提议（包括同一接收人）替换旧提议；取消、审计和新通知同事务提交。
		// 通知失败时全部回滚，旧接收人的确认仍然有效。
		for _, previous := range pending {
			previous.Status = "cancelled"
			previous.AcceptedAt = nil
			if err = tx.SaveOwnershipTransfer(ctx, previous); err != nil {
				return err
			}
			if err = audit(ctx, tx, a, id, "ownership_superseded", previous.ID, map[string]string{
				"oldTransferId": previous.ID,
				"newTransferId": out.ID,
			}); err != nil {
				return err
			}
		}
		if err = tx.SaveOwnershipTransfer(ctx, out); err != nil {
			return err
		}
		if err = audit(ctx, tx, a, id, "ownership_proposed", target, map[string]string{"transferId": out.ID}); err != nil {
			return err
		}
		return notify(ctx, tx, target, "team-ownership:"+out.ID, "请确认团队所有权转让", n.EscapeMarkdownText(team.Name)+"：团队所有者向你发起了所有权转让，请确认。", n.Link{Type: "ownership_transfer", SpaceID: id, TargetID: out.ID})
	})
	return out, err
}
func (s *Service) AcceptOwnership(ctx context.Context, a ports.Actor, id, transferID string) error {
	return s.withTeam(ctx, a, id, d.ViewMembers, func(tx Tx, team d.Team, member Member) error {
		transfer, err := tx.OwnershipTransfer(ctx, transferID)
		if err != nil {
			return err
		}
		if transfer.SpaceID != id {
			return fault.ErrNotFound
		}
		if err = d.RequireOwnershipAcceptance(transfer.Status, transfer.FromOwnerID, team.OwnerID, transfer.ToUserID, a.UserID); err != nil {
			return err
		}
		user, err := tx.FindUser(ctx, a.UserID)
		if err != nil {
			return err
		}
		if !user.Enabled {
			return fault.New("TEAM_USER_UNAVAILABLE", "接收人账号已停用")
		}
		recipient, err := tx.Member(ctx, a.UserID)
		if err != nil {
			return err
		}
		// 所有者直接使用未分配积分，接任前名下分配额退回团队。
		if recipient.Role.HoldsCredits() {
			if err = releaseHolder(ctx, tx, a, id, a.UserID); err != nil {
				return err
			}
		}
		if err = tx.SetMemberRole(ctx, team.OwnerID, d.Admin); err != nil {
			return err
		}
		if err = tx.SetMemberRole(ctx, a.UserID, d.Owner); err != nil {
			return err
		}
		if team.Revision >= ports.MaxSafeInteger {
			return fault.ErrConflict
		}
		previous := team.Revision
		team.OwnerID = a.UserID
		team.Revision++
		if err = tx.SaveTeam(ctx, team, previous); err != nil {
			return err
		}
		at := s.now()
		transfer.Status = "accepted"
		transfer.AcceptedAt = &at
		if err = tx.SaveOwnershipTransfer(ctx, transfer); err != nil {
			return err
		}
		if err = audit(ctx, tx, a, id, "ownership_transferred", a.UserID, map[string]string{"fromOwnerId": transfer.FromOwnerID, "transferId": transfer.ID}); err != nil {
			return err
		}
		return notify(ctx, tx, transfer.FromOwnerID, "team-ownership-accepted:"+transfer.ID, "团队所有权已转让", n.EscapeMarkdownText(team.Name)+"：对方已确认接收所有权，你现在是管理员。", n.Link{Type: "team", SpaceID: id})
	})
}
func (s *Service) TransferContent(ctx context.Context, a ports.Actor, id string, in TransferInput) ([]TransferReceipt, error) {
	if _, err := s.requireManager(ctx, a, id, d.TransferContent); err != nil {
		return nil, err
	}
	if !validID(in.FromUserID) || !validID(in.ToUserID) || in.FromUserID == in.ToUserID || (!in.All && len(in.Resources) == 0) || (in.All && len(in.Resources) > 0) || len(in.Resources) > 1000 {
		return nil, invalid()
	}
	for _, item := range in.Resources {
		if !validID(item.ID) || (item.Kind != d.ProjectResource && item.Kind != d.UploadResource && item.Kind != d.MediaResource && item.Kind != d.CanvasResource && item.Kind != d.AgentSessionResource) {
			return nil, invalid()
		}
	}
	return s.Store.TransferSpaceContent(ctx, id, a.UserID, in)
}
