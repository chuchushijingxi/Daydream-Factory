package project

import (
	"context"
	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/credits"
	md "drama-agent/internal/domain/modelconfig"
	"reflect"
	"testing"
	"time"
)

func TestMediaTierConfigurationDefaultsAvailabilityAndAtomicity(t *testing.T) {
	ctx := context.Background()
	configured, store, text := configFixture()
	config := configService(configured)
	config.Tasks = &taskapp.Service{}
	operator := config.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator})
	input := mockInput(1)
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 20, ResolutionPrices: map[string]int64{"2K": 12, "4K": 3}}
	input.Connection.Models[0].ResolutionEnabled = map[string]bool{"1K": false, "2K": true, "4K": true}
	input.Connection.Models[1].CreditPrice = &credits.PriceRule{UnitPrice: 10, ResolutionPrices: map[string]int64{"480p": 2, "720p": 6}}
	input.Connection.Models[1].ResolutionEnabled = map[string]bool{"480p": true, "720p": true, "1080p": false}
	view, err := operator.Update(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	options, err := config.UserOptions(ctx)
	if err != nil || len(options.Image) != 1 || options.Image[0].MediaProfile.DefaultResolution != "4K" || len(options.Image[0].MediaProfile.Resolutions) != 2 || options.Image[0].CreditPrice.UnitPrice != 3 || len(options.Image[0].CreditPrice.ResolutionPrices) != 2 || options.Video[0].MediaProfile.DefaultResolution != "720p" {
		t.Fatal(options, err)
	}
	s, _, p := p5Prepared(t)
	s.Prices = config
	s.MediaTasks = &taskapp.Service{}
	q, _, err := s.mediaQuote(ctx, p, md.MockImageModelID, MediaTarget{AssetID: "c"}, "", "", nil, time.Now())
	if err != nil || q.MediaOptions.Resolution != "4K" || q.Amount != 3 {
		t.Fatal("highest resolution must win over price", q, err)
	}
	selected := md.MediaOptions{Resolution: "2K"}
	q, _, err = s.mediaQuote(ctx, p, md.MockImageModelID, MediaTarget{AssetID: "c"}, "", "", &selected, time.Now())
	if err != nil || q.Amount != 12 {
		t.Fatal("explicit available choice lost", q, err)
	}
	selected.Resolution = "1K"
	_, _, err = s.mediaQuote(ctx, p, md.MockImageModelID, MediaTarget{AssetID: "c"}, "", "", &selected, time.Now())
	expectCode(t, err, "MEDIA_OPTIONS_INVALID")
	input.Revision = view.Revision
	input.Connection.Models[0].ResolutionEnabled["4K"] = false
	view, err = operator.Update(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	q, _, err = s.mediaQuote(ctx, p, md.MockImageModelID, MediaTarget{AssetID: "c"}, "", "", nil, time.Now())
	if err != nil || q.MediaOptions.Resolution != "2K" || q.Amount != 12 {
		t.Fatal("closed highest did not fall back", q, err)
	}
	// Closing preserves all stored rates; re-enabling never reconstructs a price.
	saved, _ := store.ModelConfigSnapshot(ctx)
	for _, conn := range saved.Connections {
		if conn.ID == "mock" && conn.Models[0].CreditPrice.ResolutionPrices["4K"] != 3 {
			t.Fatal("closed price lost")
		}
	}
	input.Revision = view.Revision
	input.Connection.Models[0].ResolutionEnabled["2K"] = false
	view, err = operator.Update(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	options, err = config.UserOptions(ctx)
	if err != nil || len(options.Image) != 0 || len(options.Video) != 1 {
		t.Fatal("all-off leaked candidate", options, err)
	}
	_, _, err = s.mediaQuote(ctx, p, md.MockImageModelID, MediaTarget{AssetID: "c"}, "", "", nil, time.Now())
	expectCode(t, err, "MEDIA_OPTIONS_INVALID")
	before, _ := store.ModelConfigSnapshot(ctx)
	for _, kind := range []string{"unknown-tier", "missing-price", "stale"} {
		input.Revision = view.Revision
		input.Connection.Models[0].ResolutionEnabled = map[string]bool{"4K": true}
		input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 20, ResolutionPrices: map[string]int64{"2K": 12, "4K": 3}}
		switch kind {
		case "unknown-tier":
			input.Connection.Models[0].ResolutionEnabled["8K"] = true
		case "missing-price":
			delete(input.Connection.Models[0].CreditPrice.ResolutionPrices, "4K")
		case "stale":
			input.Revision--
		}
		if _, err = operator.Update(ctx, input); err == nil {
			t.Fatal("invalid configuration accepted", kind)
		}
		after, _ := store.ModelConfigSnapshot(ctx)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("rejected CAS partially wrote", kind)
		}
	}
	if text.count != 0 {
		t.Fatal("configuration invoked a model")
	}
}
