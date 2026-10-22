package postgres

import (
	"context"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func creditQueryError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22003" {
		return fault.New("CREDIT_TOTAL_OVERFLOW", "积分统计超出安全整数范围，请缩小查询时间范围")
	}
	return err
}

func creditWalletTx(ctx context.Context, tx pgx.Tx, id string) (d.Wallet, error) {
	if id == "" {
		return d.Wallet{}, fault.ErrInvalid
	}
	v, err := spendableCreditWallet(tx.QueryRow(ctx, `SELECT `+creditWalletColumns+`,clock_timestamp() FROM drama_catalog_v1.credit_accounts WHERE account_id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return d.Wallet{}, nil
	}
	return v, err
}

func (s *Store) CreditWallet(ctx context.Context, id string) (d.Wallet, error) {
	if id == "" {
		return d.Wallet{}, fault.ErrInvalid
	}
	v, err := spendableCreditWallet(s.pool.QueryRow(ctx, `SELECT `+creditWalletColumns+`,clock_timestamp() FROM drama_catalog_v1.credit_accounts WHERE account_id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return d.Wallet{}, nil
	}
	return v, err
}

// 顶栏只展示最近流水，不为五条预览核算完整消费分析或读取业务正文。
func (s *Store) RecentCreditEntries(ctx context.Context, id string) ([]d.Entry, error) {
	if id == "" {
		return nil, fault.ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 ORDER BY created_at DESC,id DESC LIMIT 5`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []d.Entry{}
	for rows.Next() {
		entry, err := scanCredit(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// GET 保持只读；即使回收 worker 暂停，过期份额也不计入可用余额。
func spendableCreditWallet(row pgx.Row) (d.Wallet, error) {
	var w d.Wallet
	var at time.Time
	err := row.Scan(&w.Balance, &w.RechargeBalance, &w.GiftBalance, &w.LegacyBalance, &w.DailyBalance, &w.DailyExpiresAt, &at)
	if err != nil {
		return w, err
	}
	w, _ = w.Expire(at)
	return w, nil
}

// This adapter-only projection joins immutable ledger receipts to the existing
// persisted business completion facts. The money core does not know Runs/tasks.
// All source labels are fixed; no project title, content or identifier is sent.
// 相关项目只展开一次credit/status；不能逐扣款复制含完整模型调用的Run。
const creditHistoryProjection = `WITH entries AS MATERIALIZED (
 SELECT l.*,CASE $2 WHEN 'recharge' THEN recharge_amount WHEN 'gift' THEN gift_amount WHEN 'legacy' THEN legacy_amount ELSE amount END selected_amount
 FROM drama_catalog_v1.credit_ledger l WHERE account_id=$1 AND ($6='' OR holder_id=$6)
 AND ($3='all' OR ($3='acquired' AND kind='add') OR ($3='consumed' AND kind IN ('deduct','refund')) OR ($3='expired' AND kind='expire') OR ($3='reclaimed' AND kind='reclaim'))
 AND ($4::timestamptz IS NULL OR created_at >= $4) AND ($5::timestamptz IS NULL OR created_at < $5)
), filtered AS MATERIALIZED (SELECT * FROM entries WHERE selected_amount>0),
 language_charges AS MATERIALIZED (
 SELECT p.id project_id,'generation:'||(r.credit->>'id')||':deduct' operation_id,
 true present,bool_and(coalesce(r.status,'')='completed') completed
 FROM drama_catalog_v1.projects p
 CROSS JOIN LATERAL jsonb_to_recordset(coalesce(nullif(p.payload->'runs','null'::jsonb),'[]'::jsonb)) AS r(credit jsonb,status text)
 WHERE p.space_id=$1 AND r.credit->>'accountId'=$1 AND p.id IN (SELECT source FROM filtered WHERE kind='deduct') AND r.credit IS NOT NULL
 GROUP BY p.id,r.credit->>'id'
), deductions AS (
 SELECT f.id,f.selected_amount,
 coalesce((SELECT sum(CASE $2 WHEN 'recharge' THEN r.recharge_amount WHEN 'gift' THEN r.gift_amount WHEN 'legacy' THEN r.legacy_amount ELSE r.amount END) FROM drama_catalog_v1.credit_ledger r WHERE r.original_deduction_id=f.id),0)::bigint refunded_amount,
 CASE WHEN f.source='operations-adjustment' THEN 'operations-adjustment'
 WHEN lang.present THEN 'language-generation' WHEN media.present THEN 'media-generation' ELSE 'unsettled' END analysis_source,
 f.source='operations-adjustment' OR coalesce(lang.completed,false) OR coalesce(media.completed,false) settled
 FROM filtered f
 LEFT JOIN language_charges lang ON f.kind='deduct' AND lang.project_id=f.source AND lang.operation_id=f.operation_id
 LEFT JOIN LATERAL (
  SELECT true present,t.status IN ('completed','succeeded') completed FROM drama_catalog_v1.tasks t
  WHERE t.space_id=$1 AND t.input_snapshot#>>'{callerContext,credit,accountId}'=$1 AND f.operation_id='generation:'||(t.input_snapshot#>>'{callerContext,credit,id}')||':deduct'
 ) media ON f.kind='deduct'
 WHERE f.kind='deduct'
), net AS MATERIALIZED (
 SELECT analysis_source,settled,selected_amount-refunded_amount amount FROM deductions
)`

func (s *Store) CreditHistory(ctx context.Context, id string, f d.HistoryFilter) (d.History, error) {
	return s.creditHistory(ctx, id, "", f)
}
func (s *Store) HolderCreditHistory(ctx context.Context, id, holder string, f d.HistoryFilter) (d.History, error) {
	if holder == "" {
		return d.History{}, fault.ErrInvalid
	}
	return s.creditHistory(ctx, id, holder, f)
}
func (s *Store) creditHistory(ctx context.Context, id, holder string, f d.HistoryFilter) (d.History, error) {
	out := d.History{Items: []d.Entry{}, Analysis: []d.ConsumptionAnalysis{}, Page: f.Page, PageSize: f.PageSize}
	if id == "" {
		return out, fault.ErrInvalid
	}
	if err := f.Validate(); err != nil {
		return out, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, creditQueryError(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	args := []any{id, f.Category, f.Direction, f.Start, f.End, holder}
	err = tx.QueryRow(ctx, creditHistoryProjection+` SELECT count(*),
 coalesce(sum(selected_amount) FILTER(WHERE kind='add'),0)::bigint,
 coalesce(sum(selected_amount) FILTER(WHERE kind='deduct'),0)::bigint,
 coalesce(sum(selected_amount) FILTER(WHERE kind='refund'),0)::bigint,
 coalesce((SELECT sum(amount) FROM net),0)::bigint,
 coalesce((SELECT sum(amount) FROM net WHERE settled),0)::bigint,
 coalesce((SELECT sum(amount) FROM net WHERE NOT settled),0)::bigint
 FROM filtered`, args...).Scan(&out.Total, &out.Summary.Acquired, &out.Summary.Consumed, &out.Summary.Refunded, &out.Summary.NetConsumed, &out.Summary.SettledConsumed, &out.Summary.Pending)
	if err != nil {
		return out, creditQueryError(err)
	}
	for _, amount := range []int64{out.Summary.Acquired, out.Summary.Consumed, out.Summary.Refunded, out.Summary.NetConsumed, out.Summary.SettledConsumed, out.Summary.Pending} {
		if amount < 0 || amount > d.MaxAmount {
			return out, fault.New("CREDIT_TOTAL_OVERFLOW", "积分统计超出安全整数范围，请缩小查询时间范围")
		}
	}
	rows, err := tx.Query(ctx, creditHistoryProjection+` SELECT analysis_source,sum(amount)::bigint FROM net WHERE settled AND amount>0 GROUP BY analysis_source ORDER BY sum(amount) DESC,analysis_source`, args...)
	if err != nil {
		return out, creditQueryError(err)
	}
	for rows.Next() {
		var v d.ConsumptionAnalysis
		if err = rows.Scan(&v.Source, &v.Amount); err != nil {
			rows.Close()
			return out, creditQueryError(err)
		}
		if v.Amount <= 0 || v.Amount > d.MaxAmount {
			rows.Close()
			return out, fault.New("CREDIT_TOTAL_OVERFLOW", "积分统计超出安全整数范围，请缩小查询时间范围")
		}
		out.Analysis = append(out.Analysis, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, creditQueryError(err)
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err = tx.Query(ctx, creditHistoryProjection+` SELECT `+creditColumns+` FROM filtered ORDER BY created_at DESC,id DESC LIMIT $7 OFFSET $8`, args...)
	if err != nil {
		return out, creditQueryError(err)
	}
	for rows.Next() {
		v, e := scanCredit(rows)
		if e != nil {
			rows.Close()
			return out, creditQueryError(e)
		}
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, creditQueryError(err)
	}
	return out, tx.Commit(ctx)
}

func (s *Store) CreditBalances(ctx context.Context, account string) (d.Balances, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return d.Balances{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	w, err := creditWalletTx(ctx, tx, account)
	if err != nil {
		return d.Balances{}, err
	}
	allocated, err := allocatedCreditsTx(ctx, tx, account)
	if err != nil {
		return d.Balances{}, err
	}
	v := d.Balances{Wallet: w, Allocated: allocated}
	if _, err = v.Unallocated(); err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
