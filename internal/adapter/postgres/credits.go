package postgres

import (
	"context"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

const creditColumns = `id,operation_id,account_id,kind,amount,balance,source,subject,reason,coalesce(original_deduction_id,''),created_at,category,recharge_amount,gift_amount,legacy_amount,daily_amount,daily_expires_at,coalesce(holder_id,''),coalesce(original_addition_id,'')`

func scanCredit(row pgx.Row) (d.Entry, error) {
	var e d.Entry
	err := row.Scan(&e.ID, &e.OperationID, &e.AccountID, &e.Kind, &e.Amount, &e.Balance, &e.Source, &e.Subject, &e.Reason, &e.OriginalDeductionID, &e.CreatedAt, &e.Category, &e.RechargeAmount, &e.GiftAmount, &e.LegacyAmount, &e.DailyAmount, &e.DailyExpiresAt, &e.HolderID, &e.OriginalAdditionID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return e, err
}
func (s *Store) Add(ctx context.Context, c d.Change) (d.Entry, error) {
	return s.creditChange(ctx, "add", c)
}
func (s *Store) Deduct(ctx context.Context, c d.Change) (d.Entry, error) {
	return s.creditChange(ctx, "deduct", c)
}
func (s *Store) Refund(ctx context.Context, c d.Change) (d.Entry, error) {
	return s.creditChange(ctx, "refund", c)
}
func (s *Store) CreditOperation(ctx context.Context, id string) (d.Entry, error) {
	return scanCredit(s.pool.QueryRow(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE operation_id=$1`, id))
}
func (s *Store) CreditOperations(ctx context.Context, ids []string) (map[string]d.Entry, error) {
	out := make(map[string]d.Entry, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE operation_id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		entry, err := scanCredit(rows)
		if err != nil {
			return nil, err
		}
		out[entry.OperationID] = entry
	}
	return out, rows.Err()
}
func (s *Store) creditChange(ctx context.Context, kind string, c d.Change) (d.Entry, error) {
	if e := c.Validate(kind); e != nil {
		return d.Entry{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return d.Entry{}, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	receipt, e := s.creditChangeTx(ctx, tx, kind, c)
	if e != nil {
		return d.Entry{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return d.Entry{}, fault.New("CREDIT_COMMIT_UNKNOWN", "账务提交结果不明，请按原编号回读")
	}
	return receipt, nil
}

// creditChangeTx 是 Add/Deduct/Refund 唯一变更实现，可加入调用方事务；不自行提交。
func (s *Store) creditChangeTx(ctx context.Context, tx pgx.Tx, kind string, c d.Change) (d.Entry, error) {
	if kind != "add" && kind != "deduct" && kind != "refund" {
		return d.Entry{}, fault.ErrInvalid
	}
	if e := c.Validate(kind); e != nil {
		return d.Entry{}, e
	}
	old, found, e := existingCreditTx(ctx, tx, kind, c)
	if e != nil || found {
		return old, e
	}
	account, source, holder := c.AccountID, c.Source, c.HolderID
	var allocation d.Allocation
	if kind == "refund" {
		original, err := scanCredit(tx.QueryRow(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE id=$1 FOR UPDATE`, c.OriginalDeductionID))
		if err != nil {
			return d.Entry{}, err
		}
		if original.HolderID != c.HolderID {
			return d.Entry{}, fault.New("CREDIT_IDEMPOTENCY_CONFLICT", "退款持有人与原扣款不一致")
		}
		var returned d.Allocation
		err = tx.QueryRow(ctx, `SELECT coalesce(sum(recharge_amount),0),coalesce(sum(gift_amount),0),coalesce(sum(legacy_amount),0),coalesce(sum(daily_amount),0) FROM drama_catalog_v1.credit_ledger WHERE original_deduction_id=$1 AND kind='refund'`, original.ID).Scan(&returned.Recharge, &returned.Gift, &returned.Legacy, &returned.Daily)
		if err != nil {
			return d.Entry{}, err
		}
		allocation, err = original.RefundAllocation(c.Amount, returned)
		if err != nil {
			return d.Entry{}, err
		}
		account, source, holder = original.AccountID, original.Source, original.HolderID
	}
	if _, e = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_accounts(account_id) VALUES($1) ON CONFLICT DO NOTHING`, account); e != nil {
		return d.Entry{}, e
	}
	wallet, e := lockedCreditWalletTx(ctx, tx, account)
	if e != nil {
		return d.Entry{}, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return d.Entry{}, e
	}
	wallet, e = s.expireCreditWalletTx(ctx, tx, account, wallet, now)
	if e != nil {
		return d.Entry{}, e
	}
	expiredRefund := d.Allocation{}
	if kind == "deduct" {
		if holder != "" {
			held, err := holderCreditsTx(ctx, tx, account, holder)
			if err != nil {
				return d.Entry{}, err
			}
			held, err = held.Deduct(c.Amount)
			if err != nil {
				return d.Entry{}, err
			}
			if _, err = tx.Exec(ctx, `UPDATE drama_catalog_v1.credit_allocations SET balance=$3 WHERE account_id=$1 AND holder_id=$2`, account, holder, held.Balance); err != nil {
				return d.Entry{}, err
			}
		} else {
			allocated, err := allocatedCreditsTx(ctx, tx, account)
			if err != nil {
				return d.Entry{}, err
			}
			if allocated > 0 && wallet.Balance-allocated < c.Amount {
				return d.Entry{}, fault.New("CREDIT_UNALLOCATED_INSUFFICIENT", "未分配积分不足")
			}
		}
		wallet, allocation, e = wallet.Deduct(c.Amount)
	} else {
		if kind == "add" {
			allocation, e = d.Funding(c.Amount, c.Category)
			if e != nil {
				return d.Entry{}, e
			}
			if c.DailyExpiresAt != nil {
				if !now.Before(*c.DailyExpiresAt) {
					return d.Entry{}, fault.ErrInvalid
				}
				allocation.Daily, allocation.DailyExpiresAt = c.Amount, c.DailyExpiresAt
			}
		}
		wallet, expiredRefund, e = wallet.Restore(allocation, now)
	}
	if e != nil {
		return d.Entry{}, e
	}
	if kind == "refund" && holder != "" {
		held, err := holderCreditsTx(ctx, tx, account, holder)
		if err != nil {
			return d.Entry{}, err
		}
		if restored := c.Amount - expiredRefund.Total(); restored > 0 {
			held, err = held.Refund(restored)
			if err != nil {
				return d.Entry{}, err
			}
		}
		if _, e = tx.Exec(ctx, `UPDATE drama_catalog_v1.credit_allocations SET balance=$3 WHERE account_id=$1 AND holder_id=$2`, account, holder, held.Balance); e != nil {
			return d.Entry{}, e
		}
	}
	receipt := d.Entry{
		ID: identity.New("credit"), OperationID: c.OperationID, AccountID: account, HolderID: holder,
		Kind: kind, Amount: c.Amount, Balance: wallet.Balance,
		Source: source, Subject: c.Subject, Reason: c.Reason, OriginalDeductionID: c.OriginalDeductionID,
		Category: allocation.Category(), RechargeAmount: allocation.Recharge, GiftAmount: allocation.Gift,
		LegacyAmount: allocation.Legacy, DailyAmount: allocation.Daily, DailyExpiresAt: allocation.DailyExpiresAt,
	}
	receipt, e = insertCreditTx(ctx, tx, receipt)
	if e != nil {
		return d.Entry{}, e
	}
	if expiredRefund.Total() > 0 {
		_, e = insertCreditTx(ctx, tx, dailyExpirationEntry(account, "gift:expire-refund:"+c.OperationID, wallet.Balance, expiredRefund))
		if e != nil {
			return d.Entry{}, e
		}
	}
	return receipt, saveCreditWalletTx(ctx, tx, account, wallet)
}

// 同一操作编号共享锁和语义回读；支付扣回也沿这一个入口防止幂等规则漂移。
func existingCreditTx(ctx context.Context, tx pgx.Tx, kind string, c d.Change) (d.Entry, bool, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('drama-agent:credit:'||$1,0))`, c.OperationID); err != nil {
		return d.Entry{}, false, err
	}
	old, err := scanCredit(tx.QueryRow(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE operation_id=$1`, c.OperationID))
	if err == nil {
		if !old.Same(kind, c) {
			return d.Entry{}, false, fault.New("CREDIT_IDEMPOTENCY_CONFLICT", "原账务编号对应不同内容")
		}
		return old, true, nil
	}
	if errors.Is(err, fault.ErrNotFound) {
		return d.Entry{}, false, nil
	}
	return d.Entry{}, false, err
}

const creditWalletColumns = `balance,recharge_balance,gift_balance,legacy_balance,daily_balance,daily_expires_at`

func scanCreditWallet(row pgx.Row) (d.Wallet, error) {
	var w d.Wallet
	err := row.Scan(&w.Balance, &w.RechargeBalance, &w.GiftBalance, &w.LegacyBalance, &w.DailyBalance, &w.DailyExpiresAt)
	return w, err
}
func lockedCreditWalletTx(ctx context.Context, tx pgx.Tx, account string) (d.Wallet, error) {
	return scanCreditWallet(tx.QueryRow(ctx, `SELECT `+creditWalletColumns+` FROM drama_catalog_v1.credit_accounts WHERE account_id=$1 FOR UPDATE`, account))
}
func saveCreditWalletTx(ctx context.Context, tx pgx.Tx, account string, w d.Wallet) error {
	_, err := tx.Exec(ctx, `UPDATE drama_catalog_v1.credit_accounts SET balance=$2,recharge_balance=$3,gift_balance=$4,legacy_balance=$5,daily_balance=$6,daily_expires_at=$7 WHERE account_id=$1`, account, w.Balance, w.RechargeBalance, w.GiftBalance, w.LegacyBalance, w.DailyBalance, w.DailyExpiresAt)
	return err
}
func insertCreditTx(ctx context.Context, tx pgx.Tx, e d.Entry) (d.Entry, error) {
	var original, addition any
	if e.OriginalDeductionID != "" {
		original = e.OriginalDeductionID
	}
	if e.OriginalAdditionID != "" {
		addition = e.OriginalAdditionID
	}
	return scanCredit(tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.credit_ledger(id,operation_id,account_id,kind,amount,balance,source,subject,reason,original_deduction_id,category,recharge_amount,gift_amount,legacy_amount,daily_amount,daily_expires_at,holder_id,original_addition_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NULLIF($17,''),$18,clock_timestamp()) RETURNING `+creditColumns, e.ID, e.OperationID, e.AccountID, e.Kind, e.Amount, e.Balance, e.Source, e.Subject, e.Reason, original, e.Category, e.RechargeAmount, e.GiftAmount, e.LegacyAmount, e.DailyAmount, e.DailyExpiresAt, e.HolderID, addition))
}

func (s *Store) Balance(ctx context.Context, id string) (int64, error) {
	if id == "" {
		return 0, fault.ErrInvalid
	}
	w, err := s.CreditWallet(ctx, id)
	return w.Balance, err
}
func (s *Store) CreditEntries(ctx context.Context, id string, page, size int) ([]d.Entry, int64, error) {
	if id == "" || page < 1 || size < 1 || size > 100 {
		return nil, 0, fault.ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var count int64
	e = tx.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1`, id).Scan(&count)
	if e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, id, size, (page-1)*size)
	if e != nil {
		return nil, 0, e
	}
	out := []d.Entry{}
	for rows.Next() {
		v, err := scanCredit(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, 0, e
	}
	return out, count, tx.Commit(ctx)
}
