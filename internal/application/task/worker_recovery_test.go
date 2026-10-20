package task

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/task"
)

type recoveryStore struct {
	ports.TaskStore
	task       d.Task
	commits    int
	inputReads int
}

func (s *recoveryStore) ClaimTask(_ context.Context, id, token string, lease time.Duration) (d.Task, bool, error) {
	if id != s.task.ID || s.task.NextRunAt == nil {
		return d.Task{}, false, nil
	}
	s.task.Revision++
	s.task.LeaseToken = token
	until := time.Now().Add(lease)
	s.task.LeaseUntil = &until
	return s.task, true, nil
}

func (s *recoveryStore) CommitTask(_ context.Context, task d.Task, revision uint64, token string, release bool) (d.Task, error) {
	if revision != s.task.Revision || token != s.task.LeaseToken {
		return d.Task{}, errors.New("stale task claim")
	}
	task.Revision++
	if release {
		task.LeaseToken, task.LeaseUntil = "", nil
	}
	s.task = task
	s.commits++
	return task, nil
}

func (s *recoveryStore) RenewTask(context.Context, string, string, time.Duration) error {
	return nil
}

func (s *recoveryStore) TaskInput(context.Context, string) (d.Snapshot, error) {
	s.inputReads++
	return d.Snapshot{Input: json.RawMessage(`{"prompt":"frozen input"}`)}, nil
}

type recoveryProvider struct {
	ports.TaskProvider
	prepares, submits, queries int
}

func (p *recoveryProvider) Prepare(_ context.Context, _ d.Binding, input json.RawMessage) (json.RawMessage, error) {
	p.prepares++
	return input, nil
}

func (p *recoveryProvider) Submit(context.Context, d.Binding, string, json.RawMessage) (d.Receipt, error) {
	p.submits++
	return d.Receipt{Outcome: "accepted", Remote: &d.Remote{ID: "original-remote"}, Billing: d.Billing{Status: "known"}}, nil
}

func (p *recoveryProvider) Query(context.Context, d.Binding, d.Remote) (d.Receipt, error) {
	p.queries++
	return d.Receipt{Outcome: "pending", Billing: d.Billing{Status: "known"}}, nil
}

type recoveryPermit struct{ calls int }

func (p *recoveryPermit) ConfirmTaskSubmission(context.Context, d.Task, d.Snapshot) (*ports.TaskRejection, error) {
	p.calls++
	return nil, nil
}

type recoveryCapacity struct{ calls int }

func (c *recoveryCapacity) AcquireTaskCapacity(ctx context.Context, _ d.Binding, _ string, _ int) (ports.Lease, bool, error) {
	c.calls++
	return ports.Lease{Context: ctx, Check: func(context.Context) error { return nil }, Release: func() {}}, true, nil
}

func newRecoveryService(task d.Task) (*Service, *recoveryStore, *recoveryProvider, *recoveryPermit, *recoveryCapacity) {
	store := &recoveryStore{task: task}
	provider, permit, capacity := &recoveryProvider{}, &recoveryPermit{}, &recoveryCapacity{}
	s := &Service{
		Store: store, Provider: provider, Permit: permit, Capacity: capacity,
		Lease: time.Hour, EvidenceTimeout: time.Second, CapacityRetry: time.Millisecond,
		SubmitConcurrency: 1, QueryConcurrency: 1, SaveConcurrency: 1,
	}
	return s, store, provider, permit, capacity
}

func recoverableTask() d.Task {
	now := time.Now().UTC()
	due := now.Add(-time.Minute)
	return d.Task{
		ID: "original-task", ScopeID: "original-scope", CommandID: "original-command",
		Status: d.Queued, Revision: 1, NextRunAt: &due,
		State: d.State{
			RequirePermit: true, Binding: d.Binding{Free: true},
			Policy: d.Policy{ObserveUntil: now.Add(-time.Hour), SaveUntil: now.Add(time.Hour), MaxSubmissions: 3, PollMillis: 100, RetryMillis: 100},
		},
	}
}

