package project

import (
	"context"
	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

type batchMediaLedger struct {
	ports.CreditLedger
	calls   int
	entries map[string]c.Entry
	err     error
}

func (l *batchMediaLedger) CreditOperations(_ context.Context, ids []string) (map[string]c.Entry, error) {
	l.calls++
	return l.entries, l.err
}
func (l *batchMediaLedger) CreditOperation(context.Context, string) (c.Entry, error) {
	panic("per-row credit query")
}
func TestMediaListBatchesCreditsAndKeepsSettlementFailures(t *testing.T) {
	for _, count := range []int{0, 1, 100} {
		for _, mode := range []string{"paid", "free", "pending", "unknown-paid", "refund-pending", "refund", "missing", "owner-conflict", "refund-conflict", "unavailable"} {
			t.Run(fmt.Sprintf("%d/%s", count, mode), func(t *testing.T) {
				ctx := context.Background()
				p := d.Project{ID: "batch", OwnerID: "owner", Revision: 1, ScriptSource: d.ScriptSource{Version: 1}, Assets: []d.Asset{{ID: "prop", Kind: "prop"}}, VisualPrompts: []d.VisualPrompt{{VisualPromptInput: d.VisualPromptInput{Target: d.VisualTarget{AssetID: "prop"}, Text: "钥匙"}, Origin: "user", SourceVersion: 1}}}
				projects := newMemory()
				if err := projects.Create(ctx, p); err != nil {
					t.Fatal(err)
				}
				tasks := &historyTaskStore{previewTaskStore: &previewTaskStore{inputs: map[string]td.Snapshot{}}}
				ledger := &batchMediaLedger{entries: map[string]c.Entry{}}
				for i := 0; i < count; i++ {
					id := fmt.Sprint(i)
					charge := &c.Charge{AccountID: p.OwnerID, ActorID: p.OwnerID, DiscountPercent: 100, ID: id, Amount: 1}
					status := td.Succeeded
					if mode == "free" {
						charge.Amount = 0
					}
					if mode == "pending" || mode == "unknown-paid" {
						status = td.Unknown
					}
					if mode == "refund" || mode == "refund-conflict" || mode == "refund-pending" {
						status = td.Failed
					}
					change := generationDebit(p.ID, charge)
					debit := c.Entry{ID: "debit" + id, OperationID: change.OperationID, AccountID: p.OwnerID, Kind: "deduct", Amount: 1, Source: p.ID, Subject: p.OwnerID, Reason: "generation"}
					if mode != "missing" && mode != "pending" {
						ledger.entries[debit.OperationID] = debit
					}
					if mode == "owner-conflict" {
						debit.AccountID = "other"
						ledger.entries[debit.OperationID] = debit
					}
					if mode == "refund" || mode == "refund-conflict" {
						refund := generationRefund(charge.ID, debit)
						entry := c.Entry{ID: "refund" + id, OperationID: refund.OperationID, Kind: "refund", AccountID: p.OwnerID, Amount: refund.Amount, OriginalDeductionID: debit.ID, Source: p.ID, Subject: refund.Subject, Reason: refund.Reason}
						if mode == "refund-conflict" {
							entry.Amount = 2
						}
						ledger.entries[entry.OperationID] = entry
					}
					meta, _ := json.Marshal(MediaContext{Request: MediaCommand{Target: MediaTarget{AssetID: "prop"}, Revision: 1}, Credit: charge, SourceVersion: 1})
					tasks.rows = append(tasks.rows, td.Task{ID: id, OwnerID: p.OwnerID, ScopeID: mediaScope(p.ID), Status: status, CreatedAt: time.Unix(1000-int64(i), 0), State: td.State{Operation: "image.generate"}})
					tasks.inputs[id] = td.Snapshot{Input: json.RawMessage(`{}`), CallerContext: meta}
				}
				if mode == "unavailable" {
					ledger.err = errors.New("ledger offline")
				}
				service := &Service{Store: projects, MediaTasks: &taskapp.Service{Store: tasks}, Credits: ledger}
				list, err := service.ListMedia(ctx, p.ID, "", 100, "image")
				wantCalls := 1
				if count == 0 || mode == "free" {
					wantCalls = 0
				}
				if ledger.calls != wantCalls {
					t.Fatal("ledger queries", ledger.calls, wantCalls)
				}
				wantError := count > 0 && (mode == "missing" || mode == "owner-conflict" || mode == "refund-conflict" || mode == "unavailable")
				if wantError {
					if err == nil {
						t.Fatal("hidden ledger failure", list)
					}
					if mode == "owner-conflict" || mode == "refund-conflict" {
						if !errors.Is(err, fault.ErrConflict) {
							t.Fatal(err)
						}
					}
					return
				}
				if err != nil || len(list.Items) != count {
					t.Fatal(list, err)
				}
				for _, item := range list.Items {
					credit := item.Credit
					if credit == nil {
						t.Fatal("missing basis")
					}
					if mode == "paid" && (credit.Charged != 1 || credit.Prepaid != 1 || credit.Settlement != "settled") {
						t.Fatal(credit)
					}
					if mode == "refund" && (credit.Refunded != 1 || credit.Charged != 0 || credit.Settlement != "refunded") {
						t.Fatal(credit)
					}
					if mode == "unknown-paid" && (credit.Settlement != "pending" || credit.Prepaid != 1 || credit.Refunded != 0) {
						t.Fatal("unknown reserve lost", credit)
					}
					if mode == "refund-pending" && (credit.Settlement != "refund-pending" || credit.Prepaid != 1 || credit.Refunded != 0) {
						t.Fatal("refund evidence invented", credit)
					}
					if mode == "pending" && (credit.Settlement != "pending" || credit.Prepaid != 0) {
						t.Fatal(credit)
					}
				}
			})
		}
	}
}
