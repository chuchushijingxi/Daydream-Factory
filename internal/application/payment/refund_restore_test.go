package payment

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

func TestRefundRestorePendingRecordsOneFactAndRetriesOriginalRefund(t *testing.T) {
	s, store, _ := newFixture()
	store.orders["o"] = order("o", p.Credited)
	refund := p.Refund{ID: "request-id", RefundNo: "channel-refund-no", OrderNo: "o", Status: "pending", AmountFen: 50, RechargeCredits: 500, GiftCredits: 50}
	store.refunds[refund.RefundNo] = refund
	store.refundFinishErr = fault.Wrap("PAYMENT_REFUND_RESTORE_PENDING", "渠道退款已拒绝，积分恢复待处理", errors.New("private database cause and raw message"))
	result := ports.ChannelRefundResult{RefundNo: refund.RefundNo, OrderNo: refund.OrderNo, State: "failed"}
	var logs bytes.Buffer
	priorLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(priorLogger)
	auditCalls := 0
	store.auditContextCheck = func(ctx context.Context) {
		auditCalls++
		if ctx.Err() != nil {
			t.Errorf("audit inherited canceled request: %v", ctx.Err())
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) < 4*time.Second {
			t.Error("audit needs finite independent 5s budget")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 2 {
		out, err := s.ConfirmRefund(ctx, "demo", result, "query")
		requireCode(t, err, "PAYMENT_REFUND_RESTORE_PENDING")
		if out.Status != "pending" || store.refunds[refund.RefundNo].Status != "pending" || store.refundRestoreWrites != 0 {
			t.Fatal("restore failure finalized or partially restored refund")
		}
	}
	if len(store.events) != 1 || len(store.anomalies) != 1 || auditCalls != 4 {
		t.Fatalf("retry duplicated facts: events=%d anomalies=%d auditCalls=%d", len(store.events), len(store.anomalies), auditCalls)
	}
	event := store.events[0]
	anomaly := store.anomalies[0]
	stableID := "refund-restore-pending:" + refund.RefundNo
	if event.ID != stableID || anomaly.ID != stableID || event.RefundID != refund.ID || event.Outcome != "restore_pending" || anomaly.Kind != "refund_failed" || anomaly.Detail["stage"] != "restore_pending" || event.Facts["errorCode"] != "PAYMENT_REFUND_RESTORE_PENDING" {
		t.Fatalf("unsafe/missing restore evidence %+v %+v", event, anomaly)
	}
	if strings.Contains(logs.String(), "private database") || !strings.Contains(logs.String(), `"stage":"restore_pending"`) || !strings.Contains(logs.String(), `"payment_alert"`) {
		t.Fatalf("unsafe or missing alert %s", logs.String())
	}
	// 模拟其他消费腾出余额。应用必须仍交原退款号给事务，重复终态只回读原回执。
	store.refundFinishErr = nil
	store.auditContextCheck = nil
	for range 2 {
		out, err := s.ConfirmRefund(context.Background(), "demo", result, "query")
		if err != nil || out.Status != "failed" || out.ID != refund.ID {
			t.Fatalf("original refund did not recover %+v %v", out, err)
		}
	}
	if store.refundRestoreWrites != 1 {
		t.Fatalf("restore applied %d times", store.refundRestoreWrites)
	}
	for _, no := range store.refundFinishNos {
		if no != refund.RefundNo {
			t.Fatalf("retry changed refund identity %q", no)
		}
	}
	if len(store.events) != 1 || len(store.anomalies) != 1 {
		t.Fatal("successful retry duplicated restore-pending evidence")
	}
}
func TestRefundCommitUnknownDoesNotCreateRestoreFailureEvidence(t *testing.T) {
	s, store, _ := newFixture()
	store.orders["o"] = order("o", p.Credited)
	store.refunds["r"] = p.Refund{ID: "r", RefundNo: "r", OrderNo: "o", Status: "pending", AmountFen: 50}
	store.refundFinishErr = fault.New("PAYMENT_COMMIT_UNKNOWN", "提交结果不明")
	out, err := s.ConfirmRefund(context.Background(), "demo", ports.ChannelRefundResult{RefundNo: "r", OrderNo: "o", State: "failed"}, "query")
	requireCode(t, err, "PAYMENT_COMMIT_UNKNOWN")
	if out.Status != "pending" || len(store.events) != 0 || len(store.anomalies) != 0 || store.refundRestoreWrites != 0 {
		t.Fatal("unknown commit was classified as confirmed restoration failure")
	}
}