func TestDriveNeverSubmittedExpiryStopsBeforePermitAndSend(t *testing.T) {
	task := recoverableTask()
	s, store, provider, permit, capacity := newRecoveryService(task)
	if err := s.Drive(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	got := store.task
	if got.Status != d.Failed || got.State.ErrorCode != "TASK_START_EXPIRED" || got.State.EndedAt == nil || got.NextRunAt != nil {
		t.Fatalf("expired unsent task must fail and stop: %+v", got)
	}
	// 媒体扣款只在提交前置条件确认时触发；不能先扣款再靠失败返还弥补过期检查。
	if permit.calls != 0 || store.inputReads != 0 || capacity.calls != 0 || provider.prepares != 0 || provider.submits != 0 || provider.queries != 0 {
		t.Fatalf("expired task reached submission prerequisites or provider: permit=%d input=%d capacity=%d provider=%+v", permit.calls, store.inputReads, capacity.calls, provider)
	}
	if len(got.State.Submissions) != 0 || got.State.Policy != task.State.Policy || got.CommandID != task.CommandID || got.LeaseToken != "" {
		t.Fatal("expiry changed frozen policy/command or created a submission/kept lease")
	}
	if err := s.Drive(context.Background(), task.ID); err != nil || store.commits != 1 {
		t.Fatal("duplicate delivery repeated expired task processing", err)
	}
}

func TestDriveWithinStartDeadlineStillSubmits(t *testing.T) {
	task := recoverableTask()
	task.State.Policy.ObserveUntil = time.Now().Add(time.Hour)
	s, store, provider, permit, capacity := newRecoveryService(task)
	if err := s.Drive(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	if store.task.Status != d.Running || len(store.task.State.Submissions) != 1 || permit.calls != 1 || provider.prepares != 1 || provider.submits != 1 || provider.queries != 0 || capacity.calls != 1 {
		t.Fatalf("valid queued task did not follow normal submission: %+v", store.task)
	}
	if store.task.State.Policy != task.State.Policy {
		t.Fatal("normal start changed frozen policy")
	}
}

func TestDriveConsumesPersistedFailureAfterObservationExpiry(t *testing.T) {
	task := recoverableTask()
	task.Status = d.Unknown
	task.State.Submissions = []d.Submission{{ID: "original-submission", Disposition: d.DispositionAccepted, Receipt: &d.Receipt{Outcome: "accepted", Remote: &d.Remote{ID: "original-remote"}, Billing: d.Billing{Status: "known"}}}}
	task.State.LastObservation = &d.Observation{ID: "original-observation", At: time.Now(), Receipt: d.Receipt{Outcome: "failed", ErrorCode: "PROVIDER_REJECTED", Error: "原任务已确认失败", Raw: json.RawMessage(`{"status":"failed"}`), Billing: d.Billing{Status: "known"}}}
	s, store, provider, permit, capacity := newRecoveryService(task)
	if err := s.Drive(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	got := store.task
	if got.Status != d.Failed || got.State.ErrorCode != "PROVIDER_REJECTED" || got.State.Error != "原任务已确认失败" || got.NextRunAt != nil || got.State.EndedAt == nil {
		t.Fatalf("persisted provider failure was ignored: %+v", got)
	}
	if provider.prepares != 0 || provider.submits != 0 || provider.queries != 0 || permit.calls != 0 || capacity.calls != 0 || store.inputReads != 0 {
		t.Fatal("saved failure must be adopted without another provider call or permit")
	}
	if !reflect.DeepEqual(got.State.LastObservation, task.State.LastObservation) || !reflect.DeepEqual(got.State.Submissions, task.State.Submissions) || got.State.Policy != task.State.Policy {
		t.Fatal("failure adoption changed original evidence or deadlines")
	}
}

func TestDriveLateFailureDoesNotRewriteTerminalTask(t *testing.T) {
	for _, status := range []d.Status{d.Cancelled, d.Succeeded} {
		t.Run(string(status), func(t *testing.T) {
			task := recoverableTask()
			task.Status = status
			ended := time.Now().Add(-time.Hour)
			task.State.EndedAt = &ended
			task.State.ErrorCode = "original-error"
			task.State.Result = &d.Result{Data: json.RawMessage(`{"original":true}`)}
			task.State.LastObservation = &d.Observation{ID: "late-failure", Receipt: d.Receipt{Outcome: "failed", ErrorCode: "LATE_FAILURE"}}
			s, store, provider, permit, _ := newRecoveryService(task)
			if err := s.Drive(context.Background(), task.ID); err != nil {
				t.Fatal(err)
			}
			if store.task.Status != status || !reflect.DeepEqual(store.task.State, task.State) || store.task.NextRunAt != nil || provider.queries != 0 || provider.submits != 0 || permit.calls != 0 {
				t.Fatal("late failure rewrote terminal state, result or evidence")
			}
		})
	}
}

func TestDriveUnconfirmedSubmissionNeverResendsAfterExpiry(t *testing.T) {
	for _, tc := range []struct {
		name string
		sub  *d.Submission
	}{
		{"unknown without submission", nil},
		{"may have sent", &d.Submission{ID: "original-submission", Disposition: d.DispositionMayHaveSent}},
		{"historical unknown receipt", &d.Submission{ID: "original-submission", Disposition: d.DispositionPrepared, Receipt: &d.Receipt{Outcome: "unknown"}}},
		{"accepted remote without final observation", &d.Submission{ID: "original-submission", Disposition: d.DispositionAccepted, Receipt: &d.Receipt{Outcome: "accepted", Remote: &d.Remote{ID: "original-remote"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := recoverableTask()
			task.Status = d.Unknown
			if tc.sub != nil {
				task.State.Submissions = []d.Submission{*tc.sub}
			}
			s, store, provider, permit, _ := newRecoveryService(task)
			if err := s.Drive(context.Background(), task.ID); err != nil {
				t.Fatal(err)
			}
			if store.task.Status != d.Unknown || store.task.NextRunAt != nil || provider.queries != 0 || provider.prepares != 0 || provider.submits != 0 || permit.calls != 0 {
				t.Fatalf("uncertain submission must stay unknown and stop: %+v", store.task)
			}
			if !reflect.DeepEqual(store.task.State.Submissions, task.State.Submissions) || store.task.State.Policy != task.State.Policy {
				t.Fatal("unknown recovery changed submission evidence or frozen deadline")
			}
		})
	}
}
