package spaces

import (
	"context"
	n "drama-agent/internal/domain/notifications"
	d "drama-agent/internal/domain/spaces"
	"strconv"
	"time"
)

// Schedule 以独立团队事务发放合同积分、发送到期与余额用尽提醒。
func (s *Service) Schedule(ctx context.Context, at time.Time, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, invalid()
	}
	completed, err := s.runGrants(ctx, at, limit)
	if err != nil {
		return completed, err
	}
	teams, err := s.Store.DueTeamReminders(ctx, at, limit)
	if err != nil {
		return completed, err
	}
	for _, candidate := range teams {
		sent := 0
		err = s.Store.SpaceTx(ctx, candidate.ID, func(tx Tx) error {
			team, e := tx.Team(ctx)
			if e != nil {
				return e
			}
			if team.Disabled {
				return nil
			}
			key := strconv.FormatInt(team.ExpiresAt.UnixMicro(), 10)
			kinds := team.ReminderKinds(at)
			if team.Status(at) == d.Expired {
				balances, e := tx.CreditBalances(ctx, team.ID)
				if e != nil {
					return e
				}
				if balances.Wallet.Balance == 0 {
					kinds = append(kinds, "read_only")
				}
			}
			for _, kind := range kinds {
				created, e := tx.RecordReminder(ctx, kind, key)
				if e != nil {
					return e
				}
				if !created {
					continue
				}
				name := n.EscapeMarkdownText(team.Name)
				body := name + "：团队将在7天后到期，请联系运营续约。"
				if kind == "expires_1d" {
					body = name + "：团队将在明天到期，请联系运营续约。"
				}
				if kind == "expires_today" {
					body = name + "：团队今天到期，自动发放与邀请将停止，已有积分仍可使用。"
				}
				if kind == "read_only" {
					body = name + "：团队已到期且积分已用完，现已转为只读，请联系运营续约。"
				}
				if e = notifyManagers(ctx, tx, "team-reminder:"+team.ID+":"+kind+":"+key, "团队到期提醒", body, n.Link{Type: "team", SpaceID: team.ID}); e != nil {
					return e
				}
				sent++
			}
			return nil
		})
		if err != nil {
			return completed, err
		}
		completed += sent
	}
	return completed, nil
}
