package project

import (
	"context"
	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	td "drama-agent/internal/domain/task"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type resolutionPrices struct{ rule c.PriceRule }

func (p resolutionPrices) CreditPrice(context.Context, string) (c.PriceRule, uint64, string, error) {
	return p.rule, 19, "mock", nil
}

func TestVideoQuoteUsesSelectedSecondsAndResolutionNotSimulatedDelay(t *testing.T) {
	s, _, p := p5Prepared(t)
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(p.Storyboard.Index.Episodes[0])})
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Scenes[0].ID, "value": p5Shot(p.Storyboard.Index.Episodes[0])})
	s.MediaTasks = &taskapp.Service{}
	s.Prices = resolutionPrices{c.PriceRule{UnitPrice: 10, ResolutionPrices: map[string]int64{"480p": 3, "720p": 6}}}
	target := MediaTarget{EpisodeID: "e1", ShotID: p.Storyboard.Shots[0].ID}
	bases := map[string]bool{}
	for _, resolution := range []struct {
		id    string
		price int64
	}{{"480p", 3}, {"720p", 6}, {"1080p", 10}} {
		for seconds := 1; seconds <= 10; seconds++ {
			options := md.MediaOptions{Resolution: resolution.id, DurationSeconds: float64(seconds)}
			q, _, err := s.mediaQuote(context.Background(), p, md.MockVideoModelID, target, "", "", &options, time.Now())
			if err != nil || q.Quantity != int64(seconds) || q.Amount != int64(seconds)*resolution.price || q.Rule.UnitPrice != resolution.price || bases[q.Basis] || *q.MediaOptions != options {
				t.Fatal(resolution, seconds, q, err)
			}
			bases[q.Basis] = true
		}
	}
	q, _, err := s.mediaQuote(context.Background(), p, md.MockVideoModelID, target, "", "", nil, time.Now())
	if err != nil || q.Quantity != 10 || q.Amount != 100 || *q.MediaOptions != (md.MediaOptions{Resolution: "1080p", DurationSeconds: 10}) {
		t.Fatal(q, err)
	}
	s.Prices = resolutionPrices{c.PriceRule{UnitPrice: 10}}
	_, _, err = s.mediaQuote(context.Background(), p, md.MockVideoModelID, target, "", "", &md.MediaOptions{Resolution: "480p", DurationSeconds: 1}, time.Now())
	var f *fault.Error
	if !errors.As(err, &f) || f.Code != "MODEL_PRICE_UNAVAILABLE" {
		t.Fatal("missing resolution rate reused default", err)
	}
}

func TestMediaQuantityConfirmsFrozenVideoInputAndRetainsOldCharges(t *testing.T) {
	for _, old := range []bool{false, true} {
		frozen, _ := json.Marshal(md.MockExecution{VideoOptions: !old})
		task := td.Task{State: td.State{Operation: "video.generate", Binding: td.Binding{ProviderConfig: frozen}}}
		input := md.MediaOptions{Resolution: "720p", DurationSeconds: 3}
		charge := c.Charge{Unit: "second", Scope: "media-video", Quantity: 3}
		if old {
			input = md.MediaOptions{Resolution: "1080p", DurationSeconds: md.MockVideoDurationSeconds}
			charge.Quantity = md.MockVideoBillingSeconds
		}
		raw, _ := json.Marshal(input)
		snapshot := td.Snapshot{Input: raw}
		if !mediaChargeQuantity(task, snapshot, &charge) {
			t.Fatal("original accepted quantity rejected", old)
		}
		charge.Quantity++
		if mediaChargeQuantity(task, snapshot, &charge) {
			t.Fatal("changed quantity accepted", old)
		}
		charge.Quantity--
		snapshot.Input = json.RawMessage(`{"resolution":"720p","durationSeconds":4}`)
		if mediaChargeQuantity(task, snapshot, &charge) {
			t.Fatal("changed frozen seconds accepted", old)
		}
	}
}
func TestMediaQuoteBindsSelectedResolutionAndRejectsMissingRates(t *testing.T) {
	s, _, p := p5Prepared(t)
	s.MediaTasks = &taskapp.Service{}
	s.Prices = resolutionPrices{c.PriceRule{UnitPrice: 5, ResolutionPrices: map[string]int64{"2K": 9, "4K": 17}}}
	target := MediaTarget{AssetID: "c"}
	previous := ""
	for _, v := range []struct {
		resolution string
		price      int64
	}{{"1K", 5}, {"2K", 9}, {"4K", 17}} {
		selected := md.MediaOptions{Resolution: v.resolution}
		q, _, err := s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "", "", &selected, time.Now())
		if err != nil || q.Amount != v.price || q.Quantity != 1 || q.Rule.UnitPrice != v.price || len(q.Rule.ResolutionPrices) != 0 || q.MediaOptions == nil || *q.MediaOptions != selected || q.Basis == previous {
			t.Fatal(q, err)
		}
		previous = q.Basis
	}
	s.Prices = resolutionPrices{c.PriceRule{UnitPrice: 5}}
	_, _, err := s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "", "", &md.MediaOptions{Resolution: "2K"}, time.Now())
	var f *fault.Error
	if !errors.As(err, &f) || f.Code != "MODEL_PRICE_UNAVAILABLE" {
		t.Fatal("missing rate became default/free", err)
	}
	for _, selected := range []md.MediaOptions{{Resolution: "8K"}, {Resolution: "1K", DurationSeconds: 5}} {
		_, _, err = s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "", "", &selected, time.Now())
		if !errors.As(err, &f) || f.Code != "MEDIA_OPTIONS_INVALID" {
			t.Fatal(selected, err)
		}
	}
}

func (p resolutionPrices) MediaPrice(ctx context.Context, model string) (ports.MediaPricing, error) {
	rule, version, connection, err := p.CreditPrice(ctx, model)
	profile := md.OfficialMediaProfile(connection, model)
	return ports.MediaPricing{Rule: rule, Version: version, ConnectionID: connection, Profile: profile, PriceDefaultResolution: profile.DefaultResolution}, err
}
