package project

import (
	"context"
	"drama-agent/internal/domain/credits"
	"strings"
	"testing"

	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	md "drama-agent/internal/domain/modelconfig"
)

func TestGoogleSharesSavedCredentialAcrossNativeLanguageAndImagePreferences(t *testing.T) {
	s, store, model := configFixture()
	ctx := context.Background()
	preset, _ := md.OfficialPreset("google")
	// A saved image-only connection upgrades without changing its credential,
	// model identities, aliases, logo overrides, switches or revision on a GET.
	old := preset
	old.Protocol = "gemini-image"
	old.Models = append([]md.ConfiguredModel(nil), preset.Models[:5]...)
	old.Secret = []byte("cipher:fictional-google-key")
	old.Models[0].Alias, old.Models[0].Enabled, old.Models[0].LogoID = "角色图", true, "saved-logo"
	for i := range store.cfg.Connections {
		if store.cfg.Connections[i].ID == "google" {
			store.cfg.Connections[i] = old
		}
	}
	view, err := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).View(ctx)
	if err != nil || view.Revision != 1 {
		t.Fatal(err)
	}
	var conn modelconfig.ConnectionView
	for _, c := range view.Connections {
		if c.ID == "google" {
			conn = c
		}
	}
	if conn.Protocol != md.GoogleProtocol || len(conn.Models) != 8 || !conn.CredentialConfigured || conn.Models[0].Name != "角色图" || conn.Models[0].LogoID != "saved-logo" || !conn.Models[0].Enabled {
		t.Fatal(conn)
	}
	for _, m := range conn.Models[5:] {
		if m.Enabled || m.Available || m.Category != "analysis" || !m.Executable() {
			t.Fatal("invalid new language preset", m)
		}
	}
	stored, _ := store.ModelConfigSnapshot(ctx)
	for _, c := range stored.Connections {
		if c.ID == "google" && (c.Protocol != "gemini-image" || len(c.Models) != 5 || string(c.Secret) != "cipher:fictional-google-key") {
			t.Fatal("GET wrote stored configuration")
		}
	}
	on := true
	in := modelconfig.ModelConfigInput{Revision: 1, Connection: modelconfig.ConnectionInput{ID: preset.ID, Name: preset.Name, Kind: preset.Kind, Protocol: preset.Protocol, BaseURL: preset.BaseURL}}
	for _, m := range preset.Models {
		in.Connection.Models = append(in.Connection.Models, modelconfig.ModelEnableInput{ID: m.ID, ModelID: m.ModelID, Category: m.Category, Enabled: &on, CreditPrice: &credits.PriceRule{}, ContextWindow: m.ContextWindow, MaxOutputTokens: m.MaxOutputTokens, Pricing: m.Pricing})
	}
	in.Connection.Models[5].Alias = "编剧语言"
	view, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := s.AvailableModels(ctx)
	if err != nil || len(opts.Analysis) != 3 || len(opts.Image) != 4 || len(opts.Video) != 0 || opts.Analysis[0].Name != "编剧语言" {
		t.Fatal(opts, err)
	}
	calls := 0
	configService(s).Factory = func(_ context.Context, key, url, protocol string, profile md.ConfiguredModel) (ports.Model, error) {
		calls++
		if key != "fictional-google-key" || url != md.GoogleBaseURL || protocol != md.GoogleProtocol || profile.Category != "analysis" {
			t.Fatal("wrong provider binding")
		}
		return model, nil
	}
	if _, err = availableBindings(s, ctx); err != nil || calls != 3 || model.count != 0 {
		t.Fatal(err, calls, model.count)
	}
	for _, c := range view.Connections {
		if c.ID == "google" {
			for _, m := range c.Models {
				if m.Available != (m.Category == "analysis") || m.Provider != "google" || m.DefaultLogoURL != "/demo/assets/providers/google.svg" {
					t.Fatal(m)
				}
			}
		}
	}
	bad := in
	bad.Revision = view.Revision
	bad.Connection.Models = append([]modelconfig.ModelEnableInput(nil), in.Connection.Models...)
	bad.Connection.Models[5].ModelID = "user-modified"
	_, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, bad)
	expectCode(t, err, "OFFICIAL_CONNECTION_FIXED")
	if strings.Contains(conn.Reason, "fictional-google-key") {
		t.Fatal("credential leaked")
	}
}
