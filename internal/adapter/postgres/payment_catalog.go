package postgres

import (
	"context"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/payment"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

const paymentPackageColumns = `id,revision,name,original_amount_fen,amount_fen,recharge_credits,gift_credits,enabled,sort,test_only,created_at,updated_at`

func scanPaymentPackage(row pgx.Row) (p.Package, error) {
	var v p.Package
	err := row.Scan(&v.ID, &v.Revision, &v.Name, &v.OriginalAmountFen, &v.AmountFen, &v.RechargeCredits, &v.GiftCredits, &v.Enabled, &v.Sort, &v.TestOnly, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func (s *Store) PaymentPackages(ctx context.Context) ([]p.Package, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+paymentPackageColumns+` FROM drama_catalog_v1.payment_packages ORDER BY sort,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []p.Package{}
	for rows.Next() {
		v, err := scanPaymentPackage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SavePaymentPackage(ctx context.Context, in p.Package, operator string) (p.Package, error) {
	if err := in.Validate(); err != nil {
		return p.Package{}, err
	}
	if in.Revision >= uint64(p.MaxAmount) || operator == "" {
		return p.Package{}, fault.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p.Package{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	// 新档位期望版本0；同ID并发创建也必须走CAS，不能覆盖对方。
	v, err := scanPaymentPackage(tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.payment_packages AS packages(id,revision,name,original_amount_fen,amount_fen,recharge_credits,gift_credits,enabled,sort,test_only) SELECT $1,1,$9,$10,$3,$4,$5,$6,$7,$8 WHERE $2::bigint=0 ON CONFLICT(id) DO NOTHING RETURNING `+paymentPackageColumns, in.ID, in.Revision, in.AmountFen, in.RechargeCredits, in.GiftCredits, in.Enabled, in.Sort, in.TestOnly, in.Name, in.OriginalAmountFen))
	if errors.Is(err, pgx.ErrNoRows) && in.Revision > 0 {
		v, err = scanPaymentPackage(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_packages SET revision=revision+1,name=$9,original_amount_fen=$10,amount_fen=$3,recharge_credits=$4,gift_credits=$5,enabled=$6,sort=$7,test_only=$8,updated_at=clock_timestamp() WHERE id=$1 AND revision=$2 RETURNING `+paymentPackageColumns, in.ID, in.Revision, in.AmountFen, in.RechargeCredits, in.GiftCredits, in.Enabled, in.Sort, in.TestOnly, in.Name, in.OriginalAmountFen))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return p.Package{}, fault.New("PAYMENT_CONFLICT", "档位已变化，请重新读取")
	}
	if err != nil {
		return p.Package{}, err
	}
	if err = recordPaymentEventTx(ctx, tx, p.Event{Source: "package", Outcome: "saved", OperatorID: operator, Facts: map[string]any{"packageId": v.ID, "revision": v.Revision}}); err != nil {
		return p.Package{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p.Package{}, paymentUnknown()
	}
	return v, nil
}
func (s *Store) PaymentSettings(ctx context.Context) (p.Settings, error) {
	var v p.Settings
	err := s.pool.QueryRow(ctx, `SELECT revision,daily_limit_fen FROM drama_catalog_v1.payment_settings WHERE id`).Scan(&v.Revision, &v.DailyLimitFen)
	return v, err
}
func (s *Store) SavePaymentSettings(ctx context.Context, in p.Settings, operator string) (p.Settings, error) {
	if in.Revision >= uint64(p.MaxAmount) || in.DailyLimitFen < 0 || in.DailyLimitFen > p.MaxAmount || operator == "" {
		return p.Settings{}, fault.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p.Settings{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var v p.Settings
	err = tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_settings SET revision=revision+1,daily_limit_fen=$2 WHERE id AND revision=$1 RETURNING revision,daily_limit_fen`, in.Revision, in.DailyLimitFen).Scan(&v.Revision, &v.DailyLimitFen)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, fault.New("PAYMENT_CONFLICT", "充值设置已变化，请重新读取")
	}
	if err != nil {
		return v, err
	}
	if err = recordPaymentEventTx(ctx, tx, p.Event{Source: "settings", Outcome: "saved", OperatorID: operator, Facts: map[string]any{"revision": v.Revision, "dailyLimitFen": v.DailyLimitFen}}); err != nil {
		return v, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p.Settings{}, paymentUnknown()
	}
	return v, nil
}
func (s *Store) PaymentAgreement(ctx context.Context, user, version string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.payment_agreements WHERE user_id=$1 AND version=$2)`, user, version).Scan(&ok)
	return ok, err
}
func paymentUnknown() error {
	return fault.New("PAYMENT_COMMIT_UNKNOWN", "支付提交结果不明，请按原编号回读")
}
func recordPaymentEventTx(ctx context.Context, tx pgx.Tx, e p.Event) error {
	if e.ID == "" {
		e.ID = identity.New("payment_event")
	}
	if e.Facts == nil {
		e.Facts = map[string]any{}
	}
	raw, err := json.Marshal(e.Facts)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.payment_events(id,order_no,refund_id,source,outcome,facts,operator_id,reason) VALUES($1,NULLIF($2,''),NULLIF($3,''),$4,$5,$6,NULLIF($7,''),$8) ON CONFLICT(id) DO NOTHING`, e.ID, e.OrderNo, e.RefundID, e.Source, e.Outcome, raw, e.OperatorID, e.Reason)
	return err
}
func (s *Store) RecordPaymentEvent(ctx context.Context, e p.Event) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = recordPaymentEventTx(ctx, tx, e); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return paymentUnknown()
	}
	return nil
}
