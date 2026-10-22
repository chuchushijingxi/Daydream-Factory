package spaces

import (
	"context"
	c "drama-agent/internal/domain/credits"
	d "drama-agent/internal/domain/spaces"
	"strconv"
	"time"
)

func grantCredits(ctx context.Context, tx Tx, team d.Team, sequence uint64) error {
	if team.GrantAmount == 0 {
		return nil
	}
	_, err := tx.AddCredits(ctx, c.Change{AccountID: team.ID, Amount: team.GrantAmount, Category: c.Gift, OperationID: "team-grant:" + team.ID + ":" + strconv.FormatUint(sequence, 10), Source: "team-grant:" + team.ID, Subject: "system", Reason: "团队合同积分发放"})
	return err
}
func (s *Service) runGrants(ctx context.Context, at time.Time, limit int) (int, error) {
	return s.Store.RunDueTeamGrants(ctx, at, limit, func(tx Tx) error {
		team, err := tx.Team(ctx)
		if err != nil {
			return err
		}
		if team.Status(at) != d.Active {
			return nil
		}
		schedule, err := tx.GrantSchedule(ctx)
		if err != nil {
			return err
		}
		if schedule.NextAt.After(at) {
			return nil
		}
		next, err := schedule.AfterGrant()
		if err != nil {
			return err
		}
		if err = grantCredits(ctx, tx, team, next.Sequence); err != nil {
			return err
		}
		return tx.SaveGrantSchedule(ctx, next)
	})
}
