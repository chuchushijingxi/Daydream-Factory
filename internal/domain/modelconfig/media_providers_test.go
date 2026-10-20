package modelconfig

import (
	"testing"
)

func TestOfficialModelIdentitiesAreUniqueAndMediaCategoriesAreFixed(t *testing.T) {
	seen := map[string]bool{}
	for _, conn := range DefaultModelConfig().Connections {
		for _, m := range conn.Models {
			if m.ID == "" || seen[m.ID] {
				t.Fatal("duplicate stable identity", m.ID)
			}
			seen[m.ID] = true
			if m.Category != "analysis" && (m.Executable() || m.Pricing != nil || m.ContextWindow != 0 || m.MaxOutputTokens != 0) {
				t.Fatal("media cannot use language profile", m)
			}
		}
	}
	kling, _ := OfficialPreset("kling")
	ali, _ := OfficialPreset("alibaba")
	if len(kling.Models) != 5 || len(ali.Models) != 6 {
		t.Fatal("incomplete official catalogs")
	}
	if kling.Models[0].ModelID != "kling-v3" || kling.Models[2].ModelID != "kling-3.0" || kling.Models[4].ModelID != "kling-3.0-turbo" {
		t.Fatal("upgraded video route identifiers confused with image model_name")
	}
	if ali.Models[0].ModelID != "wan2.7-image" || ali.Models[2].ModelID != "happyhorse-1.1-t2v" || ali.Models[5].ModelID != "happyhorse-1.0-video-edit" {
		t.Fatal("incorrect published IDs")
	}
	google, _ := OfficialPreset("google")
	want := []string{"gemini-nano-banana-2.1", "gemini-3-pro-image", "gemini-3.1-flash-image", "gemini-3.1-flash-lite-image", "gemini-2.5-flash-image"}
	if len(google.Models) != len(want)+3 || google.Protocol != GoogleProtocol {
		t.Fatal("incomplete Nano Banana family")
	}
	for i, m := range google.Models[:5] {
		if m.ModelID != want[i] || m.Category != "image" || m.DefaultName() == m.ModelID {
			t.Fatal("incorrect Google model", m)
		}
	}
}

func TestOfficialDirectoryExpansionKeepsSavedCredentialsAndOverrides(t *testing.T) {
	old := volcenginePreset()
	old.Models = old.Models[:8]
	old.Secret = []byte("fictional-ciphertext")
	old.Models[6].Alias, old.Models[6].LogoID, old.Models[6].Enabled = "旧视频别名", "saved-logo", true
	expanded, err := ResolveOfficial(old)
	if err != nil || len(expanded.Models) != 11 || string(expanded.Secret) != string(old.Secret) || old.Models[6].Alias != expanded.Models[6].Alias || old.Models[6].LogoID != expanded.Models[6].LogoID || !expanded.Models[6].Enabled {
		t.Fatal("official expansion lost saved values", err)
	}
	lite := expanded.Models[10]
	if lite.ModelID != SeedreamLiteModelID || lite.Enabled || lite.CreditPrice != nil || lite.Category != "image" || lite.Executable() {
		t.Fatal("new Lite changed old model or invented activation/price", lite)
	}
	for i, id := range []string{SeedanceFastModelID, SeedanceMiniModelID} {
		m := expanded.Models[8+i]
		if m.ModelID != id || m.Enabled || m.Category != "video" || m.Executable() || m.Pricing != nil {
			t.Fatal("new video model is not a disabled preference", m)
		}
	}
	if len(old.Models) != 8 {
		t.Fatal("reading changed stored directory")
	}
}
