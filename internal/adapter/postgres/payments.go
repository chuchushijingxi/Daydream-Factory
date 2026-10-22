package postgres

import (
	"context"
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

const paymentOrderColumns = `order_no,buyer_user_id,client_request_id,target_kind,target_id,target_name,credit_account_id,package_id,original_amount_fen,amount_fen,recharge_credits,gift_credits,channel,code_url,channel_trade_no,status,close_reason,agreement_version,expires_at,next_query_at,query_attempts,created_at,credited_at,closed_at,refunded_at,channel_paid_at`

func scanPaymentOrder(row pgx.Row) (p.Order, error) {
	var o p.Order
	err := row.Scan(&o.OrderNo, &o.BuyerUserID, &o.ClientRequestID, &o.Target.Kind, &o.Target.ID, &o.Target.Name, &o.CreditAccountID, &o.PackageID, &o.OriginalAmountFen, &o.AmountFen, &o.RechargeCredits, &o.GiftCredits, &o.Channel, &o.CodeURL, &o.ChannelTradeNo, &o.Status, &o.CloseReason, &o.AgreementVersion, &o.ExpiresAt, &o.NextQueryAt, &o.QueryAttempts, &o.CreatedAt, &o.CreditedAt, &o.ClosedAt, &o.RefundedAt, &o.PaidAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, fault.New("PAYMENT_ORDER_NOT_FOUND", "充值订单不存在")
	}
	return o, err
}
func (s *Store) PaymentOrder(ctx context.Context, no string) (p.Order, error) {
	return scanPaymentOrder(s.pool.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE order_no=$1`, no))
}
func (s *Store) PaymentOrderByRequest(ctx context.Context, buyer, request string) (p.Order, error) {
	return scanPaymentOrder(s.pool.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE buyer_user_id=$1 AND client_request_id=$2`, buyer, request))
}
func (s *Store) PendingPaymentOrder(ctx context.Context, buyer string) (p.Order, error) {
	return scanPaymentOrder(s.pool.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE buyer_user_id=$1 AND status='pending'`, buyer))
}
func (s *Store) CreatePaymentOrder(ctx context.Context, in p.Order) (p.Order, bool, error) {
	if !p.ValidOrderNo(in.OrderNo) || !in.Target.Valid() || in.BuyerUserID == "" || in.ClientRequestID == "" || len(in.ClientRequestID) > 128 || in.CreditAccountID == "" || in.AgreementVersion != p.AgreementVersion {
		return p.Order{}, false, fault.ErrInvalid
	}
	if err := in.PackageSnapshot.Validate(); err != nil {
		return p.Order{}, false, err
	}
	// 团队归属、到期与停用由RechargeTargets在下单前核验；这里只固定目标与钱包的对应关系。
	if in.CreditAccountID != in.Target.ID || (in.Target.Kind == "personal" && in.Target.ID != in.BuyerUserID) {
		return p.Order{}, false, fault.New("PAYMENT_TARGET_FORBIDDEN", "充值目标无权访问")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p.Order{}, false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('drama-agent:payment:buyer:'||$1,0))`, in.BuyerUserID); err != nil {
		return p.Order{}, false, err
	}
	old, err := scanPaymentOrder(tx.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE buyer_user_id=$1 AND client_request_id=$2`, in.BuyerUserID, in.ClientRequestID))
	if err == nil {
		if old.Target.Kind != in.Target.Kind || old.Target.ID != in.Target.ID || old.PackageID != in.PackageID || old.Channel != in.Channel || old.AgreementVersion != in.AgreementVersion {
			return p.Order{}, false, fault.New("PAYMENT_CONFLICT", "下单编号对应不同内容")
		}
		return old, false, nil
	}
	var missing *fault.Error
	if !errors.As(err, &missing) || missing.Code != "PAYMENT_ORDER_NOT_FOUND" {
		return p.Order{}, false, err
	}
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT enabled FROM drama_catalog_v1.users WHERE id=$1 FOR SHARE`, in.BuyerUserID).Scan(&enabled)
	if err != nil {
		return p.Order{}, false, err
	}
	if !enabled {
		return p.Order{}, false, fault.New("PAYMENT_TARGET_INACTIVE", "账号已停用")
	}
	current, err := scanPaymentPackage(tx.QueryRow(ctx, `SELECT `+paymentPackageColumns+` FROM drama_catalog_v1.payment_packages WHERE id=$1 FOR SHARE`, in.PackageID))
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (!current.Enabled || current.Snapshot() != in.PackageSnapshot) {
		return p.Order{}, false, fault.New("PAYMENT_PACKAGE_UNAVAILABLE", "充值档位已变化，请重新选择")
	}
	if err != nil {
		return p.Order{}, false, err
	}
	var now time.Time
	var limit int64
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp(),daily_limit_fen FROM drama_catalog_v1.payment_settings WHERE id FOR SHARE`).Scan(&now, &limit); err != nil {
		return p.Order{}, false, err
	}
	start, end := p.BeijingDay(now)
	var paid int64
	// 0为不限，无需累计；有限上限只需知道是否已饱和，避免生命周期总额超出int64。
	if limit > 0 {
		if err = tx.QueryRow(ctx, `SELECT least(coalesce(sum(amount_fen),0),9007199254740991)::bigint FROM drama_catalog_v1.payment_orders WHERE buyer_user_id=$1 AND credited_at>=$2 AND credited_at<$3`, in.BuyerUserID, start, end).Scan(&paid); err != nil {
			return p.Order{}, false, err
		}
	}
	if err = p.CheckDailyLimit(paid, in.AmountFen, limit); err != nil {
		return p.Order{}, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.payment_agreements(user_id,version,agreed_at) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, in.BuyerUserID, in.AgreementVersion, now); err != nil {
		return p.Order{}, false, err
	}
	out, err := scanPaymentOrder(tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.payment_orders(order_no,buyer_user_id,client_request_id,target_kind,target_id,target_name,credit_account_id,package_id,original_amount_fen,amount_fen,recharge_credits,gift_credits,channel,status,agreement_version,expires_at,next_query_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$17,$9,$10,$11,$12,'pending',$13,$14,$15,$16) RETURNING `+paymentOrderColumns, in.OrderNo, in.BuyerUserID, in.ClientRequestID, in.Target.Kind, in.Target.ID, in.Target.Name, in.CreditAccountID, in.PackageID, in.AmountFen, in.RechargeCredits, in.GiftCredits, in.Channel, in.AgreementVersion, now.Add(p.OrderLifetime), now.Add(10*time.Second), now, in.OriginalAmountFen))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return p.Order{}, false, fault.New("PAYMENT_CONFLICT", "已有待支付订单，请先关闭")
	}
	if err != nil {
		return p.Order{}, false, err
	}
	if err = recordPaymentEventTx(ctx, tx, p.Event{OrderNo: out.OrderNo, Source: "create", Outcome: "pending", Facts: map[string]any{"amountFen": out.AmountFen, "channel": out.Channel}}); err != nil {
		return p.Order{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p.Order{}, false, paymentUnknown()
	}
	return out, true, nil
}
func (s *Store) SetPaymentOrderCode(ctx context.Context, no, code, source string) (p.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p.Order{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	o, err := scanPaymentOrder(tx.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE order_no=$1 FOR UPDATE`, no))
	if err != nil {
		return o, err
	}
	if o.Status == p.Pending {
		o, err = scanPaymentOrder(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_orders SET code_url=$2,next_query_at=CASE WHEN $2='' THEN clock_timestamp() ELSE next_query_at END WHERE order_no=$1 RETURNING `+paymentOrderColumns, no, code))
		if err != nil {
			return o, err
		}
	}
	if err = recordPaymentEventTx(ctx, tx, p.Event{OrderNo: no, Source: source, Outcome: "created", Facts: map[string]any{"codeAvailable": code != ""}}); err != nil {
		return p.Order{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p.Order{}, paymentUnknown()
	}
	return o, nil
}
func (s *Store) CreditPaymentOrder(ctx context.Context, in ports.PaymentConfirmation) (p.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p.Order{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	o, err := scanPaymentOrder(tx.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE order_no=$1 FOR UPDATE`, in.OrderNo))
	if err != nil {
		return o, err
	}
	if in.Channel != o.Channel || in.Trade.State != "paid" || in.Trade.OrderNo != o.OrderNo || in.Trade.TradeNo == "" || in.Trade.PaidAt == nil || in.Trade.PaidAt.IsZero() || in.Trade.AmountFen != o.AmountFen {
		return p.Order{}, fault.New("PAYMENT_CHANNEL_FAILED", "支付事实与订单不符")
	}
	if o.ChannelTradeNo != "" && o.ChannelTradeNo != in.Trade.TradeNo || o.PaidAt != nil && !o.PaidAt.Equal(in.Trade.PaidAt.UTC().Truncate(time.Microsecond)) {
		return p.Order{}, fault.New("PAYMENT_CHANNEL_FAILED", "渠道交易身份或付款时间与已冻结事实不符")
	}
	if o.Status == p.Credited || o.Status == p.Refunded {
		return o, nil
	}
	if o.Status == p.Closed {
		if _, err = tx.Exec(ctx, `UPDATE drama_catalog_v1.payment_orders SET channel_trade_no=$2,channel_paid_at=$3 WHERE order_no=$1`, o.OrderNo, in.Trade.TradeNo, in.Trade.PaidAt); err != nil {
			return p.Order{}, err
		}
		o.ChannelTradeNo = in.Trade.TradeNo
		o.PaidAt = in.Trade.PaidAt
		no := "LR" + o.OrderNo
		result, insertErr := tx.Exec(ctx, `INSERT INTO drama_catalog_v1.payment_refunds(id,refund_no,order_no,kind,amount_fen,recharge_credits,gift_credits,status,reason,next_query_at) VALUES($1,$1,$2,'late_payment',$3,0,0,'pending','关单后付款自动原路退款',clock_timestamp()) ON CONFLICT DO NOTHING`, no, o.OrderNo, o.AmountFen)
		if insertErr != nil {
			return p.Order{}, insertErr
		}
		if result.RowsAffected() == 0 {
			return o, nil
		}
		if err = recordPaymentAnomalyTx(ctx, tx, p.Anomaly{ID: "late:" + o.OrderNo, OrderNo: o.OrderNo, Channel: o.Channel, Kind: "paid_after_close", Detail: map[string]any{"tradeNo": in.Trade.TradeNo, "amountFen": o.AmountFen}}); err != nil {
			return p.Order{}, err
		}
	} else {
		if _, err = o.Status.Transition(p.Credited); err != nil {
			return p.Order{}, err
		}
		change := c.Change{AccountID: o.CreditAccountID, Amount: o.RechargeCredits, OperationID: "recharge:order:" + o.OrderNo, Source: "payment-order:" + o.OrderNo, Subject: "payment:" + o.Channel, Reason: "充值到账", Category: c.Recharge}
		if _, err = s.creditChangeTx(ctx, tx, "add", change); err != nil {
			return p.Order{}, err
		}
		if o.GiftCredits > 0 {
			change.Amount = o.GiftCredits
			change.OperationID = "gift:recharge-bonus:" + o.OrderNo
			change.Reason = "充值赠送"
			change.Category = c.Gift
			if _, err = s.creditChangeTx(ctx, tx, "add", change); err != nil {
				return p.Order{}, err
			}
		}
		o, err = scanPaymentOrder(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_orders SET status='credited',channel_trade_no=$2,credited_at=clock_timestamp(),channel_paid_at=$3 WHERE order_no=$1 AND status='pending' RETURNING `+paymentOrderColumns, o.OrderNo, in.Trade.TradeNo, in.Trade.PaidAt))
		if err != nil {
			return p.Order{}, err
		}
	}
	if err = recordPaymentEventTx(ctx, tx, p.Event{OrderNo: o.OrderNo, Source: in.Source, Outcome: string(o.Status), Facts: map[string]any{"tradeNo": in.Trade.TradeNo, "amountFen": in.Trade.AmountFen}}); err != nil {
		return p.Order{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		// 请求取消不等于数据库回滚；有限期限内只读原编号，不换编号补发。
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if confirmed, ok, e := s.ConfirmedPaymentCredit(readCtx, o.OrderNo); e == nil && ok {
			return confirmed, nil
		}
		return p.Order{}, paymentUnknown()
	}
	return o, nil
}
func (s *Store) ConfirmedPaymentCredit(ctx context.Context, no string) (p.Order, bool, error) {
	o, err := s.PaymentOrder(ctx, no)
	if err != nil {
		return o, false, err
	}
	if o.Status != p.Credited && o.Status != p.Refunded {
		return o, false, nil
	}
	change := c.Change{AccountID: o.CreditAccountID, Amount: o.RechargeCredits, OperationID: "recharge:order:" + no, Source: "payment-order:" + no, Subject: "payment:" + o.Channel, Reason: "充值到账", Category: c.Recharge}
	entry, err := s.CreditOperation(ctx, change.OperationID)
	if err != nil {
		return o, false, err
	}
	if !entry.Same("add", change) {
		return o, false, fault.New("PAYMENT_CONFLICT", "到账流水与订单不符")
	}
	if o.GiftCredits > 0 {
		change.Amount = o.GiftCredits
		change.OperationID = "gift:recharge-bonus:" + no
		change.Reason = "充值赠送"
		change.Category = c.Gift
		entry, err = s.CreditOperation(ctx, change.OperationID)
		if err != nil {
			return o, false, err
		}
		if !entry.Same("add", change) {
			return o, false, fault.New("PAYMENT_CONFLICT", "赠送流水与订单不符")
		}
	}
	return o, true, nil
}
func (s *Store) ClosePaymentOrder(ctx context.Context, no, reason, source string) (p.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p.Order{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	o, err := scanPaymentOrder(tx.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE order_no=$1 FOR UPDATE`, no))
	if err != nil {
		return o, err
	}
	if o.Status != p.Pending {
		return o, nil
	}
	o, err = scanPaymentOrder(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_orders SET status='closed',close_reason=$2,closed_at=clock_timestamp() WHERE order_no=$1 AND status='pending' RETURNING `+paymentOrderColumns, no, reason))
	if err != nil {
		return o, err
	}
	if err = recordPaymentEventTx(ctx, tx, p.Event{OrderNo: no, Source: source, Outcome: "closed", Reason: reason}); err != nil {
		return p.Order{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p.Order{}, paymentUnknown()
	}
	return o, nil
}
func (s *Store) ClaimPaymentOrders(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]p.Order, error) {
	if limit < 1 || limit > 100 || lease <= 0 {
		return nil, fault.ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `WITH due AS (SELECT order_no FROM drama_catalog_v1.payment_orders WHERE status='pending' AND next_query_at<=$1 ORDER BY next_query_at,order_no LIMIT $2 FOR UPDATE SKIP LOCKED) UPDATE drama_catalog_v1.payment_orders o SET next_query_at=$3,query_attempts=query_attempts+1 FROM due WHERE o.order_no=due.order_no RETURNING `+prefixPaymentColumns(paymentOrderColumns, "o"), now, limit, now.Add(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []p.Order{}
	for rows.Next() {
		o, err := scanPaymentOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Store) SchedulePaymentOrder(ctx context.Context, no string, expectedAttempts int, next time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE drama_catalog_v1.payment_orders SET next_query_at=$2 WHERE order_no=$1 AND status='pending' AND query_attempts=$3`, no, next, expectedAttempts)
	return err
}

var _ ports.PaymentStore = (*Store)(nil)
