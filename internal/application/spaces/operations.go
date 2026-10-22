package spaces

import (
	"context"
	"strconv"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	n "drama-agent/internal/domain/notifications"
	d "drama-agent/internal/domain/spaces"
)

func revisionString(revision uint64) string { return strconv.FormatUint(revision, 10) }

func (s *Service) OperatorUpdate(ctx context.Context, a ports.Actor, id string, in TeamPatch) (TeamView, error) {
	if err := a.RequireOperator(); err != nil {
		return TeamView{}, err
	}
	if in.Revision == 0 || in.Revision >= ports.MaxSafeInteger || (in.ExpiresAt == nil && in.MemberLimit == nil && in.GrantAmount == nil && in.OwnerID == nil && in.Disabled == nil && in.ModelDiscounts == nil) {
		return TeamView{}, invalid()
	}
	if in.ModelDiscounts != nil {
		if err := validateDiscounts(*in.ModelDiscounts); err != nil {
			return TeamView{}, err
		}
	}
	err := s.Store.SpaceTx(ctx, id, func(tx Tx) error {
		team, err := tx.Team(ctx)
		if err != nil {
			return err
		}
		if team.Revision != in.Revision {
			return fault.ErrConflict
		}
		original, at := team, s.now()
		type change struct {
			action, target string
			detail         any
		}
		var changes []change
		if in.ExpiresAt != nil {
			if !in.ExpiresAt.After(team.ExpiresAt) || !in.ExpiresAt.After(s.now()) {
				return invalid()
			}
			team.ExpiresAt = *in.ExpiresAt
			changes = append(changes, change{"team_renewed", id, map[string]any{"expiresAt": team.ExpiresAt}})
		}
		if in.MemberLimit != nil {
			if *in.MemberLimit < 1 {
				return invalid()
			}
			team.MemberLimit = *in.MemberLimit
			changes = append(changes, change{"member_limit_changed", id, map[string]int{"memberLimit": team.MemberLimit}})
		}
		if in.GrantAmount != nil {
			if *in.GrantAmount < 0 || *in.GrantAmount > ports.MaxSafeInteger {
				return invalid()
			}
			team.GrantAmount = *in.GrantAmount
			changes = append(changes, change{"grant_amount_changed", id, map[string]int64{"grantAmount": team.GrantAmount}})
		}
		if in.Disabled != nil {
			team.Disabled = *in.Disabled
			action := "team_enabled"
			if team.Disabled {
				action = "team_disabled"
			}
			changes = append(changes, change{action, id, nil})
		}
		if in.ModelDiscounts != nil {
			if err = tx.SetTeamDiscounts(ctx, *in.ModelDiscounts); err != nil {
				return err
			}
			changes = append(changes, change{"model_discounts_changed", id, *in.ModelDiscounts})
		}
		if in.OwnerID != nil && *in.OwnerID != team.OwnerID {
			if !validID(*in.OwnerID) {
				return invalid()
			}
			recipient, e := tx.Member(ctx, *in.OwnerID)
			if e != nil {
				return e
			}
			if recipient.Role.HoldsCredits() {
				if e = releaseHolder(ctx, tx, a, id, recipient.UserID); e != nil {
					return e
				}
			}
			user, e := tx.FindUser(ctx, *in.OwnerID)
			if e != nil {
				return e
			}
			if !user.Enabled {
				return fault.New("TEAM_USER_UNAVAILABLE", "接收人账号已停用")
			}
			oldOwner := team.OwnerID
			if err = tx.SetMemberRole(ctx, oldOwner, d.Admin); err != nil {
				return err
			}
			if err = tx.SetMemberRole(ctx, *in.OwnerID, d.Owner); err != nil {
				return err
			}
			team.OwnerID = *in.OwnerID
			changes = append(changes, change{"owner_changed_by_operator", team.OwnerID, map[string]string{"fromOwnerId": oldOwner}})
			if err = notify(ctx, tx, team.OwnerID, "team-operator-owner:"+id+":"+revisionString(in.Revision), "团队所有者已更新", n.EscapeMarkdownText(team.Name)+"：运营已将你设为团队所有者。", n.Link{Type: "team", SpaceID: id}); err != nil {
				return err
			}
		}
		// 到期后在启用状态下续约立即发放；未到期续约保持原节奏。
		// 保持停用的续约只延长合同，之后启用再沿原节奏跳过停用周期。
		if in.ExpiresAt != nil && !at.Before(original.ExpiresAt) && !team.Disabled {
			schedule, e := tx.GrantSchedule(ctx)
			if e != nil {
				return e
			}
			next, e := schedule.AfterRenewal(at)
			if e != nil {
				return e
			}
			if e = grantCredits(ctx, tx, team, next.Sequence); e != nil {
				return e
			}
			if e = tx.SaveGrantSchedule(ctx, next); e != nil {
				return e
			}
		}
		if original.Disabled && !team.Disabled && at.Before(team.ExpiresAt) {
			schedule, e := tx.GrantSchedule(ctx)
			if e != nil {
				return e
			}
			next, e := schedule.AfterEnable(at)
			if e != nil {
				return e
			}
			if e = tx.SaveGrantSchedule(ctx, next); e != nil {
				return e
			}
		}
		team.Revision++
		if err = tx.SaveTeam(ctx, team, in.Revision); err != nil {
			return err
		}
		for _, item := range changes {
			if err = operatorAudit(ctx, tx, a, id, item.action, item.target, item.detail); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return TeamView{}, err
	}
	return s.Store.TeamView(ctx, id)
}
