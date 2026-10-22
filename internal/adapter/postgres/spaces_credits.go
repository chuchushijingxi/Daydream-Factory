package postgres

import (
	"context"
	"errors"
	"time"

	app "drama-agent/internal/application/spaces"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/spaces"
	"github.com/jackc/pgx/v5"
)

func (t *spaceTx) EnsureCreditAccount(ctx context.Context, account string) error {
	if account != t.id {
		return fault.ErrInvalid
	}
	_, err := t.tx.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_accounts(account_id) VALUES($1) ON CONFLICT DO NOTHING`, account)
	return err
}
func (t *spaceTx) AddCredits(ctx context.Context, in c.Change) (c.Entry, error) {
	if in.AccountID != t.id {
		return c.Entry{}, fault.ErrInvalid
	}
	return t.store.creditChangeTx(ctx, t.tx, "add", in)
}
func (t *spaceTx) CreditBalances(ctx context.Context, account string) (c.Balances, error) {
	if account != t.id {
		return c.Balances{}, fault.ErrInvalid
	}
	if err := t.EnsureCreditAccount(ctx, account); err != nil {
		return c.Balances{}, err
	}
	w, err := lockedCreditWalletTx(ctx, t.tx, account)
	if err != nil {
		return c.Balances{}, err
	}
	var at time.Time
	if err = t.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return c.Balances{}, err
	}
	w, err = t.store.expireCreditWalletTx(ctx, t.tx, account, w, at)
	if err != nil {
		return c.Balances{}, err
	}
	allocated, err := allocatedCreditsTx(ctx, t.tx, account)
	if err != nil {
		return c.Balances{}, err
	}
	v := c.Balances{Wallet: w, Allocated: allocated}
	_, err = v.Unallocated()
	return v, err
}
func (t *spaceTx) AllocateCredits(ctx context.Context, in c.AllocationChange) (c.AllocationEvent, error) {
	if in.AccountID != t.id {
		return c.AllocationEvent{}, fault.ErrInvalid
	}
	return t.store.allocationChangeTx(ctx, t.tx, "allocate", in)
}
func (t *spaceTx) ReclaimCredits(ctx context.Context, in c.AllocationChange) (c.AllocationEvent, error) {
	if in.AccountID != t.id {
		return c.AllocationEvent{}, fault.ErrInvalid
	}
	return t.store.allocationChangeTx(ctx, t.tx, "reclaim", in)
}
func (t *spaceTx) ReleaseCredits(ctx context.Context, in c.AllocationChange) (c.AllocationEvent, error) {
	if in.AccountID != t.id {
		return c.AllocationEvent{}, fault.ErrInvalid
	}
	return t.store.allocationChangeTx(ctx, t.tx, "release", in)
}
func (t *spaceTx) GrantSchedule(ctx context.Context) (d.GrantSchedule, error) {
	var v d.GrantSchedule
	err := t.tx.QueryRow(ctx, `SELECT next_grant_at,grant_sequence FROM drama_catalog_v1.teams WHERE space_id=$1`, t.id).Scan(&v.NextAt, &v.Sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return v, err
}
func (t *spaceTx) SaveGrantSchedule(ctx context.Context, v d.GrantSchedule) error {
	if v.NextAt.IsZero() || v.Sequence == 0 || v.Sequence > uint64(c.MaxAmount) {
		return fault.ErrInvalid
	}
	tag, err := t.tx.Exec(ctx, `UPDATE drama_catalog_v1.teams SET next_grant_at=$2,grant_sequence=$3 WHERE space_id=$1`, t.id, v.NextAt, v.Sequence)
	if err == nil && tag.RowsAffected() != 1 {
		return fault.ErrNotFound
	}
	return err
}

// 每次锁定一个候选并独立提交；其余实例跳过持锁团队，恢复扫描仍用持久周期号。
func (s *Store) RunDueTeamGrants(ctx context.Context, at time.Time, limit int, apply func(app.Tx) error) (int, error) {
	if limit < 1 || limit > 1000 || apply == nil {
		return 0, fault.ErrInvalid
	}
	processed := 0
	for processed < limit {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return processed, err
		}
		var id string
		err = tx.QueryRow(ctx, `SELECT space_id FROM drama_catalog_v1.teams WHERE NOT disabled AND expires_at>$1 AND next_grant_at<=$1 ORDER BY next_grant_at,space_id LIMIT 1 FOR UPDATE SKIP LOCKED`, at).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return processed, nil
		}
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return processed, err
		}
		if err = apply(&spaceTx{store: s, tx: tx, id: id}); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return processed, spaceWriteError(err)
		}
		if err = tx.Commit(ctx); err != nil {
			return processed, fault.New("CREDIT_COMMIT_UNKNOWN", "团队发放结果未确认，请按原周期回读")
		}
		processed++
	}
	return processed, nil
}
func (s *Store) CreditDiscount(ctx context.Context, spaceID, modelID string) (int, error) {
	var percent int
	err := s.pool.QueryRow(ctx, `SELECT percent FROM drama_catalog_v1.team_model_discounts WHERE space_id=$1 AND model_instance_id=$2`, spaceID, modelID).Scan(&percent)
	if errors.Is(err, pgx.ErrNoRows) {
		return 100, nil
	}
	return percent, err
}

func (t *spaceTx) HolderCredits(ctx context.Context, account, holder string) (c.HolderAllocation, error) {
	if account != t.id {
		return c.HolderAllocation{}, fault.ErrInvalid
	}
	if _, err := lockedCreditWalletTx(ctx, t.tx, account); err != nil {
		return c.HolderAllocation{}, err
	}
	return holderCreditsTx(ctx, t.tx, account, holder)
}
