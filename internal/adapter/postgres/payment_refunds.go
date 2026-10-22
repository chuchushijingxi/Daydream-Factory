package postgres

import (
	"context"
	"crypto/sha256"
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"time"
)

const paymentRefundColumns = `id,refund_no,order_no,kind,amount_fen,recharge_credits,gift_credits,status,coalesce(operator_id,''),reason,channel_refund_id,attempts,next_query_at,created_at,finished_at,channel_bill_at`

func scanPaymentRefund(row pgx.Row) (p.Refund, error) {
	var r p.Refund
	err := row.Scan(&r.ID, &r.RefundNo, &r.OrderNo, &r.Kind, &r.AmountFen, &r.RechargeCredits, &r.GiftCredits, &r.Status, &r.OperatorID, &r.Reason, &r.ChannelRefundID, &r.Attempts, &r.NextQueryAt, &r.CreatedAt, &r.FinishedAt, &r.BillAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return r, err
}
func (s *Store) PaymentRefundByRequest(ctx context.Context, id string) (p.Refund, error) {
	return scanPaymentRefund(s.pool.QueryRow(ctx, `SELECT `+paymentRefundColumns+` FROM drama_catalog_v1.payment_refunds WHERE id=$1`, id))
}
func (s *Store) PaymentRefund(ctx context.Context, id string) (p.Refund, error) {
	return scanPaymentRefund(s.pool.QueryRow(ctx, `SELECT `+paymentRefundColumns+` FROM drama_catalog_v1.payment_refunds WHERE refund_no=$1`, id))
}

// refundableLimitTx 返回团队“未分配”上限：余额减全部open持有人余额，不扣成员名下积分。
// 个人账户没有分配，返回不限；locked路径已持有钱包行锁，与分配操作互斥。
func refundableLimitTx(ctx context.Context, tx pgx.Tx, accountID string, w c.Wallet) (int64, bool, error) {
	allocated, err := allocatedCreditsTx(ctx, tx, accountID)
	if err != nil || allocated == 0 {
		return p.MaxAmount, false, err
	}
	limit, err := c.Unallocated(w.Balance, allocated)
	return limit, true, err
}
func (s *Store) paymentRefundQuoteTx(ctx context.Context, tx pgx.Tx, o p.Order, requested int64, locked bool) (p.RefundQuote, error) {
	if o.Status != p.Credited {
		return p.RefundQuote{}, fault.New("PAYMENT_CONFLICT", "仅已到账订单可退款")
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.payment_refunds WHERE order_no=$1 AND kind='operator')`, o.OrderNo).Scan(&exists); err != nil {
		return p.RefundQuote{}, err
	}
	if exists {
		return p.RefundQuote{}, fault.New("PAYMENT_REFUND_EXISTS", "此订单已发起过退款")
	}
	var w c.Wallet
	var err error
	if locked {
		w, err = lockedCreditWalletTx(ctx, tx, o.CreditAccountID)
		if err == nil {
			var now time.Time
			err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
			if err == nil {
				w, err = s.expireCreditWalletTx(ctx, tx, o.CreditAccountID, w, now)
			}
		}
	} else {
		w, err = creditWalletTx(ctx, tx, o.CreditAccountID)
	}
	if err != nil {
		return p.RefundQuote{}, err
	}
	limit, limited, err := refundableLimitTx(ctx, tx, o.CreditAccountID, w)
	if err != nil {
		return p.RefundQuote{}, err
	}
	maximum := min(o.RechargeCredits, w.RechargeBalance, limit)
	if requested == 0 {
		requested = maximum
	}
	if maximum == 0 {
		return p.RefundQuote{}, nil
	}
	q, err := p.QuoteRefund(o.PackageSnapshot, w.RechargeBalance, w.GiftBalance-w.DailyBalance, limit, requested)
	if err == nil && limited {
		// 赠送同比例扣回同样只动未分配部分，不足扣到0为止，不抵退款金额。
		q.GiftCredits = min(q.GiftCredits, limit-q.RechargeCredits)
	}
	return q, err
}
func (s *Store) PaymentRefundQuote(ctx context.Context, no string, requested int64) (p.RefundQuote, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return p.RefundQuote{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	o, err := scanPaymentOrder(tx.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE order_no=$1`, no))
	if err != nil {
		return p.RefundQuote{}, err
	}
	q, err := s.paymentRefundQuoteTx(ctx, tx, o, requested, false)
	if err != nil {
		return q, err
	}
	return q, tx.Commit(ctx)
}
func paymentRefundNumber(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "RF" + hex.EncodeToString(sum[:15])
}
func (s *Store) CreatePaymentRefund(ctx context.Context, in p.RefundRequest) (p.Refund, error) {
	if in.RefundID == "" || len(in.RefundID) > 128 || in.OperatorID == "" || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2000 || in.Credits <= 0 || in.Credits > p.MaxAmount {
		return p.Refund{}, fault.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p.Refund{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('drama-agent:payment:refund:'||$1,0))`, in.RefundID); err != nil {
		return p.Refund{}, err
	}
	old, err := scanPaymentRefund(tx.QueryRow(ctx, `SELECT `+paymentRefundColumns+` FROM drama_catalog_v1.payment_refunds WHERE id=$1`, in.RefundID))
	if err == nil {
		if old.OrderNo != in.OrderNo || old.OperatorID != in.OperatorID || old.Reason != in.Reason || old.RechargeCredits != in.Credits {
			return p.Refund{}, fault.New("PAYMENT_CONFLICT", "退款编号对应不同内容")
		}
		return old, nil
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return p.Refund{}, err
	}
	o, err := scanPaymentOrder(tx.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE order_no=$1 FOR UPDATE`, in.OrderNo))
	if err != nil {
		return p.Refund{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return p.Refund{}, err
	}
	if now.After(o.CreatedAt.Add(365 * 24 * time.Hour)) {
		return p.Refund{}, fault.New("PAYMENT_REFUND_EXCEEDED", "订单已超过可申请退款期限")
	}
	// 与creditReclaimTx保持原Add→钱包的锁序。按ID排序同时锁两笔，避免交叉死锁。
	rows, err := tx.Query(ctx, `SELECT id FROM drama_catalog_v1.credit_ledger WHERE operation_id=ANY($1) ORDER BY id FOR UPDATE`, []string{"recharge:order:" + o.OrderNo, "gift:recharge-bonus:" + o.OrderNo})
	if err != nil {
		return p.Refund{}, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return p.Refund{}, err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p.Refund{}, err
	}
	q, err := s.paymentRefundQuoteTx(ctx, tx, o, in.Credits, true)
	if err != nil {
		return p.Refund{}, err
	}
	if q.RechargeCredits != in.Credits {
		return p.Refund{}, fault.New("PAYMENT_REFUND_EXCEEDED", "退款积分超过可退上限")
	}
	if q.AmountFen == 0 {
		return p.Refund{}, fault.New("PAYMENT_INVALID_AMOUNT", "所选退款积分折算不足 0.01 元，请增加退款积分")
	}
	r, err := scanPaymentRefund(tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.payment_refunds(id,refund_no,order_no,kind,amount_fen,recharge_credits,gift_credits,status,operator_id,reason,next_query_at) VALUES($1,$2,$3,'operator',$4,$5,$6,'pending',$7,$8,$9) RETURNING `+paymentRefundColumns, in.RefundID, paymentRefundNumber(in.RefundID), o.OrderNo, q.AmountFen, q.RechargeCredits, q.GiftCredits, in.OperatorID, in.Reason, now))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return p.Refund{}, fault.New("PAYMENT_REFUND_EXISTS", "此订单已发起过退款")
	}
	if err != nil {
		return p.Refund{}, err
	}
	for _, v := range []struct {
		prefix string
		amount int64
	}{{"recharge", r.RechargeCredits}, {"gift", r.GiftCredits}} {
		if v.amount == 0 {
			continue
		}
		op := "recharge:order:" + o.OrderNo
		if v.prefix == "gift" {
			op = "gift:recharge-bonus:" + o.OrderNo
		}
		original, e := scanCredit(tx.QueryRow(ctx, `SELECT `+creditColumns+` FROM drama_catalog_v1.credit_ledger WHERE operation_id=$1`, op))
		if e != nil {
			return p.Refund{}, e
		}
		expected := c.Change{AccountID: o.CreditAccountID, Amount: o.RechargeCredits, Category: v.prefix, OperationID: op, Source: "payment-order:" + o.OrderNo, Subject: "payment:" + o.Channel, Reason: "充值到账"}
		if v.prefix == "gift" {
			expected.Amount = o.GiftCredits
			expected.Reason = "充值赠送"
		}
		if !original.Same("add", expected) {
			return p.Refund{}, fault.New("PAYMENT_CONFLICT", "原到账流水与订单不符")
		}
		if _, e = s.creditReclaimTx(ctx, tx, c.Change{OriginalAdditionID: original.ID, Amount: v.amount, OperationID: v.prefix + ":reclaim:" + r.RefundNo, Subject: in.OperatorID, Reason: "充值退款扣回"}); e != nil {
			return p.Refund{}, e
		}
	}
	if err = recordPaymentEventTx(ctx, tx, p.Event{OrderNo: o.OrderNo, RefundID: r.ID, Source: "refund", Outcome: "pending", OperatorID: in.OperatorID, Reason: in.Reason, Facts: map[string]any{"amountFen": r.AmountFen, "rechargeCredits": r.RechargeCredits, "giftCredits": r.GiftCredits}}); err != nil {
		return p.Refund{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if saved, e := s.PaymentRefund(readCtx, r.RefundNo); e == nil && saved.RefundNo == r.RefundNo && saved.RechargeCredits == r.RechargeCredits {
			return saved, nil
		}
		return p.Refund{}, paymentUnknown()
	}
	return r, nil
}
func (s *Store) FinishPaymentRefund(ctx context.Context, id string, result ports.ChannelRefundResult, source string) (p.Refund, error) {
	current, err := s.PaymentRefund(ctx, id)
	if err != nil {
		return p.Refund{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p.Refund{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	o, err := scanPaymentOrder(tx.QueryRow(ctx, `SELECT `+paymentOrderColumns+` FROM drama_catalog_v1.payment_orders WHERE order_no=$1 FOR UPDATE`, current.OrderNo))
	if err != nil {
		return p.Refund{}, err
	}
	r, err := scanPaymentRefund(tx.QueryRow(ctx, `SELECT `+paymentRefundColumns+` FROM drama_catalog_v1.payment_refunds WHERE id=$1 FOR UPDATE`, current.ID))
	if err != nil {
		return r, err
	}
	wrongIdentity := result.RefundNo != r.RefundNo || result.OrderNo != "" && result.OrderNo != o.OrderNo || result.TradeNo != "" && result.TradeNo != o.ChannelTradeNo
	wrongAmounts := result.AmountFen != 0 && result.AmountFen != r.AmountFen || result.TotalFen != 0 && result.TotalFen != o.AmountFen
	incompleteSuccess := result.State == "succeeded" && (result.AmountFen != r.AmountFen || result.TotalFen != o.AmountFen || result.TradeNo == "" || result.TradeNo != o.ChannelTradeNo)
	if wrongIdentity || wrongAmounts || incompleteSuccess {
		return p.Refund{}, fault.New("PAYMENT_CHANNEL_FAILED", "渠道退款事实与原单不符")
	}
	if result.BillAt != nil && result.BillAt.IsZero() {
		return p.Refund{}, fault.ErrInvalid
	}
	if result.State != "succeeded" && result.State != "failed" {
		if result.State != "pending" && result.State != "not_found" {
			return p.Refund{}, fault.ErrInvalid
		}
		if result.State == "pending" && r.Status == "pending" && r.BillAt == nil && result.BillAt != nil {
			r, err = scanPaymentRefund(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_refunds SET channel_bill_at=$2 WHERE id=$1 AND channel_bill_at IS NULL RETURNING `+paymentRefundColumns, r.ID, result.BillAt))
			if err != nil {
				return r, err
			}
			if err = recordPaymentEventTx(ctx, tx, p.Event{OrderNo: o.OrderNo, RefundID: r.ID, Source: source, Outcome: "refund_accepted", Facts: map[string]any{"refundNo": r.RefundNo}}); err != nil {
				return p.Refund{}, err
			}
			if err = tx.Commit(ctx); err != nil {
				return p.Refund{}, paymentUnknown()
			}
		}
		return r, nil
	}
	if result.State == "succeeded" && r.BillAt == nil && (result.BillAt == nil || result.BillAt.IsZero()) {
		return p.Refund{}, fault.New("PAYMENT_CHANNEL_FAILED", "渠道退款缺少账单归属时间")
	}
	if r.Status != "pending" {
		if r.Status != result.State {
			return p.Refund{}, fault.New("PAYMENT_CONFLICT", "退款已进入不同终态")
		}
		return r, nil
	}
	if result.State == "failed" {
		if r.Kind == "operator" {
			for _, v := range []struct {
				prefix string
				amount int64
			}{{"recharge", r.RechargeCredits}, {"gift", r.GiftCredits}} {
				if v.amount == 0 {
					continue
				}
				if _, err = s.creditChangeTx(ctx, tx, "add", c.Change{AccountID: o.CreditAccountID, Amount: v.amount, Category: v.prefix, OperationID: v.prefix + ":refund-restore:" + r.RefundNo, Source: "payment-order:" + o.OrderNo, Subject: "payment:" + o.Channel, Reason: "支付退款失败恢复"}); err != nil {
					return p.Refund{}, fault.Wrap("PAYMENT_REFUND_RESTORE_PENDING", "渠道退款已拒绝，积分恢复待处理", err)
				}
			}
		}
		if err = recordPaymentAnomalyTx(ctx, tx, p.Anomaly{ID: "refund-failed:" + r.ID, OrderNo: o.OrderNo, Channel: o.Channel, Kind: "refund_failed", Detail: map[string]any{"refundNo": r.RefundNo}}); err != nil {
			return p.Refund{}, err
		}
	} else if r.Kind == "operator" {
		if _, err = o.Status.Transition(p.Refunded); err != nil {
			return p.Refund{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE drama_catalog_v1.payment_orders SET status='refunded',refunded_at=clock_timestamp() WHERE order_no=$1 AND status='credited'`, o.OrderNo); err != nil {
			return p.Refund{}, err
		}
	}
	r, err = scanPaymentRefund(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.payment_refunds SET status=$2,channel_refund_id=$3,finished_at=clock_timestamp(),channel_bill_at=coalesce(channel_bill_at,$4) WHERE id=$1 AND status='pending' RETURNING `+paymentRefundColumns, r.ID, result.State, result.ChannelRefundID, result.BillAt))
	if err != nil {
		return r, err
	}
	if err = recordPaymentEventTx(ctx, tx, p.Event{OrderNo: o.OrderNo, RefundID: r.ID, Source: source, Outcome: r.Status, OperatorID: r.OperatorID, Reason: r.Reason, Facts: map[string]any{"refundNo": r.RefundNo}}); err != nil {
		return p.Refund{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p.Refund{}, paymentUnknown()
	}
	return r, nil
}
func (s *Store) ClaimPaymentRefunds(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]p.Refund, error) {
	if limit < 1 || limit > 100 || lease <= 0 {
		return nil, fault.ErrInvalid
	}
	// coalesce列不能用简单前缀处理；RETURNING使用主表整行的子查询投影。
	rows, err := s.pool.Query(ctx, `WITH due AS (SELECT id FROM drama_catalog_v1.payment_refunds WHERE status='pending' AND next_query_at<=$1 ORDER BY next_query_at,id LIMIT $2 FOR UPDATE SKIP LOCKED), changed AS (UPDATE drama_catalog_v1.payment_refunds r SET next_query_at=$3,attempts=attempts+1 FROM due WHERE r.id=due.id RETURNING r.*) SELECT `+paymentRefundColumns+` FROM changed`, now, limit, now.Add(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []p.Refund{}
	for rows.Next() {
		v, e := scanPaymentRefund(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
