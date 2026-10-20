package modelconfig

import (
	"drama-agent/internal/domain/credits"
	"reflect"
	"testing"
)

func TestMediaSelectionUsesPricePresenceOnlyWhenSwitchesUnset(t *testing.T) {
	p := OfficialMediaProfile("mock", MockImageModelID)
	m := ConfiguredModel{Category: "image", CreditPrice: &credits.PriceRule{UnitPrice: 0, ResolutionPrices: map[string]int64{"4K": 1}}}
	if got := m.ResolutionSwitches(p); !reflect.DeepEqual(got, map[string]bool{"1K": true, "2K": false, "4K": true}) {
		t.Fatal(got)
	}
	if m.EnabledMediaProfile(p).DefaultResolution != "4K" || p.DefaultResolution != "1K" {
		t.Fatal("profile or pricing basis changed")
	}
	m.ResolutionEnabled = map[string]bool{}
	if got := m.EnabledMediaProfile(p); len(got.Resolutions) != 0 || got.DefaultResolution != "" {
		t.Fatal(got)
	}
	m.ResolutionEnabled = nil
	m.CreditPrice = nil
	if len(m.EnabledMediaProfile(p).Resolutions) != 0 {
		t.Fatal("unpriced tiers enabled")
	}
	p = OfficialMediaProfile("openai", "gpt-image-2")
	m.ResolutionEnabled = map[string]bool{"1024x1024": true, "3840x2160": true, "2160x3840": true}
	if m.EnabledMediaProfile(p).DefaultResolution != "3840x2160" {
		t.Fatal("pixel area order")
	}
}
