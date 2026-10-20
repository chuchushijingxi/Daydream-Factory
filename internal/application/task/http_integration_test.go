package task_test

import (
	"context"
	"sync/atomic"
	"testing"

	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/application/modelconfig"
	md "drama-agent/internal/domain/modelconfig"
)

type changingConfiguration struct {
	*postgres.Store
	reads atomic.Int32
}

func (s *changingConfiguration) ModelConfigSnapshot(ctx context.Context) (md.ModelConfig, error) {
	cfg, e := s.Store.ModelConfigSnapshot(ctx)
	if s.reads.Add(1) > 1 {
		cfg.Revision++
		for i := range cfg.Connections {
			for j := range cfg.Connections[i].Models {
				cfg.Connections[i].Models[j].Enabled = false
			}
		}
	}
	return cfg, e
}
func TestModelConfigurationUsesOneSnapshotForTaskBinding(t *testing.T) {
	s, store := fixture(t)
	ctx := context.Background()
	cfg, e := store.ModelConfigSnapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	cfg = md.WithOfficialPresets(cfg)
	old := cfg.Revision
	cfg.Revision++
	for i := range cfg.Connections {
		if cfg.Connections[i].ID == "mock" {
			for j := range cfg.Connections[i].Models {
				cfg.Connections[i].Models[j].Enabled = true
			}
		}
	}
	if e = store.ModelConfigCommit(ctx, cfg, old); e != nil {
		t.Fatal(e)
	}
	changing := &changingConfiguration{Store: store}
	configuration := &modelconfig.Service{Store: changing, Tasks: s}
	s.Resolver = configuration
	input := command("snapshot", "image", 0)
	input.ModelID = md.MockImageModelID
	v, e := s.Submit(ctx, input)
	if e != nil {
		t.Fatal(e)
	}
	if changing.reads.Load() != 1 {
		t.Fatal("validated registry snapshot changed during binding", changing.reads.Load())
	}
	saved, e := s.Store.Task(ctx, v.ID)
	if e != nil || saved.State.Binding.RequestModelID != md.MockImageModelID {
		t.Fatal("wrong frozen binding", e)
	}
	again, e := s.Submit(ctx, input)
	if e != nil || again.ID != v.ID || changing.reads.Load() != 1 {
		t.Fatal("existing command depended on changed registry", e)
	}
}
