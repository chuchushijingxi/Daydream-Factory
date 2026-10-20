package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"drama-agent/internal/application/ports"
	md "drama-agent/internal/domain/modelconfig"
)

func TestMockBehaviorCASIndependentModelsAndProbabilityBoundaries(t *testing.T) {
	s, store, text := configFixture()
	c := configService(s)
	ctx := context.Background()
	actor := ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}
	in := mockInput(1)
	in.Connection.Models[0].MockBehavior = &md.MockBehavior{DelaySeconds: 2, FailurePercent: 50}
	in.Connection.Models[1].MockBehavior = &md.MockBehavior{DelaySeconds: 4, FailurePercent: 100}
	v, err := c.ForOperator(actor).Update(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := store.ModelConfigSnapshot(ctx)
	c.MockRandom = bytes.NewReader([]byte{49, 50})
	for i, want := range []bool{true, false} {
		binding, err := c.ResolveTask(ctx, md.MockImageModelID, "image.generate")
		var frozen md.MockExecution
		if err != nil || json.Unmarshal(binding.ProviderConfig, &frozen) != nil || frozen.DelaySeconds != 2 || frozen.FailurePercent != 50 || frozen.Fail != want {
			t.Fatal(i, frozen, err)
		}
	}
	// Neither endpoint probability consumes entropy, and the video has its own values.
	c.MockRandom = bytes.NewReader(nil)
	binding, err := c.ResolveTask(ctx, md.MockVideoModelID, "video.generate")
	var frozen md.MockExecution
	if err != nil || json.Unmarshal(binding.ProviderConfig, &frozen) != nil || frozen.DelaySeconds != 4 || !frozen.Fail {
		t.Fatal(frozen, err)
	}
	if _, err = c.ResolveTask(ctx, md.MockImageModelID, "image.generate"); err == nil {
		t.Fatal("entropy failure silently accepted")
	}
	after, _ := store.ModelConfigSnapshot(ctx)
	if !reflect.DeepEqual(before, after) || text.count != 0 {
		t.Fatal("binding resolution changed configuration or invoked text model")
	}
	_, err = c.ForOperator(actor).Update(ctx, in)
	if err == nil {
		t.Fatal("stale CAS accepted")
	}
	in = mockInput(v.Revision)
	in.Connection.Models[0].MockBehavior = &md.MockBehavior{DelaySeconds: 60}
	if _, err = c.ForOperator(actor).Update(ctx, in); err != nil {
		t.Fatal(err)
	}
	if frozen.DelaySeconds != 4 || !frozen.Fail {
		t.Fatal("previous binding changed")
	}
}

func TestMockBehaviorInvalidWritesAreAtomicAndDefaultsAreReadOnly(t *testing.T) {
	ctx := context.Background()
	actor := ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}
	for _, behavior := range []*md.MockBehavior{nil, {DelaySeconds: -1}, {DelaySeconds: 61}, {FailurePercent: -1}, {FailurePercent: 101}} {
		s, store, _ := configFixture()
		before, _ := store.ModelConfigSnapshot(ctx)
		in := mockInput(1)
		in.Connection.Models[1].MockBehavior = behavior
		_, err := configService(s).ForOperator(actor).Update(ctx, in)
		if err == nil {
			t.Fatal("invalid behavior accepted", behavior)
		}
		after, _ := store.ModelConfigSnapshot(ctx)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("invalid second model partially saved first")
		}
	}
	s, store, _ := configFixture()
	for i := range store.cfg.Connections {
		if store.cfg.Connections[i].ID == "mock" {
			for j := range store.cfg.Connections[i].Models {
				store.cfg.Connections[i].Models[j].MockBehavior = nil
			}
		}
	}
	before, _ := store.ModelConfigSnapshot(ctx)
	read, err := configService(s).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, conn := range read.Connections {
		if conn.ID == "mock" {
			for _, m := range conn.Models {
				if m.MockBehavior == nil || *m.MockBehavior != (md.MockBehavior{}) {
					t.Fatal("missing default")
				}
			}
		}
	}
	after, _ := store.ModelConfigSnapshot(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("GET persisted defaults")
	}
	in := enabledInput(1, nil)
	in.Connection.Models[0].MockBehavior = &md.MockBehavior{}
	if _, err = configService(s).ForOperator(actor).Update(ctx, in); err == nil {
		t.Fatal("non-Mock accepted behavior")
	}
	_, err = configService(s).ForOperator(ports.Actor{}).Update(ctx, mockInput(1))
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("missing Actor accepted")
	}
}
