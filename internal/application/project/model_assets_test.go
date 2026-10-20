package project

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
)

type logoMemory map[string][]byte

func (l logoMemory) SaveModelLogo(_ context.Context, id string, raw []byte) error {
	l[id] = bytes.Clone(raw)
	return nil
}

func (l logoMemory) ModelLogo(_ context.Context, id string) ([]byte, error) {
	v, ok := l[id]
	if !ok {
		return nil, fault.ErrNotFound
	}
	return bytes.Clone(v), nil
}

func testLogo(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 30, 12))
	for x := 0; x < 30; x++ {
		img.SetNRGBA(x, 5, color.NRGBA{20, 140, 200, 255})
	}
	var out bytes.Buffer
	if e := png.Encode(&out, img); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}

func TestOfficialConfigurationIsProgramOwned(t *testing.T) {
	s, store, _ := configFixture()
	ctx := context.Background()
	key := "dummy"
	cases := []func(*modelconfig.ModelConfigInput){
		func(v *modelconfig.ModelConfigInput) { v.Connection.ID = "" }, func(v *modelconfig.ModelConfigInput) { v.Connection.Kind = "gateway" },
		func(v *modelconfig.ModelConfigInput) { v.Connection.BaseURL = "https://other.example.test" }, func(v *modelconfig.ModelConfigInput) { v.Connection.Name = "伪装官方" },
		func(v *modelconfig.ModelConfigInput) { v.Connection.Protocol = "openai-chat" }, func(v *modelconfig.ModelConfigInput) { v.Connection.Models[1].ModelID = "other-model" },
		func(v *modelconfig.ModelConfigInput) { v.Connection.Models = v.Connection.Models[:1] }, func(v *modelconfig.ModelConfigInput) { v.Connection.Models[1].ContextWindow++ },
		func(v *modelconfig.ModelConfigInput) { v.Connection.Models[1].Category = "video" },
	}
	for _, change := range cases {
		in := enabledInput(1, &key)
		change(&in)
		_, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
		expectCode(t, e, "OFFICIAL_CONNECTION_FIXED")
	}
	_, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Delete(ctx, "deepseek", 1)
	expectCode(t, e, "OFFICIAL_CONNECTION_FIXED")
	cfg, _ := store.ModelConfigSnapshot(ctx)
	if cfg.Revision != 1 || cfg.HasCredentials() {
		t.Fatal("rejected writes altered configuration")
	}
	in := enabledInput(1, &key)
	in.Connection.Models[1].Alias = "官方推理"
	v, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if e != nil || v.Connections[0].Models[1].Name != "官方推理" {
		t.Fatal(v, e)
	}
}

func TestLogoPersistenceOverrideResetAndMalformedAssets(t *testing.T) {
	s, store, _ := configFixture()
	configService(s).Logos = logoMemory{}
	ctx := context.Background()
	logo, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).UploadLogo(ctx, testLogo(t))
	if e != nil {
		t.Fatal(e)
	}
	cfg, _ := store.ModelConfigSnapshot(ctx)
	if cfg.Revision != 1 || cfg.Connections[0].Models[0].LogoID != "" {
		t.Fatal("upload bound before explicit save")
	}
	raw, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).ReadLogo(ctx, logo.LogoID)
	img, e2 := png.Decode(bytes.NewReader(raw))
	if e != nil || e2 != nil || img.Bounds().Dx() != 96 || img.Bounds().Dy() != 96 {
		t.Fatal("not canonical PNG", e, e2)
	}
	duplicate, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).UploadLogo(ctx, testLogo(t))
	if e != nil || duplicate != logo {
		t.Fatal("immutable asset not deduplicated")
	}
	in := enabledInput(1, nil)
	off := false
	for i := range in.Connection.Models {
		in.Connection.Models[i].Enabled = &off
		in.Connection.Models[i].LogoID = logo.LogoID
	}
	v, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if e != nil || v.Connections[0].Models[0].LogoURL != logo.LogoURL {
		t.Fatal(v, e)
	}
	options, e := s.AvailableModels(ctx)
	if e != nil || options.Catalog[0].LogoURL != logo.LogoURL {
		t.Fatal("disabled project metadata lost logo", e)
	}
	in.Revision = v.Revision
	in.Connection.Models[0].LogoID = ""
	v, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if e != nil || v.Connections[0].Models[0].LogoURL != "/demo/assets/providers/deepseek.svg" || v.Connections[0].Models[1].LogoURL != logo.LogoURL {
		t.Fatal("reset changed shared model", v, e)
	}
	in.Revision = v.Revision
	in.Connection.Models[0].LogoID = "bad"
	_, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	expectCode(t, e, "INVALID_MODEL_LOGO")
	for _, raw := range [][]byte{[]byte(`<svg onload="alert(1)"/>`), make([]byte, (2<<20)+1)} {
		_, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).UploadLogo(ctx, raw)
		expectCode(t, e, "INVALID_MODEL_LOGO")
	}
	configService(s).ReadOnly = true
	_, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).UploadLogo(ctx, testLogo(t))
	expectCode(t, e, "SERVICE_READ_ONLY")
}

func TestDirectoryOnlyUsesCredentialsForExplicitMatchingAddress(t *testing.T) {
	s, store, _ := configFixture()
	ctx := context.Background()
	key := "dummy-gateway"
	calls := 0
	configService(s).Directory = func(_ context.Context, k, url, protocol string) ([]ports.RemoteModel, error) {
		calls++
		if k != key || url != "https://relay.example.test/v1" || protocol != "openai-chat" {
			t.Fatal("wrong directory connection")
		}
		return []ports.RemoteModel{{ModelID: "upstream-text"}}, nil
	}
	v, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, modelconfig.ModelConfigInput{Revision: 1, APIKey: &key, Connection: modelconfig.ConnectionInput{Name: "中转测试", Kind: "gateway", Protocol: "openai-chat", BaseURL: "https://relay.example.test/v1"}})
	if e != nil {
		t.Fatal(e)
	}
	in := modelconfig.DiscoverModelsInput{Revision: v.Revision, ConnectionID: v.Connections[len(v.Connections)-1].ID, BaseURL: "https://relay.example.test/v1", Protocol: "openai-chat"}
	got, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).DiscoverModels(ctx, in)
	if e != nil || len(got.Models) != 1 || calls != 1 {
		t.Fatal(got, e)
	}
	after, _ := store.ModelConfigSnapshot(ctx)
	if after.Revision != v.Revision || len(after.Connections[len(after.Connections)-1].Models) != 0 {
		t.Fatal("discovery silently imported models")
	}
	in.BaseURL = "https://different.example.test/v1"
	_, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).DiscoverModels(ctx, in)
	expectCode(t, e, "MODEL_KEY_REQUIRED")
	if calls != 1 {
		t.Fatal("forwarded old key")
	}
	in.ConnectionID = "deepseek"
	in.BaseURL = md.DeepSeekBaseURL
	_, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).DiscoverModels(ctx, in)
	expectCode(t, e, "OFFICIAL_CONNECTION_FIXED")
	in.ConnectionID = ""
	in.BaseURL = "https://relay.example.test/v1"
	in.APIKey = &key
	got, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).DiscoverModels(ctx, in)
	if e != nil || len(got.Models) != 1 || calls != 2 {
		t.Fatal("unsaved connection discovery failed", e)
	}
}
