package postgres

import (
	"context"
	"time"

	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	"github.com/jackc/pgx/v5"
)

func dailyExpirationEntry(account, operation string, balance int64, a d.Allocation) d.Entry {
	return d.Entry{
		ID: identity.New("credit"), OperationID: operation, AccountID: account,
		Kind: "expire", Amount: a.Daily, Balance: balance, Category: d.Gift,
		GiftAmount: a.Daily, DailyAmount: a.Daily, DailyExpiresAt: a.DailyExpiresAt,
		Source: "daily-gift-expiration", Subject: "system", Reason: "每日赠送到期回收",
	}
}

// 调用方已持账户锁。旧余额回收、新领取、扣款和退款使用同一账户锁，
// 多实例重复扫描不会重复写账；没有每日余额的账号不在扫描集合内。
func (s *Store) expireCreditWalletTx(ctx context.Context, tx pgx.Tx, account string, w d.Wallet, at time.Time) (d.Wallet, error) {
	next, expired := w.Expire(at)
	if expired.Total() == 0 {
		return w, nil
	}
	operation := "gift:expire:" + account + ":" + expired.DailyExpiresAt.UTC().Format(time.RFC3339Nano)
	if _, err := insertCreditTx(ctx, tx, dailyExpirationEntry(account, operation, next.Balance, expired)); err != nil {
		return d.Wallet{}, err
	}
	return next, saveCreditWalletTx(ctx, tx, account, next)
}

// ExpireDailyCredits 仅回收已发出且过期的份额，不发放任何积分。
// 每账户独立提交，失败原样返回；下次扫描继续核对尚未提交的账户。
func (s *Store) ExpireDailyCredits(ctx context.Context) error {
	for {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		var account string
		err = tx.QueryRow(ctx, `SELECT account_id FROM drama_catalog_v1.credit_accounts WHERE daily_balance>0 AND daily_expires_at<=statement_timestamp() ORDER BY daily_expires_at,account_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&account)
		if err == pgx.ErrNoRows {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return nil
		}
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return err
		}
		w, err := lockedCreditWalletTx(ctx, tx, account)
		var at time.Time
		if err == nil {
			err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at)
		}
		if err == nil {
			_, err = s.expireCreditWalletTx(ctx, tx, account, w, at)
		}
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
}
