package milvusevidence

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

type lazyFixtureIndex struct{ ensures atomic.Int32 }

func (s *lazyFixtureIndex) Ensure(context.Context, ports.EvidenceDocument) error {
	s.ensures.Add(1)
	return nil
}

func (s *lazyFixtureIndex) Search(context.Context, ports.EvidenceDocument, string, int, int) ([]ports.EvidenceHit, bool, error) {
	return []ports.EvidenceHit{{SpanID: "fictional-source", Score: .8}}, true, nil
}

func TestLazyIndexDoesNotInitializeUntilUsedAndRetriesFailedInitialization(t *testing.T) {
	var attempts atomic.Int32
	index := &lazyFixtureIndex{}
	lazy := NewLazy(func() (ports.EvidenceIndex, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("fictional local model files missing")
		}
		return index, nil
	})
	if attempts.Load() != 0 {
		t.Fatal("ordinary construction initialized evidence resources")
	}
	var unavailable *fault.Error
	if err := lazy.Ensure(context.Background(), ports.EvidenceDocument{}); !errors.As(err, &unavailable) || unavailable.Code != "EVIDENCE_UNAVAILABLE" {
		t.Fatal("missing resources not reported explicitly", err)
	}
	if err := lazy.Ensure(context.Background(), ports.EvidenceDocument{}); err != nil {
		t.Fatal("resources repaired for a new task cannot initialize", err)
	}
	hits, more, err := lazy.Search(context.Background(), ports.EvidenceDocument{}, "fictional-query", 1, 0)
	if err != nil || len(hits) != 1 || !more || attempts.Load() != 2 || index.ensures.Load() != 1 {
		t.Fatal("successful index not reused or result changed", hits, more, err)
	}
}

func TestLazyIndexConcurrentInitializationAndCancellation(t *testing.T) {
	var attempts atomic.Int32
	index := &lazyFixtureIndex{}
	lazy := NewLazy(func() (ports.EvidenceIndex, error) {
		attempts.Add(1)
		return index, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := lazy.Ensure(ctx, ports.EvidenceDocument{}); !errors.Is(err, context.Canceled) || attempts.Load() != 0 {
		t.Fatal("cancelled task initialized resources", err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := lazy.Ensure(context.Background(), ports.EvidenceDocument{}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if attempts.Load() != 1 || index.ensures.Load() != 16 {
		t.Fatal("concurrent initialization repeated", attempts.Load(), index.ensures.Load())
	}
}

func TestLazyIndexNeverTreatsAbsentIndexAsReady(t *testing.T) {
	for _, lazy := range []*Lazy{NewLazy(nil), NewLazy(func() (ports.EvidenceIndex, error) { return nil, nil })} {
		if _, _, err := lazy.Search(context.Background(), ports.EvidenceDocument{}, "fictional-query", 1, 0); err == nil {
			t.Fatal("absent index silently succeeded")
		}
	}
}
