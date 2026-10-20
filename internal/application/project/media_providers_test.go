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

func TestOfficialMediaProvidersShareOneCredentialAndRemainPreferences(t *testing.T) {
	for _, id := range []string{"kling", "alibaba", "openai"} {
		t.Run(id, func(t *testing.T) {
			s, store, model := configFixture()
			ctx := context.Background()
			store.cfg.Connections = store.cfg.Connections[:2]
			view, err := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).View(ctx)
			if err != nil || len(view.Connections) != len(md.DefaultModelConfig().Connections) || len(store.cfg.Connections) != 2 || store.cfg.Revision != 1 {
				t.Fatal("reading new providers changed storage", err)
			}
			preset, ok := md.OfficialPreset(id)
			if !ok {
				t.Fatal(id)
			}
			key := "fictional-shared-" + id
			on := true
			in := modelconfig.ModelConfigInput{Revision: 1, APIKey: &key, Connection: modelconfig.ConnectionInput{ID: preset.ID, Name: preset.Name, Kind: preset.Kind, Protocol: preset.Protocol, BaseURL: preset.BaseURL}}
			images, videos := 0, 0
			for _, m := range preset.Models {
				if m.Enabled || m.Executable() || m.ContextWindow != 0 || m.MaxOutputTokens != 0 || m.Pricing != nil {
					t.Fatal("media acquired fake text profile", m)
				}
				if m.Category == "image" {
					images++
				} else if m.Category == "video" {
					videos++
				} else {
					t.Fatal("unexpected language model")
				}
				in.Connection.Models = append(in.Connection.Models, modelconfig.ModelEnableInput{ID: m.ID, ModelID: m.ModelID, Category: m.Category, Enabled: &on, CreditPrice: &credits.PriceRule{}})
			}
			in.Connection.Models[0].Alias = "画面助手"
			view, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			var conn modelconfig.ConnectionView
			for _, c := range view.Connections {
				if c.ID == id {
					conn = c
				}
			}
			if !conn.CredentialConfigured || conn.Models[0].Name != "画面助手" || conn.Models[0].DefaultName == conn.Models[0].ModelID {
				t.Fatal(conn)
			}
			for _, m := range conn.Models {
				if m.Available || m.Provider != id || m.DefaultLogoURL != "/demo/assets/providers/"+id+".svg" {
					t.Fatal("media executable or wrong brand", m)
				}
			}
			raw, _ := json.Marshal(view)
			if strings.Contains(string(raw), key) || strings.Contains(string(raw), "cipher:") {
				t.Fatal("credential leaked")
			}
			opts, err := s.AvailableModels(ctx)
			if err != nil || len(opts.Analysis) != 0 || len(opts.Image) != images || len(opts.Video) != videos {
				t.Fatal(opts, err)
			}
			factoryCalls := 0
			configService(s).Factory = func(context.Context, string, string, string, md.ConfiguredModel) (ports.Model, error) {
				factoryCalls++
				return model, nil
			}
			if _, err = availableBindings(s, ctx); err != nil || factoryCalls != 0 || model.count != 0 {
				t.Fatal("media constructed a language adapter or generated", err)
			}
			in.Revision = view.Revision
			in.APIKey = nil
			off := false
			in.Connection.Models[0].Enabled = &off
			view, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			stored, _ := store.ModelConfigSnapshot(ctx)
			for _, c := range stored.Connections {
				if c.ID == id && string(c.Secret) != "cipher:"+key {
					t.Fatal("toggle lost shared credential")
				}
			}
			bad := in
			bad.Revision = view.Revision
			bad.Connection.Models = append([]modelconfig.ModelEnableInput(nil), in.Connection.Models...)
			bad.Connection.Models[0].ModelID = "user-replacement"
			_, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, bad)
			expectCode(t, err, "OFFICIAL_CONNECTION_FIXED")
			bad = in
			bad.Revision = view.Revision
			bad.Connection.BaseURL = "https://other.example.test"
			_, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, bad)
			expectCode(t, err, "OFFICIAL_CONNECTION_FIXED")
			_, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Delete(ctx, id, view.Revision)
			expectCode(t, err, "OFFICIAL_CONNECTION_FIXED")
			gateway := in
			gateway.Connection.Kind = "gateway"
			gateway.Revision = view.Revision
			_, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, gateway)
			expectCode(t, err, "INVALID_INPUT")
		})
	}
}
