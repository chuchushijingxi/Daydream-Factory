package postgres

import (
	"context"
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/payment"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

func prefixPaymentColumns(columns, prefix string) string {
	parts := strings.Split(columns, ",")
	for i := range parts {
		parts[i] = prefix + "." + parts[i]
	}
	return strings.Join(parts, ",")
}
func paymentPageValid(page, size int) bool {
	return page >= 1 && page <= 1000000 && size >= 1 && size <= 100
}

const paymentOrderFilterSQL = ` FROM drama_catalog_v1.payment_orders WHERE ($1='' OR buyer_user_id=$1) AND ($2='' OR order_no=$2) AND ($3='' OR channel_trade_no=$3) AND ($4='' OR target_kind=$4) AND ($5='' OR target_id=$5) AND ($6='' OR channel=$6) AND ($7='' OR status=$7) AND ($8::timestamptz IS NULL OR created_at>=$8) AND ($9::timestamptz IS NULL OR created_at<$9)`

func (s *Store) PaymentOrders(ctx context.Context, f ports.PaymentOrderFilter) (ports.Page[p.Order], error) {
	out := ports.Page[p.Order]{Items: []p.Order{}, Page: f.Page, PageSize: f.PageSize}
	if !paymentPageValid(f.Page, f.PageSize) {
		return out, fault.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	args := []any{f.BuyerUserID, f.OrderNo, f.TradeNo, f.TargetKind, f.TargetID, f.Channel, f.Status, f.Start, f.End}
	if err = tx.QueryRow(ctx, `SELECT count(*)`+paymentOrderFilterSQL, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := tx.Query(ctx, `SELECT `+paymentOrderColumns+paymentOrderFilterSQL+` ORDER BY created_at DESC,order_no DESC LIMIT $10 OFFSET $11`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanPaymentOrder(rows)
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
func (s *Store) PaymentOrderDetails(ctx context.Context, no string) (ports.PaymentOrderDetails, error) {
	out := ports.PaymentOrderDetails{Events: []p.Event{}, Refunds: []p.Refund{}, Credits: []c.Entry{}}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	out.Order, err = scanPaymentOrder(tx.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE order_no=$1`, no))
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id,coalesce(order_no,''),coalesce(refund_id,''),source,outcome,coalesce(operator_id,''),reason,facts,created_at FROM drama_catalog_v1.payment_events WHERE order_no=$1 ORDER BY created_at,id`, no)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var e p.Event
		var raw []byte
		if err = rows.Scan(&e.ID, &e.OrderNo, &e.RefundID, &e.Source, &e.Outcome, &e.OperatorID, &e.Reason, &raw, &e.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		if err = json.Unmarshal(raw, &e.Facts); err != nil {
			rows.Close()
			return out, err
		}
		out.Events = append(out.Events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT `+paymentRefundColumns+` FROM drama_catalog_v1.payment_refunds WHERE order_no=$1 ORDER BY created_at,id`, no)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanPaymentRefund(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Refunds = append(out.Refunds, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE source=$1 ORDER BY created_at,id`, "payment-order:"+no)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanCredit(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Credits = append(out.Credits, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

const paymentAnomalyColumns = `id,coalesce(order_no,''),channel,kind,detail,status,coalesce(resolved_by,''),note,resolved_at,created_at`

func scanPaymentAnomaly(row pgx.Row) (p.Anomaly, error) {
	var v p.Anomaly
	var raw []byte
	err := row.Scan(&v.ID, &v.OrderNo, &v.Channel, &v.Kind, &raw, &v.Status, &v.ResolvedBy, &v.Note, &v.ResolvedAt, &v.CreatedAt)
	if err == nil {
		err = json.Unmarshal(raw, &v.Detail)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return v, err
}
func recordPaymentAnomalyTx(ctx context.Context, tx pgx.Tx, in p.Anomaly) error {
	if in.ID == "" {
		in.ID = identity.New("payment_anomaly")
	}
	if in.Detail == nil {
		in.Detail = map[string]any{}
	}
	raw, err := json.Marshal(in.Detail)
	if err != nil {
		return err
	}
	// 未知订单号来自渠道，不能把不存在的外部引用写入FK。
	_, err = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.payment_anomalies(id,order_no,channel,kind,detail) VALUES($1,(SELECT order_no FROM drama_catalog_v1.payment_orders WHERE order_no=$2),$3,$4,$5) ON CONFLICT(id) DO NOTHING`, in.ID, in.OrderNo, in.Channel, in.Kind, raw)
	return err
}
func (s *Store) RecordPaymentAnomaly(ctx context.Context, in p.Anomaly) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = recordPaymentAnomalyTx(ctx, tx, in); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return paymentUnknown()
	}
	return nil
}
func (s *Store) PaymentAnomalies(ctx context.Context, f ports.PaymentAnomalyFilter) (ports.Page[p.Anomaly], error) {
	out := ports.Page[p.Anomaly]{Items: []p.Anomaly{}, Page: f.Page, PageSize: f.PageSize}
	if !paymentPageValid(f.Page, f.PageSize) {
		return out, fault.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	where := ` FROM drama_catalog_v1.payment_anomalies WHERE ($1='' OR status=$1) AND ($2='' OR kind=$2) AND ($3='' OR channel=$3)`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, f.Status, f.Kind, f.Channel).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT `+paymentAnomalyColumns+where+` ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5`, f.Status, f.Kind, f.Channel, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanPaymentAnomaly(rows)
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
func (s *Store) ResolvePaymentAnomaly(ctx context.Context, id, operator, note string) (p.Anomaly, error) {
	if id == "" || operator == "" || strings.TrimSpace(note) == "" || len(note) > 2000 {
		return p.Anomaly{}, fault.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p.Anomaly{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	v, err := scanPaymentAnomaly(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_anomalies SET status='resolved',resolved_by=$2,note=$3,resolved_at=clock_timestamp() WHERE id=$1 AND status='open' RETURNING `+paymentAnomalyColumns, id, operator, note))
	if err != nil {
		return v, err
	}
	if err = recordPaymentEventTx(ctx, tx, p.Event{OrderNo: v.OrderNo, Source: "anomaly", Outcome: "resolved", OperatorID: operator, Reason: note, Facts: map[string]any{"anomalyId": id}}); err != nil {
		return p.Anomaly{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p.Anomaly{}, paymentUnknown()
	}
	return v, nil
}
func (s *Store) ClaimPaymentBill(ctx context.Context, channel string, candidate *time.Time, now time.Time) (ports.PaymentBillClaim, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ports.PaymentBillClaim{}, false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('drama-agent:payment:bill:'||$1,0))`, channel); err != nil {
		return ports.PaymentBillClaim{}, false, err
	}
	if candidate != nil {
		var last *time.Time
		if err = tx.QueryRow(ctx, `SELECT max(bill_date) FROM drama_catalog_v1.payment_bills WHERE channel=$1`, channel).Scan(&last); err != nil {
			return ports.PaymentBillClaim{}, false, err
		}
		first := *candidate
		if last != nil {
			first = last.AddDate(0, 0, 1)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.payment_bills(channel,bill_date,status,next_attempt_at) SELECT $1,d::date,'pending',$4 FROM generate_series($2::date,$3::date,interval '1 day') d ON CONFLICT DO NOTHING`, channel, first.Format("2006-01-02"), candidate.Format("2006-01-02"), now); err != nil {
			return ports.PaymentBillClaim{}, false, err
		}
	}
	var claim ports.PaymentBillClaim
	err = tx.QueryRow(ctx, `WITH due AS (SELECT channel,bill_date FROM drama_catalog_v1.payment_bills WHERE channel=$1 AND status<>'compared' AND attempts<24 AND next_attempt_at<=$2 ORDER BY bill_date LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE drama_catalog_v1.payment_bills b SET status='pending',attempts=b.attempts+1,next_attempt_at=$3 FROM due WHERE b.channel=due.channel AND b.bill_date=due.bill_date RETURNING b.bill_date,b.attempts`, channel, now, now.Add(time.Hour)).Scan(&claim.Date, &claim.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return claim, false, paymentUnknown()
		}
		return claim, false, nil
	}
	if err != nil {
		return claim, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return claim, false, paymentUnknown()
	}
	return claim, true, nil
}
func (s *Store) FinishPaymentBill(ctx context.Context, channel string, claim ports.PaymentBillClaim, success bool, summary map[string]int64) error {
	raw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	status := "failed"
	if success {
		status = "compared"
	}
	_, err = s.pool.Exec(ctx, `UPDATE drama_catalog_v1.payment_bills SET status=$3,compared_at=CASE WHEN $3='compared' THEN clock_timestamp() ELSE NULL END,summary=$4 WHERE channel=$1 AND bill_date=$2 AND status='pending' AND attempts=$5`, channel, claim.Date.Format("2006-01-02"), status, raw, claim.Attempts)
	return err
}
func (s *Store) PaymentOrdersForBill(ctx context.Context, channel string, start, end time.Time) ([]p.Order, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE channel=$1 AND ((channel_paid_at>=$2 AND channel_paid_at<$3) OR order_no IN (SELECT order_no FROM drama_catalog_v1.payment_refunds WHERE status='succeeded' AND channel_bill_at>=$2 AND channel_bill_at<$3)) ORDER BY order_no`, channel, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []p.Order{}
	for rows.Next() {
		v, e := scanPaymentOrder(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
