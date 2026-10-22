package postgres

import (
	"context"
	"errors"
	"time"

	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"

	"github.com/jackc/pgx/v5"
)

// UnallocatedTx 与积分变更使用同一账户行锁，调用方负责提交或回滚。
// 未分配=钱包可用余额减全部open持有人余额；个人账户没有分配，保持原值。
func (s *Store) UnallocatedTx(ctx context.Context, tx pgx.Tx, account string) (int64, error) {
	if account == "" {
		return 0, fault.ErrInvalid
	}
	if _, err := tx.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_accounts(account_id) VALUES($1) ON CONFLICT DO NOTHING`, account); err != nil {
		return 0, err
	}
	wallet, err := lockedCreditWalletTx(ctx, tx, account)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fault.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if wallet.DailyExpiresAt != nil {
		// 到期回收与退款上限处于同一事务，避免把过期每日份额算入余额。
		var at time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
			return 0, err
		}
		wallet, err = s.expireCreditWalletTx(ctx, tx, account, wallet, at)
		if err != nil {
			return 0, err
		}
	}
	allocated, err := allocatedCreditsTx(ctx, tx, account)
	if err != nil {
		return 0, err
	}
	return d.Unallocated(wallet.Balance, allocated)
}
