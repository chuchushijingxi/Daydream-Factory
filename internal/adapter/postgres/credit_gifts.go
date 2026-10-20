package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	c "drama-agent/internal/application/credits"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"github.com/jackc/pgx/v5"
)

func scanGiftPolicy(row pgx.Row) (c.GiftPolicy, error) {
	v := c.GiftPolicy{Timezone: c.GiftTimezone}
	err := row.Scan(&v.Revision, &v.RegistrationGift, &v.DailyGift)
	return v, err
}

func (s *Store) CreditGiftPolicy(ctx context.Context) (c.GiftPolicy, error) {
	v, err := scanGiftPolicy(s.pool.QueryRow(ctx, `SELECT revision,registration_gift,daily_gift FROM drama_catalog_v1.credit_gift_policy WHERE id`))
	if err != nil {
		return v, fault.New("CREDIT_POLICY_UNAVAILABLE", "无法读取积分赠送设置")
	}
	return v, nil
}

func (s *Store) SaveCreditGiftPolicy(ctx context.Context, in c.GiftPolicyInput) (c.GiftPolicy, error) {
	if in.Revision >= uint64(d.MaxAmount) || in.RegistrationGift < 0 || in.RegistrationGift > d.MaxAmount || in.DailyGift < 0 || in.DailyGift > d.MaxAmount {
		return c.GiftPolicy{}, fault.New("CREDIT_POLICY_INVALID", "赠送额度无效")
	}
	v, err := scanGiftPolicy(s.pool.QueryRow(ctx, `UPDATE drama_catalog_v1.credit_gift_policy SET revision=revision+1,registration_gift=$2,daily_gift=$3 WHERE id AND revision=$1 RETURNING revision,registration_gift,daily_gift`, in.Revision, in.RegistrationGift, in.DailyGift))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, fault.New("CREDIT_POLICY_CONFLICT", "赠送设置已变化，请重新读取")
	}
	if err != nil {
		return v, fault.New("CREDIT_POLICY_SAVE_UNKNOWN", "设置保存结果不明，请重新读取")
	}
	return v, nil
}

func giftPolicyTx(ctx context.Context, tx pgx.Tx) (c.GiftPolicy, error) {
	return scanGiftPolicy(tx.QueryRow(ctx, `SELECT revision,registration_gift,daily_gift FROM drama_catalog_v1.credit_gift_policy WHERE id FOR SHARE`))
}

// 注册保留一次性资格；每日只在实际发放正数时调用。
// 旧版每日0记录可在原用户锁内升级，账本与资格必须一起提交或回滚。
func (s *Store) grantCreditGiftTx(ctx context.Context, tx pgx.Tx, user, kind, period string, amount int64, revision uint64) error {
	op := "gift:" + kind + ":" + user + ":" + period
	if amount > 0 {
		change := d.Change{AccountID: user, Amount: amount, OperationID: op, Source: kind + "-gift", Subject: user, Reason: kind + "-gift", Category: d.Gift}
		if kind == "daily" {
			day, err := time.Parse(time.RFC3339, period+"T00:00:00+08:00")
			if err != nil {
				return err
			}
			expires := day.AddDate(0, 0, 1)
			change.DailyExpiresAt = &expires
		}
		if _, err := s.creditChangeTx(ctx, tx, "add", change); err != nil {
			return err
		}
	}
	result, err := tx.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_gift_claims AS claims(user_id,kind,period,amount,policy_revision,operation_id)
		VALUES($1,$2,$3,$4,$5,CASE WHEN $4::bigint>0 THEN $6::text ELSE NULL END)
		ON CONFLICT(user_id,kind,period) DO UPDATE
		SET amount=EXCLUDED.amount,policy_revision=EXCLUDED.policy_revision,operation_id=EXCLUDED.operation_id,created_at=clock_timestamp()
		WHERE claims.kind='daily' AND claims.amount=0 AND claims.operation_id IS NULL AND EXCLUDED.amount>0`, user, kind, period, amount, revision, op)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("record %s gift for user %s period %s: eligibility already consumed", kind, user, period)
	}
	return nil
}

func (s *Store) ClaimDailyCreditGift(ctx context.Context, user string) (c.DailyGift, error) {
	if user == "" {
		return c.DailyGift{}, fault.New("AUTH_REQUIRED", "请先登录")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return c.DailyGift{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	policy, err := giftPolicyTx(ctx, tx)
	if err != nil {
		return c.DailyGift{}, err
	}
	var enabled bool
	// The user lock serializes claims and a concurrent disable. Never rely on
	// a browser-provided date or a balance query to establish eligibility.
	err = tx.QueryRow(ctx, `SELECT enabled FROM drama_catalog_v1.users WHERE id=$1 FOR UPDATE`, user).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.DailyGift{}, fault.ErrNotFound
	}
	if err != nil {
		return c.DailyGift{}, err
	}
	if !enabled {
		return c.DailyGift{}, fault.New("ACCOUNT_DISABLED", "账号已禁用")
	}
	var accepted time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&accepted); err != nil {
		return c.DailyGift{}, err
	}
	out := c.DailyGift{Date: c.GiftDate(accepted)}
	err = tx.QueryRow(ctx, `SELECT amount FROM drama_catalog_v1.credit_gift_claims WHERE user_id=$1 AND kind='daily' AND period=$2`, user, out.Date).Scan(&out.Amount)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return c.DailyGift{}, err
	}
	if err == nil && out.Amount > 0 {
		out.Claimed = true
		out.AlreadyClaimed = true
	} else {
		out.Amount = policy.DailyGift
		if out.Amount > 0 {
			if err = s.grantCreditGiftTx(ctx, tx, user, "daily", out.Date, out.Amount, policy.Revision); err != nil {
				return c.DailyGift{}, err
			}
			out.Claimed = true
		}
	}
	if out.Claimed {
		midnight, err := time.Parse(time.RFC3339, out.Date+"T00:00:00+08:00")
		if err != nil {
			return c.DailyGift{}, err
		}
		out.CacheTTL = midnight.AddDate(0, 0, 1).Sub(accepted)
	}
	if err = tx.Commit(ctx); err != nil {
		return c.DailyGift{}, fault.New("CREDIT_COMMIT_UNKNOWN", "赠送提交结果不明，请按当天领取接口回读")
	}
	return out, nil
}
