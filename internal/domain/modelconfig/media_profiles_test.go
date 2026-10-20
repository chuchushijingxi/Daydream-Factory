package modelconfig

import (
	"reflect"
	"testing"
)

func TestSeedreamLiteHasItsOwnIdentityAndHighResolutionTiers(t *testing.T) {
	p := OfficialMediaProfile("volcengine", SeedreamLiteModelID)
	if p == nil || p.DefaultResolution != "2K" || !reflect.DeepEqual(p.Resolutions, resolutions("2K", "3K", "4K")) || p.HasResolution("1K") || p.HasResolution("1.5K") {
		t.Fatal(p)
	}
	for _, id := range []string{SeedreamProModelID, SeedreamFlashModelID} {
		p := OfficialMediaProfile("volcengine", id)
		if !reflect.DeepEqual(p.Resolutions, resolutions("1K", "1.5K", "2K")) || p.HasResolution("3K") || p.HasResolution("4K") {
			t.Fatal("different Seedream models shared capabilities", id, p)
		}
	}
	if (ConfiguredModel{ModelID: SeedreamLiteModelID}).DefaultName() != "Doubao Seedream 5.0 Lite" {
		t.Fatal("Lite identity/name mismatch")
	}
}

func TestMockVideoCapabilitiesMatchEverySelectableCombination(t *testing.T) {
	p := OfficialMediaProfile("mock", MockVideoModelID)
	if p.Defaults() != (MediaOptions{Resolution: "1080p", DurationSeconds: 10}) || len(p.Resolutions) != 3 || len(p.Durations) != 10 || len(p.AspectRatios) != 1 || p.AspectRatios[0] != "16:9" {
		t.Fatal(p)
	}
	for _, resolution := range p.Resolutions {
		if len(resolution.Pixels) != 1 || resolution.Pixels["16:9"] == "" {
			t.Fatal("unsupported ratio advertised", resolution)
		}
		for seconds := 1; seconds <= 10; seconds++ {
			if !p.Supports(MediaOptions{Resolution: resolution.ID, DurationSeconds: float64(seconds)}) {
				t.Fatal(resolution, seconds)
			}
		}
	}
	for _, seconds := range []float64{0, -1, 0.5, 1.5, 10.793333, 11} {
		if p.Supports(MediaOptions{Resolution: "1080p", DurationSeconds: seconds}) {
			t.Fatal("invalid duration advertised", seconds)
		}
	}
}

func TestEntireMediaCatalogHasReviewedCapabilities(t *testing.T) {
	images, videos := 0, 0
	for _, conn := range DefaultModelConfig().Connections {
		for _, m := range conn.Models {
			if m.Category == "analysis" {
				continue
			}
			if m.Category == "image" {
				images++
			} else {
				videos++
			}
			p := OfficialMediaProfile(conn.ID, m.ModelID)
			if p == nil || p.CheckedAt != "2026-10-17" || p.SupplierPrice == "" || !p.HasResolution(p.DefaultResolution) || !p.Retired && !p.Supports(p.Defaults()) {
				t.Fatal(m.ID, p)
			}
		}
	}
	if images != 16 || videos != 12 {
		t.Fatal(images, videos)
	}
	if OfficialMediaProfile("gateway", "kling-3.0") != nil {
		t.Fatal("gateway inherited official capabilities")
	}
	if OfficialMediaProfile("volcengine", "doubao-seedance-2-5-260628").HasResolution("4k") {
		t.Fatal("input 4K incorrectly treated as output 4K")
	}
	if OfficialMediaProfile("google", "gemini-3.1-flash-lite-image").HasResolution("2K") {
		t.Fatal("Lite invented 2K")
	}
	if !OfficialMediaProfile("google", "gemini-2.5-flash-image").Retired {
		t.Fatal("closed model remained selectable")
	}
}
