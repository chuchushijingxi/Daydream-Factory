package project

import (
	"context"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/redisruntime"
)

type observingBus struct {
	ports.UpdateBus
	ready chan struct{}
}

func (b observingBus) Watch(ctx context.Context, key string) (<-chan struct{}, func(), error) {
	ch, close, err := b.UpdateBus.Watch(ctx, key)
	if err == nil {
		select {
		case b.ready <- struct{}{}:
		default:
		}
	}
	return ch, close, err
}

func TestRemoteCancellationUsesRedisHintAndKeepsAutomaticContinuation(t *testing.T) {
	runtime := redisruntime.Connect(t)
	publisher := redisruntime.Connect(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newMemory()
	id := identity.New("remote_cancel")
	p := d.Project{ID: id, Revision: 1, Runs: []d.Run{{ID: "first", Status: "running"}}}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	workers := NewWorkers(&Service{Store: store})
	workers.Updates = observingBus{UpdateBus: runtime, ready: ready}
	done := make(chan struct{})
	go func() { defer close(done); workers.watchCancellation(ctx, id, "first", cancel) }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("remote watcher not ready")
	}
	// A completed run can automatically continue under the same local worker.
	p.Runs[0].Status = "completed"
	p.Runs = append(p.Runs, d.Run{ID: "second", Status: "running"})
	p.Revision++
	if err := store.Commit(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(ctx, "project:"+id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("automatic continuation was cancelled")
	case <-time.After(100 * time.Millisecond):
	}
	p.Runs[1].Status = "cancelled"
	p.Revision++
	if err := store.Commit(ctx, p, 2); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(ctx, "project:"+id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("another node's cancellation did not stop local execution")
	}
	<-done
}

func TestCancellationReconcilesWhenRedisHintIsMissed(t *testing.T) {
	runtime := redisruntime.Connect(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newMemory()
	id := identity.New("missed_cancel")
	p := d.Project{ID: id, Revision: 1, Runs: []d.Run{{ID: "run", Status: "running"}}}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	workers := NewWorkers(&Service{Store: store})
	workers.Updates = observingBus{UpdateBus: runtime, ready: ready}
	done := make(chan struct{})
	go func() { defer close(done); workers.watchCancellation(ctx, id, "run", cancel) }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("watcher not ready")
	}
	p.Runs[0].Status = "cancelled"
	p.Revision++
	if err := store.Commit(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("missed hint bypassed durable cancellation")
	}
	<-done
}

func TestRecoveryWaitsForPredecessorExecutionLease(t *testing.T) {
	model := &fakeModel{generate: func(ports.Request) (ports.Response, error) { return response(`{"assets":[]}`), nil }}
	service, p := fixture(t, model)
	store := service.Store.(*memory)
	store.mu.Lock()
	store.locked[p.ID] = true
	store.mu.Unlock()
	workers := NewWorkers(service)
	defer workers.Shutdown(context.Background())
	workers.Launch(p.ID)
	time.Sleep(50 * time.Millisecond)
	store.mu.Lock()
	delete(store.locked, p.ID)
	store.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := workers.WaitUntilIdle(ctx); err != nil {
		t.Fatal("recovery did not retry after lease release", err)
	}
	if model.count == 0 {
		t.Fatal("recovery treated a held lease as completed execution")
	}
}
