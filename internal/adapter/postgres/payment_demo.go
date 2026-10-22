package postgres

import (
	"context"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"errors"
	"github.com/jackc/pgx/v5"
)

const demoPaymentColumns = `order_no,state,amount_fen,refunded_fen,paid_at,refund_no,refund_bill_at,updated_at`

func scanDemoPayment(row pgx.Row) (ports.DemoPaymentTrade, error) {
	var t ports.DemoPaymentTrade
	err := row.Scan(&t.OrderNo, &t.State, &t.AmountFen, &t.RefundedFen, &t.PaidAt, &t.RefundNo, &t.RefundBillAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return t, err
}
func (s *Store) DemoPayment(ctx context.Context, no string) (ports.DemoPaymentTrade, error) {
	return scanDemoPayment(s.pool.QueryRow(ctx, `SELECT `+demoPaymentColumns+` FROM drama_catalog_v1.payment_demo_trades WHERE order_no=$1`, no))
}
func (s *Store) CreateDemoPayment(ctx context.Context, no string, amount int64) (ports.DemoPaymentTrade, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ports.DemoPaymentTrade{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.payment_demo_trades(order_no,state,amount_fen) VALUES($1,'notpay',$2) ON CONFLICT(order_no) DO NOTHING`, no, amount); err != nil {
		return ports.DemoPaymentTrade{}, err
	}
	trade, err := scanDemoPayment(tx.QueryRow(ctx, `SELECT `+demoPaymentColumns+` FROM drama_catalog_v1.payment_demo_trades WHERE order_no=$1 FOR UPDATE`, no))
	if err != nil {
		return trade, err
	}
	if trade.AmountFen != amount {
		return trade, fault.New("PAYMENT_CONFLICT", "演示订单编号对应不同金额")
	}
	if err = tx.Commit(ctx); err != nil {
		return ports.DemoPaymentTrade{}, paymentUnknown()
	}
	return trade, nil
}
func (s *Store) CloseDemoPayment(ctx context.Context, no string) (ports.DemoPaymentTrade, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ports.DemoPaymentTrade{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	trade, err := scanDemoPayment(tx.QueryRow(ctx, `SELECT `+demoPaymentColumns+` FROM drama_catalog_v1.payment_demo_trades WHERE order_no=$1 FOR UPDATE`, no))
	if err != nil {
		return trade, err
	}
	if trade.State == "paid" {
		return trade, fault.New("PAYMENT_CONFLICT", "演示订单已支付，请重新查单")
	}
	if trade.State != "closed" {
		trade, err = scanDemoPayment(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_demo_trades SET state='closed',updated_at=clock_timestamp() WHERE order_no=$1 RETURNING `+demoPaymentColumns, no))
		if err != nil {
			return trade, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return ports.DemoPaymentTrade{}, paymentUnknown()
	}
	return trade, nil
}
func (s *Store) SimulateDemoPayment(ctx context.Context, no, result string) (ports.DemoPaymentTrade, error) {
	if result != "paid" && result != "failed" {
		return ports.DemoPaymentTrade{}, fault.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ports.DemoPaymentTrade{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	trade, err := scanDemoPayment(tx.QueryRow(ctx, `SELECT `+demoPaymentColumns+` FROM drama_catalog_v1.payment_demo_trades WHERE order_no=$1 FOR UPDATE`, no))
	if err != nil {
		return trade, err
	}
	if trade.State == "paid" {
		if result != "paid" {
			return trade, fault.New("PAYMENT_CONFLICT", "已支付的演示交易不能回退")
		}
		return trade, nil
	}
	if trade.State == "closed" && result != "paid" {
		return trade, fault.New("PAYMENT_CONFLICT", "已关闭的演示交易不能回退")
	}
	// closed→paid只模拟渠道迟到事实；是否允许用户触发由支付应用development gate负责。
	trade, err = scanDemoPayment(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_demo_trades SET state=$2,paid_at=CASE WHEN $2='paid' THEN coalesce(paid_at,clock_timestamp()) ELSE paid_at END,updated_at=clock_timestamp() WHERE order_no=$1 RETURNING `+demoPaymentColumns, no, result))
	if err != nil {
		return trade, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ports.DemoPaymentTrade{}, paymentUnknown()
	}
	return trade, nil
}
func (s *Store) RefundDemoPayment(ctx context.Context, in ports.ChannelRefund) (ports.DemoPaymentTrade, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ports.DemoPaymentTrade{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('drama-agent:demo-refund:'||$1,0))`, in.RefundNo); err != nil {
		return ports.DemoPaymentTrade{}, err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT order_no FROM drama_catalog_v1.payment_demo_trades WHERE refund_no=$1`, in.RefundNo).Scan(&existing)
	if err == nil && existing != in.OrderNo {
		return ports.DemoPaymentTrade{}, fault.New("PAYMENT_CONFLICT", "演示退款编号对应不同订单")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ports.DemoPaymentTrade{}, err
	}
	trade, err := scanDemoPayment(tx.QueryRow(ctx, `SELECT `+demoPaymentColumns+` FROM drama_catalog_v1.payment_demo_trades WHERE order_no=$1 FOR UPDATE`, in.OrderNo))
	if err != nil {
		return trade, err
	}
	if trade.State != "paid" || trade.AmountFen != in.TotalFen || in.AmountFen <= 0 || in.AmountFen > trade.AmountFen {
		return trade, fault.New("PAYMENT_CONFLICT", "演示退款与原支付不符")
	}
	if trade.RefundNo != "" {
		if trade.RefundNo != in.RefundNo || trade.RefundedFen != in.AmountFen {
			return trade, fault.New("PAYMENT_CONFLICT", "演示退款编号对应不同内容")
		}
		return trade, nil
	}
	trade, err = scanDemoPayment(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_demo_trades SET refunded_fen=$2,refund_no=$3,refund_bill_at=clock_timestamp(),updated_at=clock_timestamp() WHERE order_no=$1 RETURNING `+demoPaymentColumns, in.OrderNo, in.AmountFen, in.RefundNo))
	if err != nil {
		return trade, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ports.DemoPaymentTrade{}, paymentUnknown()
	}
	return trade, nil
}

var _ ports.DemoPaymentStore = (*Store)(nil)
