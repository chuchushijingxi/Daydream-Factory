package project

import (
	"context"
	"drama-agent/internal/domain/credits"
	"encoding/json"
	"strings"
	"testing"

	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	md "drama-agent/internal/domain/modelconfig"
)

func TestVolcengineSharedCredentialsFixedDirectoryAndMediaConfigurationOnly(t *testing.T) {
	s, store, model := configFixture()
	ctx := context.Background()
	// Existing installations have only DeepSeek saved; GET cannot write or bump revision.
	store.cfg.Connections = store.cfg.Connections[:1]
	v, err := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).View(ctx)
	if err != nil || len(v.Connections) != len(md.DefaultModelConfig().Connections) || len(store.cfg.Connections) != 1 || store.cfg.Revision != 1 {
		t.Fatal(v, err)
	}
	preset, ok := md.OfficialPreset("volcengine")
	if !ok {
		t.Fatal("missing preset")
	}
	key := "fictional-shared-ark-key"
	on := true
	in := modelconfig.ModelConfigInput{Revision: 1, APIKey: &key, Connection: modelconfig.ConnectionInput{ID: preset.ID, Name: preset.Name, Kind: preset.Kind, Protocol: preset.Protocol, BaseURL: preset.BaseURL}}
	for _, m := range preset.Models {
		in.Connection.Models = append(in.Connection.Models, modelconfig.ModelEnableInput{ID: m.ID, ModelID: m.ModelID, Category: m.Category, Enabled: &on, CreditPrice: &credits.PriceRule{}, ContextWindow: m.ContextWindow, MaxOutputTokens: m.MaxOutputTokens, Pricing: m.Pricing})
	}
	in.Connection.Models[0].Alias = "编剧助手"
	v, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	conn := v.Connections[1]
	if len(conn.Models) != 11 || !conn.CredentialConfigured || conn.Models[0].Name != "编剧助手" || conn.Models[0].DefaultName != "Doubao Seed Evolving" || conn.Models[0].DefaultLogoURL != "/demo/assets/providers/volcengine.svg" {
		t.Fatal(conn)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), key) {
		t.Fatal("credential leaked")
	}
	for _, m := range conn.Models {
		if m.Available != (m.Category == "analysis") {
			t.Fatal("media exposed as executable", m)
		}
	}
	opts, err := s.AvailableModels(ctx)
	if err != nil || len(opts.Analysis) != 4 || len(opts.Image) != 3 || len(opts.Video) != 4 {
		t.Fatal(opts, err)
	}
	factoryCalls := 0
	configService(s).Factory = func(_ context.Context, k, url, protocol string, profile md.ConfiguredModel) (ports.Model, error) {
		factoryCalls++
		if k != key || url != md.VolcengineBaseURL || protocol != md.VolcengineProtocol || profile.Category != "analysis" {
			t.Fatal("wrong shared provider mapping")
		}
		return model, nil
	}
	if _, err = availableBindings(s, ctx); err != nil || factoryCalls != 4 || model.count != 0 {
		t.Fatal(err, factoryCalls, model.count)
	}
	in.Revision = v.Revision
	in.APIKey = nil
	off := false
	in.Connection.Models[0].Enabled = &off
	v, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Connections[1].CredentialConfigured || v.Connections[1].Models[0].Available {
		t.Fatal("toggle lost key or remained available")
	}
	for _, change := range []string{"url", "id", "category", "capacity", "price"} {
		copyIn := in
		copyIn.Revision = v.Revision
		copyIn.Connection.Models = append([]modelconfig.ModelEnableInput(nil), in.Connection.Models...)
		switch change {
		case "url":
			copyIn.Connection.BaseURL = "https://other.example.test"
		case "id":
			copyIn.Connection.Models[0].ModelID = "user-defined"
		case "category":
			copyIn.Connection.Models[0].Category = "image"
		case "capacity":
			copyIn.Connection.Models[0].ContextWindow++
		case "price":
			p := *copyIn.Connection.Models[0].Pricing
			p.Input++
			copyIn.Connection.Models[0].Pricing = &p
		}
		_, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, copyIn)
		expectCode(t, err, "OFFICIAL_CONNECTION_FIXED")
	}
	_, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Delete(ctx, "volcengine", v.Revision)
	expectCode(t, err, "OFFICIAL_CONNECTION_FIXED")
	if model.count != 0 {
		t.Fatal("configuration called model")
	}
}
