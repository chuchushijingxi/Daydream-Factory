package task

import (
	"testing"
	"time"

	d "drama-agent/internal/domain/task"
)

// 终态任务的费用结算轮询必须在保存期限内停止，不能对永久丢失费用信息的
// 厂商任务无限查询；未知费用证据保留在已存提交里。
func TestSettlementPollingStopsAtSaveDeadline(t *testing.T) {
	s := &Service{}
	newTask := func(saveUntil time.Time) d.Task {
		return d.Task{
			ID: "task_1", Status: d.Cancelled,
			State: d.State{
				Operation: "image.generate",
				Policy:    d.Policy{PollMillis: 1000, SaveUntil: saveUntil},
				Binding:   d.Binding{Free: false},
				Submissions: []d.Submission{{
					ID: "submission_1", Disposition: "accepted",
					Receipt: &d.Receipt{Outcome: "accepted", Remote: &d.Remote{ID: "p-1"}, Billing: d.Billing{Status: "pending"}},
				}},
			},
		}
	}
	pending := d.Receipt{Outcome: "pending", Billing: d.Billing{Status: "pending"}}

	t.Run("known billing settles and stops", func(t *testing.T) {
		tk := newTask(time.Now().Add(time.Hour))
		if err := s.apply(&tk, d.Receipt{Outcome: "pending", Billing: d.Billing{Status: "known", KnownMicroCNY: 42}}, true); err != nil {
			t.Fatal(err)
		}
		if tk.NextRunAt != nil {
			t.Fatal("settled task must stop polling")
		}
		if tk.State.Submissions[0].Receipt.Billing.KnownMicroCNY != 42 {
			t.Fatal("known billing must merge into the original submission receipt")
		}
	})
	t.Run("unknown billing polls within deadline", func(t *testing.T) {
		tk := newTask(time.Now().Add(time.Hour))
		if err := s.apply(&tk, pending, true); err != nil {
			t.Fatal(err)
		}
		if tk.NextRunAt == nil {
			t.Fatal("unsettled task within deadline must keep polling")
		}
	})
	t.Run("unknown billing stops after deadline", func(t *testing.T) {
		tk := newTask(time.Now().Add(-time.Hour))
		if err := s.apply(&tk, pending, true); err != nil {
			t.Fatal(err)
		}
		if tk.NextRunAt != nil {
			t.Fatal("unsettled task past deadline must stop polling")
		}
		if tk.Status != d.Cancelled {
			t.Fatal("settlement must not revive or rewrite a terminal task")
		}
		if got := tk.Billing(); got.Status == "known" || got.KnownMicroCNY != 0 {
			t.Fatalf("deadline must not fabricate fees, got %q/%d", got.Status, got.KnownMicroCNY)
		}
	})
}
