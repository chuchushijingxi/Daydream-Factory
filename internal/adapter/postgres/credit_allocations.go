package postgres

import (
	"context"
	"errors"
	"time"

	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	"github.com/jackc/pgx/v5"
)

var _ ports.CreditAllocations = (*Store)(nil)
var _ ports.CreditAllocationQueries = (*Store)(nil)

const allocationEventColumns = `id,operation_id,account_id,holder_id,kind,actor_id,amount,balance,created_at,requested_amount,requested_all`

func scanAllocationEvent(row rowScanner) (d.AllocationEvent, error) {
	var v d.AllocationEvent
	err := row.Scan(&v.ID, &v.OperationID, &v.AccountID, &v.HolderID, &v.Kind, &v.ActorID, &v.Amount, &v.Balance, &v.CreatedAt, &v.RequestedAmount, &v.All)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return v, err
}
func (s *Store) Allocate(ctx context.Context, c d.AllocationChange) (d.AllocationEvent, error) {
	return s.allocationChange(ctx, "allocate", c)
}
func (s *Store) Reclaim(ctx context.Context, c d.AllocationChange) (d.AllocationEvent, error) {
	return s.allocationChange(ctx, "reclaim", c)
}
func (s *Store) Release(ctx context.Context, c d.AllocationChange) (d.AllocationEvent, error) {
	return s.allocationChange(ctx, "release", c)
}
func (s *Store) allocationChange(ctx context.Context, kind string, c d.AllocationChange) (d.AllocationEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return d.AllocationEvent{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	v, err := s.allocationChangeTx(ctx, tx, kind, c)
	if err != nil {
		return v, err
	}
	if err = tx.Commit(ctx); err != nil {
		return d.AllocationEvent{}, fault.New("CREDIT_COMMIT_UNKNOWN", "分配提交结果不明，请按原编号回读")
	}
	return v, nil
}
func allocatedCreditsTx(ctx context.Context, tx pgx.Tx, account string) (int64, error) {
	var v int64
	err := tx.QueryRow(ctx, `SELECT coalesce(sum(balance),0)::bigint FROM drama_catalog_v1.credit_allocations WHERE account_id=$1 AND open`, account).Scan(&v)
	return v, err
}
func holderCreditsTx(ctx context.Context, tx pgx.Tx, account, holder string) (d.HolderAllocation, error) {
	v := d.HolderAllocation{AccountID: account, HolderID: holder}
	err := tx.QueryRow(ctx, `SELECT balance,open FROM drama_catalog_v1.credit_allocations WHERE account_id=$1 AND holder_id=$2`, account, holder).Scan(&v.Balance, &v.Open)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return v, err
}
func ensureHolderOpenTx(ctx context.Context, tx pgx.Tx, account, holder string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_accounts(account_id) VALUES($1) ON CONFLICT DO NOTHING`, account); err != nil {
		return err
	}
	if _, err := lockedCreditWalletTx(ctx, tx, account); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_allocations(account_id,holder_id,balance,open) VALUES($1,$2,0,true) ON CONFLICT(account_id,holder_id) DO UPDATE SET open=true WHERE NOT credit_allocations.open AND credit_allocations.balance=0`, account, holder)
	return err
}
func (s *Store) allocationChangeTx(ctx context.Context, tx pgx.Tx, kind string, c d.AllocationChange) (d.AllocationEvent, error) {
	if err := c.Validate(kind); err != nil {
		return d.AllocationEvent{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('drama-agent:credit:'||$1,0))`, c.OperationID); err != nil {
		return d.AllocationEvent{}, err
	}
	var old d.AllocationEvent
	var requested int64
	var all bool
	err := tx.QueryRow(ctx, `SELECT `+allocationEventColumns+` FROM drama_catalog_v1.credit_allocation_events WHERE operation_id=$1`, c.OperationID).Scan(&old.ID, &old.OperationID, &old.AccountID, &old.HolderID, &old.Kind, &old.ActorID, &old.Amount, &old.Balance, &old.CreatedAt, &requested, &all)
	if err == nil {
		old.RequestedAmount, old.All, old.Replayed = requested, all, true
		if old.AccountID != c.AccountID || old.HolderID != c.HolderID || old.Kind != kind || old.ActorID != c.ActorID || requested != c.Amount || all != c.All {
			return old, fault.New("CREDIT_IDEMPOTENCY_CONFLICT", "原分配编号对应不同内容")
		}
		return old, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return old, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_accounts(account_id) VALUES($1) ON CONFLICT DO NOTHING`, c.AccountID); err != nil {
		return old, err
	}
	wallet, err := lockedCreditWalletTx(ctx, tx, c.AccountID)
	if err != nil {
		return old, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return old, err
	}
	wallet, err = s.expireCreditWalletTx(ctx, tx, c.AccountID, wallet, now)
	if err != nil {
		return old, err
	}
	held, err := holderCreditsTx(ctx, tx, c.AccountID, c.HolderID)
	if err != nil {
		return old, err
	}
	total, err := allocatedCreditsTx(ctx, tx, c.AccountID)
	if err != nil {
		return old, err
	}
	amount := c.Amount
	unallocated, err := d.Unallocated(wallet.Balance, total)
	if err != nil {
		return old, err
	}
	switch kind {
	case "allocate":
		held, err = held.Allocate(unallocated, c.Amount)
	case "reclaim":
		held, amount, err = held.Reclaim(c.Amount, c.All)
	case "release":
		held, amount, err = held.Release()
	}
	if err != nil {
		return old, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_allocations(account_id,holder_id,balance,open) VALUES($1,$2,$3,$4) ON CONFLICT(account_id,holder_id) DO UPDATE SET balance=EXCLUDED.balance,open=EXCLUDED.open`, c.AccountID, c.HolderID, held.Balance, held.Open); err != nil {
		return old, err
	}
	return scanAllocationEvent(tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.credit_allocation_events(id,operation_id,account_id,holder_id,kind,actor_id,amount,balance,requested_amount,requested_all) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+allocationEventColumns, identity.New("allocation"), c.OperationID, c.AccountID, c.HolderID, kind, c.ActorID, amount, held.Balance, c.Amount, c.All))
}
func (s *Store) HolderCredits(ctx context.Context, account, holder string) (d.HolderAllocation, error) {
	v := d.HolderAllocation{AccountID: account, HolderID: holder}
	err := s.pool.QueryRow(ctx, `SELECT balance,open FROM drama_catalog_v1.credit_allocations WHERE account_id=$1 AND holder_id=$2`, account, holder).Scan(&v.Balance, &v.Open)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return v, err
}
func (s *Store) AllocationHistory(ctx context.Context, account, holder string, f d.HistoryFilter) (ports.Page[d.AllocationEvent], error) {
	out := ports.Page[d.AllocationEvent]{Items: []d.AllocationEvent{}, Page: f.Page, PageSize: f.PageSize}
	if err := f.Validate(); err != nil {
		return out, err
	}
	if f.Category != "all" || f.Direction != "all" {
		return out, nil
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	const where = ` WHERE account_id=$1 AND ($2='' OR holder_id=$2) AND ($3::timestamptz IS NULL OR created_at>=$3) AND ($4::timestamptz IS NULL OR created_at<$4)`
	args := []any{account, holder, f.Start, f.End}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_allocation_events`+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT `+allocationEventColumns+` FROM drama_catalog_v1.credit_allocation_events`+where+` ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6`, append(args, f.PageSize, (f.Page-1)*f.PageSize)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanAllocationEvent(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) RecentAllocationEvents(ctx context.Context, account, holder string) ([]d.AllocationEvent, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+allocationEventColumns+` FROM drama_catalog_v1.credit_allocation_events WHERE account_id=$1 AND holder_id=$2 ORDER BY created_at DESC,id DESC LIMIT 5`, account, holder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []d.AllocationEvent{}
	for rows.Next() {
		v, e := scanAllocationEvent(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) RecentHolderCreditEntries(ctx context.Context, account, holder string) ([]d.Entry, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND holder_id=$2 ORDER BY created_at DESC,id DESC LIMIT 5`, account, holder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []d.Entry{}
	for rows.Next() {
		v, e := scanCredit(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
