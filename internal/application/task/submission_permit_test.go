package task

import (
	"context"
	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/task"
	"encoding/json"
	"errors"
	"testing"
)

type permitStore struct {
	ports.TaskStore
	saved    *d.Task
	released bool
}

func (s *permitStore) TaskInput(context.Context, string) (d.Snapshot, error) {
	return d.Snapshot{Input: json.RawMessage(`{"prompt":"private"}`)}, nil
}
func (s *permitStore) CommitTask(_ context.Context, t d.Task, _ uint64, _ string, release bool) (d.Task, error) {
	s.saved = &t
	s.released = release
	return t, nil
}

type testPermit struct {
	rejection *ports.TaskRejection
	err       error
}

func (p testPermit) ConfirmTaskSubmission(context.Context, d.Task, d.Snapshot) (*ports.TaskRejection, error) {
	return p.rejection, p.err
}

type forbidPermitProvider struct {
	ports.TaskProvider
	calls int
}

func (p *forbidPermitProvider) Prepare(context.Context, d.Binding, json.RawMessage) (json.RawMessage, error) {
	p.calls++
	return nil, errors.New("provider must not be reached")
}

func TestSubmissionPrerequisiteIsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		permit   ports.TaskSubmissionPermit
		terminal bool
	}{
		{"missing wiring", nil, false},
		{"unconfirmed prerequisite", testPermit{err: errors.New("unconfirmed transaction")}, false},
		{"confirmed rejection", testPermit{rejection: &ports.TaskRejection{Code: "CALLER_REJECTED", Message: "拒绝"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &permitStore{}
			provider := &forbidPermitProvider{}
			s := &Service{Store: store, Provider: provider, Permit: tc.permit}
			task := d.Task{ID: "task", Status: d.Queued, State: d.State{RequirePermit: true, Policy: d.Policy{MaxSubmissions: 1, RetryMillis: 5}}}
			err := s.submit(context.Background(), task)
			if provider.calls != 0 {
				t.Fatal("unconfirmed caller reached provider")
			}
			if tc.terminal {
				if err != nil || store.saved == nil || store.saved.Status != d.Failed || store.saved.State.ErrorCode != "CALLER_REJECTED" {
					t.Fatal("known rejection not persisted", err)
				}
			} else if err == nil {
				t.Fatal("missing prerequisite not surfaced")
			}
			if tc.permit != nil && !store.released {
				t.Fatal("recovery lease not released")
			}
			if tc.name == "unconfirmed prerequisite" && (store.saved.NextRunAt == nil || store.saved.State.ErrorCode != "TASK_PREREQUISITE_PENDING") {
				t.Fatal("unconfirmed transaction lost recoverable intent")
			}
		})
	}
}
