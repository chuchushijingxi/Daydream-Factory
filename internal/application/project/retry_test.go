package project

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	d "drama-agent/internal/domain/project"
)

type interruptedFixture struct{}

func (interruptedFixture) Error() string { return "typed response interruption" }

func (interruptedFixture) ResponseInterrupted() bool { return true }

func TestInterruptedOutputDiscardedAndCurrentDialoguePreserved(t *testing.T) {
	m := &fakeModel{window: 1000000, output: 32}
	var interruptedRequest ports.Request
	m.generate = func(req ports.Request) (ports.Response, error) {
		switch m.count {
		case 1:
			return response(`{"assets":[{"kind":"character","name":"祖父","description":"姓名未知","sourceIds":["s1"]},{"kind":"prop","name":"铜钥匙","sourceIds":["s1"]},{"kind":"prop","name":"另一把铜钥匙","sourceIds":["s2"]}]}`), nil
		case 2:
			var input struct {
				Assets []d.Asset `json:"assets"`
			}
			json.Unmarshal([]byte(req.Input), &input)
			input.Assets[0].Description = "姓名未说明，坐在工具间"
			raw, _ := json.Marshal(CatalogChangeSet{Assets: catalogInputs(input.Assets[:1])})
			out := response(string(raw))
			out.Actions = out.Actions[:1]
			out.Actions[0].ID = "correct-save"
			out.Reasoning = "complete reasoning"
			return out, nil
		case 3:
			interruptedRequest = req
			// Decoded-looking fields from a broken response must not become
			// actions, tool feedback, or assistant history.
			out := response(`{"assets":[],"removeIds":["do-not-adopt"]}`)
			out.Text, out.Reasoning, out.Raw, out.Usage = "partial", "partial reasoning", json.RawMessage(`{"choices":[`), billing.Usage{}
			return out, interruptedFixture{}
		case 4:
			if !reflect.DeepEqual(req, interruptedRequest) || len(req.Dialogue) != 2 || req.Dialogue[0].Reasoning != "complete reasoning" {
				t.Fatal("retry changed source, catalog, tool receipt or current assistant/tool conversation")
			}
			out := response(`{"assets":[]}`)
			out.Actions = out.Actions[1:]
			out.Actions[0].ID = "correct-finish"
			return out, nil
		default:
			t.Fatal("unexpected model attempt")
			return ports.Response{}, errors.New("extra request")
		}
	}
	s, p := fixture(t, m)
	if err := s.Execute(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Store.Snapshot(context.Background(), p.ID)
	r := got.LatestRun()
	c := r.Calls[2]
	if r.Status != "completed" || len(got.Assets) != 3 || !reflect.DeepEqual(got.Assets, r.Calls[1].After) || c.Status != "unknown" || c.ErrorCode != interruptionCode || len(c.Actions) != 0 || c.Text != "" || c.Reasoning != "" || c.Response != `{"choices":[` || got.UsedMicroCNY() != 1080+c.ReservedMicroCNY {
		t.Fatal("partial response adopted, correct content lost or unknown fees released", got)
	}
	for i, a := range got.Assets {
		if a.ID != r.Calls[0].After[i].ID {
			t.Fatal("saved identity changed")
		}
	}
}

func TestInterruptedAttemptsBoundedAndPersistedAcrossRecovery(t *testing.T) {
	for _, recovered := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "recovered"}[recovered], func(t *testing.T) {
			m := &fakeModel{window: 1000000, output: 32, generate: func(ports.Request) (ports.Response, error) {
				return ports.Response{Raw: json.RawMessage(`{"partial":`)}, interruptedFixture{}
			}}
			s, p := fixture(t, m)
			if recovered {
				p.LatestRun().End = 2
				p.LatestRun().Calls = []d.Call{{ID: "already-sent", Stage: "extract", End: 2, Status: "unknown", ErrorCode: interruptionCode, ReservedMicroCNY: 100}}
				if _, err := s.commit(context.Background(), p, p.Revision); err != nil {
					t.Fatal(err)
				}
			}
			if s.Execute(context.Background(), p.ID) == nil {
				t.Fatal("interrupted run marked successful")
			}
			got, _ := s.Store.Snapshot(context.Background(), p.ID)
			if len(got.LatestRun().Calls) != 3 || m.count != 3-map[bool]int{false: 0, true: 1}[recovered] || got.LatestRun().ErrorCode != interruptionCode || len(got.Assets) != 0 {
				t.Fatal("retry count reset or partial output adopted")
			}
			var reserved int64
			ids := map[string]bool{}
			for _, c := range got.LatestRun().Calls {
				reserved += c.ReservedMicroCNY
				if ids[c.ID] || c.Usage.Known || len(c.Actions) > 0 {
					t.Fatal("attempt accounting invalid")
				}
				ids[c.ID] = true
			}
			if got.UsedMicroCNY() != reserved {
				t.Fatal("unknown fees released")
			}
			if err := s.Execute(context.Background(), p.ID); err != nil || m.count != 3-map[bool]int{false: 0, true: 1}[recovered] {
				t.Fatal("terminal run resent")
			}
		})
	}
}

func TestInterruptedRetryChecksBudgetAndCancellation(t *testing.T) {
	for _, scenario := range []string{"budget", "context-cancel", "cancel-and-new-run"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m := &fakeModel{}
			s, p := fixture(t, m)
			if scenario == "budget" {
				p.LimitMicroCNY = 100000
				if _, err := s.commit(ctx, p, p.Revision); err != nil {
					t.Fatal(err)
				}
			}
			var cancelledRunID string
			m.generate = func(ports.Request) (ports.Response, error) {
				if scenario == "context-cancel" {
					cancel()
				}
				if scenario == "cancel-and-new-run" {
					current, _ := s.Store.Snapshot(ctx, p.ID)
					current, err := s.Cancel(ctx, p.ID, current.Revision)
					if err != nil {
						t.Fatal(err)
					}
					current, err = s.Start(ctx, p.ID, current.Revision)
					if err == nil {
						t.Fatal("cancelled project reopened")
					}
					cancelledRunID = current.LatestRun().ID
				}
				return ports.Response{Raw: json.RawMessage(`{"partial":`)}, interruptedFixture{}
			}
			if s.Execute(ctx, p.ID) == nil {
				t.Fatal("interruption returned success")
			}
			got, _ := s.Store.Snapshot(context.Background(), p.ID)
			if m.count != 1 || got.UsedMicroCNY() == 0 {
				t.Fatal("cancelled/unaffordable retry sent or reservation lost")
			}
			if scenario == "budget" && got.LatestRun().ErrorCode != "BUDGET_EXHAUSTED" {
				t.Fatal("missing budget boundary")
			}
			if scenario == "cancel-and-new-run" && (got.LatestRun().ID != cancelledRunID || got.LatestRun().Status != "cancelled" || len(got.LatestRun().Calls) != 1) {
				t.Fatal("old retry entered or failed replacement run")
			}
		})
	}
}
