package task

import "testing"

func TestStateMachineAndTerminalEvidence(t *testing.T) {
	states := []Status{Queued, Running, Unknown, Succeeded, Failed, Cancelled}
	allowed := map[Status][]Status{Queued: {Running, Failed, Cancelled}, Running: {Unknown, Succeeded, Failed, Cancelled}, Unknown: {Running, Failed, Cancelled}}
	for _, from := range states {
		for _, to := range states {
			want := from == to
			for _, v := range allowed[from] {
				want = want || v == to
			}
			task := Task{Status: from}
			if (task.Transition(to) == nil) != want {
				t.Fatalf("%s -> %s", from, to)
			}
		}
	}
	task := Task{Status: Cancelled}
	task.Observe(Observation{ID: "late", Receipt: Receipt{Outcome: "completed", Billing: Billing{KnownMicroCNY: 10}}})
	task.Observe(Observation{ID: "next", Receipt: Receipt{Outcome: "pending"}})
	if task.Status != Cancelled || len(task.State.RetainedObservations) != 1 {
		t.Fatal("late evidence revived terminal task or lost receipt")
	}
}

func TestUnchangedFeeDoesNotCreatePollingSnapshots(t *testing.T) {
	task := Task{Status: Running}
	for i := 0; i < 100; i++ {
		task.Observe(Observation{ID: string(rune(i + 100)), Receipt: Receipt{Outcome: "pending", Billing: Billing{Status: "known", KnownMicroCNY: 123}}})
	}
	if len(task.State.RetainedObservations) != 1 {
		t.Fatal("unchanged fee duplicated every poll", len(task.State.RetainedObservations))
	}
}

func TestReceiptDispositionIsSingleSource(t *testing.T) {
	cases := map[string]string{"accepted": "accepted", "completed": "immediate", "failed": "rejected", "not_sent": "not_sent", "pending": "may_have_sent", "unknown": "may_have_sent", "": "may_have_sent"}
	for outcome, want := range cases {
		if got := (Receipt{Outcome: outcome}).Disposition(); got != want {
			t.Fatalf("outcome %q: got %q want %q", outcome, got, want)
		}
	}
}
