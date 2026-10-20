package project

import (
	"context"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/credits"
	"errors"
	"reflect"
	"testing"

	"drama-agent/internal/application/modelconfig"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
)

func mockInput(revision uint64) modelconfig.ModelConfigInput {
	conn, _ := md.OfficialPreset("mock")
	in := modelconfig.ModelConfigInput{Revision: revision, Connection: modelconfig.ConnectionInput{ID: conn.ID, Name: conn.Name, Kind: conn.Kind, Protocol: conn.Protocol}}
	for _, m := range conn.Models {
		on := true
		in.Connection.Models = append(in.Connection.Models, modelconfig.ModelEnableInput{ID: m.ID, ModelID: m.ModelID, Category: m.Category, Enabled: &on, CreditPrice: &credits.PriceRule{}, MockBehavior: &md.MockBehavior{}})
	}
	return in
}

func TestMockConfigurationFreeCandidatesAndNoProjectChanges(t *testing.T) {
	s, store, text := configFixture()
	c := configService(s)
	c.Tasks = &taskapp.Service{}
	ctx := context.Background()
	before, _ := s.Store.(*memory).List(ctx)
	in := mockInput(1)
	in.Connection.Models[0].Alias = "图片联调"
	v, err := c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := c.Options(ctx)
	if err != nil || len(opts.Analysis) != 0 || len(opts.Image) != 1 || len(opts.Video) != 1 || opts.Image[0].Name != "图片联调" {
		t.Fatal(opts, err)
	}
	for _, id := range []string{md.MockImageModelID, md.MockVideoModelID} {
		binding, err := c.ResolveTask(ctx, id, md.MockOperation(map[string]string{md.MockImageModelID: "image", md.MockVideoModelID: "video"}[id]))
		if err != nil || len(binding.ProviderConfig) == 0 {
			t.Fatal(err)
		}
		_, err = c.Resolve(ctx, id)
		if !errors.Is(err, fault.ErrModelUnavailable) {
			t.Fatal("mock routed into text model", err)
		}
	}
	after, _ := s.Store.(*memory).List(ctx)
	cfg, _ := store.ModelConfigSnapshot(ctx)
	if !reflect.DeepEqual(before, after) || cfg.HasCredentials() || cfg.Revision != v.Revision || text.count != 0 {
		t.Fatal("mock altered projects, credentials, revision, or called text model")
	}
	off := false
	in = mockInput(v.Revision)
	in.Connection.Models[0].Enabled = &off
	v, err = c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ResolveTask(ctx, md.MockImageModelID, "image.generate")
	if !errors.Is(err, fault.ErrModelUnavailable) {
		t.Fatal(err)
	}
	_, err = c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, mockInput(1))
	if !errors.Is(err, fault.ErrConflict) {
		t.Fatal(err)
	}
	c.ReadOnly = true
	_, err = c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, mockInput(v.Revision))
	expectCode(t, err, "SERVICE_READ_ONLY")
}

func TestMockBuiltinIdentityAndCredentialsCannotBeChanged(t *testing.T) {
	s, store, _ := configFixture()
	c := configService(s)
	ctx := context.Background()
	for _, change := range []func(*modelconfig.ModelConfigInput){
		func(in *modelconfig.ModelConfigInput) { in.Connection.ID = "" },
		func(in *modelconfig.ModelConfigInput) {
			in.Connection.Kind = "gateway"
			in.Connection.Protocol = "openai-chat"
			in.Connection.BaseURL = "https://example.test"
		},
		func(in *modelconfig.ModelConfigInput) { in.Connection.BaseURL = "https://example.test" },
		func(in *modelconfig.ModelConfigInput) { in.Connection.Models[0].Category = "analysis" },
		func(in *modelconfig.ModelConfigInput) { in.Connection.Models[0].ModelID = "paid-model" },
		func(in *modelconfig.ModelConfigInput) { key := "must-not-save"; in.APIKey = &key },
	} {
		in := mockInput(1)
		change(&in)
		_, err := c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
		expectCode(t, err, "BUILTIN_CONNECTION_FIXED")
	}
	_, err := c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Delete(ctx, "mock", 1)
	expectCode(t, err, "BUILTIN_CONNECTION_FIXED")
	cfg, _ := store.ModelConfigSnapshot(ctx)
	if cfg.Revision != 1 || cfg.HasCredentials() {
		t.Fatal("rejection changed config")
	}
}

func TestMockSavePreservesExistingCredentialsAndModelOverrides(t *testing.T) {
	s, store, _ := configFixture()
	store.cfg.Connections = store.cfg.Connections[:2]
	for i := range store.cfg.Connections {
		conn := &store.cfg.Connections[i]
		conn.Secret = []byte("existing encrypted credential")
		conn.Models = conn.Models[:1]
		conn.Models[0].Alias = "原模型别名"
		conn.Models[0].LogoID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		conn.Models[0].Enabled = true
	}
	before, _ := store.ModelConfigSnapshot(context.Background())
	if _, err := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(context.Background(), mockInput(before.Revision)); err != nil {
		t.Fatal(err)
	}
	after, _ := store.ModelConfigSnapshot(context.Background())
	for i, conn := range before.Connections {
		current := after.Connections[i]
		if !reflect.DeepEqual(conn.Secret, current.Secret) {
			t.Fatal("changed existing encrypted credential")
		}
		old, saved := conn.Models[0], current.Models[0]
		if old.ID != saved.ID || old.ModelID != saved.ModelID || old.Alias != saved.Alias || old.LogoID != saved.LogoID || old.Enabled != saved.Enabled {
			t.Fatal("changed existing model identity or user overrides")
		}
	}
}
