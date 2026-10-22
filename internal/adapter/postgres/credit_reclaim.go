package postgres

import (
	"context"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	"github.com/jackc/pgx/v5"
	"time"
)

// creditReclaimTx 仅供支付退款事务调用。原发放、钱包及累计上限均在锁内核对，
// 与同事务的订单/退款状态一起提交；不增加通用 HTTP 或 CreditLedger 写入口。
func (s *Store) creditReclaimTx(ctx context.Context, tx pgx.Tx, c d.Change) (d.Entry, error) {
	if err := c.Validate("reclaim"); err != nil {
		return d.Entry{}, err
	}
	old, found, err := existingCreditTx(ctx, tx, "reclaim", c)
	if err != nil || found {
		return old, err
	}
	original, err := scanCredit(tx.QueryRow(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE id=$1 FOR UPDATE`, c.OriginalAdditionID))
	if err != nil {
		return d.Entry{}, err
	}
	if original.Kind != "add" || (original.Category != d.Recharge && original.Category != d.Gift) || original.DailyAmount != 0 {
		return d.Entry{}, fault.ErrInvalid
	}
	var reclaimed int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(sum(amount),0) FROM drama_catalog_v1.credit_ledger WHERE original_addition_id=$1 AND kind='reclaim'`, original.ID).Scan(&reclaimed); err != nil {
		return d.Entry{}, err
	}
	if reclaimed < 0 || reclaimed > original.Amount || c.Amount > original.Amount-reclaimed {
		return d.Entry{}, fault.New("CREDIT_RECLAIM_EXCEEDED", "累计扣回超过原发放")
	}
	wallet, err := lockedCreditWalletTx(ctx, tx, original.AccountID)
	if err != nil {
		return d.Entry{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return d.Entry{}, err
	}
	wallet, err = s.expireCreditWalletTx(ctx, tx, original.AccountID, wallet, now)
	if err != nil {
		return d.Entry{}, err
	}
	allocated, err := allocatedCreditsTx(ctx, tx, original.AccountID)
	if err != nil {
		return d.Entry{}, err
	}
	if allocated > 0 && wallet.Balance-allocated < c.Amount {
		return d.Entry{}, fault.New("CREDIT_UNALLOCATED_INSUFFICIENT", "未分配积分不足")
	}
	wallet, allocation, err := wallet.Reclaim(original.Category, c.Amount)
	if err != nil {
		return d.Entry{}, err
	}
	receipt, err := insertCreditTx(ctx, tx, d.Entry{
		ID: identity.New("credit"), OperationID: c.OperationID, AccountID: original.AccountID,
		Kind: "reclaim", Amount: c.Amount, Balance: wallet.Balance, Category: allocation.Category(),
		RechargeAmount: allocation.Recharge, GiftAmount: allocation.Gift,
		Source: original.Source, Subject: c.Subject, Reason: c.Reason, OriginalAdditionID: original.ID,
	})
	if err != nil {
		return d.Entry{}, err
	}
	return receipt, saveCreditWalletTx(ctx, tx, original.AccountID, wallet)
}
