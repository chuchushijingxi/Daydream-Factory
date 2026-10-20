package project

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/mockmedia"
	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

func TestImageTypeQuotesValidateTargetsAndBindTypeAndRatio(t *testing.T) {
	s, _, p := p5Prepared(t)
	p.ProjectSettings = &d.ProjectSettings{AspectRatio: "9:16"}
	s.MediaTasks = &taskapp.Service{}
	s.Prices = resolutionPrices{c.PriceRule{UnitPrice: 5}}
	target := MediaTarget{AssetID: "c"}
	bases := map[string]bool{}
	for _, imageType := range []string{"closeup", "main_view", "closeup_main", "three_view", "closeup_three_view", "no_preset"} {
		for _, target := range []MediaTarget{target, {AssetID: "c", LookID: "l"}} {
			q, _, err := s.mediaQuote(context.Background(), p, md.MockImageModelID, target, imageType, "", nil, time.Now())
			if err != nil || q.Quantity != 1 || q.Amount != 5 || bases[q.Basis] {
				t.Fatal("type must bind one image quote", imageType, q, err)
			}
			bases[q.Basis] = true
		}
	}
	portrait, _, err := s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "main_view", "9:16", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	square, _, err := s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "main_view", "1:1", nil, time.Now())
	if err != nil || portrait.Basis == square.Basis {
		t.Fatal("ratio did not bind quote", err)
	}
	for _, target := range []MediaTarget{{AssetID: "prop"}, {AssetID: "loc"}} {
		_, _, err := s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "closeup", "", nil, time.Now())
		expectCode(t, err, "IMAGE_TYPE_INVALID")
	}
	_, _, err = s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "unsupported", "", nil, time.Now())
	expectCode(t, err, "IMAGE_TYPE_INVALID")
	for _, ratio := range []string{"9:16", "arbitrary"} {
		_, _, err = s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "three_view", ratio, nil, time.Now())
		expectCode(t, err, "IMAGE_ASPECT_RATIO_INVALID")
	}
	_, _, err = s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "closeup", "arbitrary", nil, time.Now())
	expectCode(t, err, "IMAGE_ASPECT_RATIO_INVALID")
}

type compositionTasks struct{ previewTaskStore }

func (s *compositionTasks) TaskByCommand(_ context.Context, scope, command string) (td.Task, error) {
	for _, row := range s.rows {
		if row.ScopeID == scope && row.CommandID == command {
			return row, nil
		}
	}
	return td.Task{}, fault.ErrNotFound
}
func (s *compositionTasks) CreateTask(_ context.Context, row td.Task) (td.Task, error) {
	s.rows = append(s.rows, row)
	s.inputs[row.ID] = row.Snapshot
	return row, nil
}
func (s *compositionTasks) ForTaskOwner(string) ports.TaskStore { return s }

type compositionQueue struct{ ports.TaskQueue }

func (compositionQueue) PublishTasks(context.Context, []string) error { return nil }

type unusedCompositionResults struct{ ports.TaskResultStore }
type unusedCompositionLedger struct{ ports.CreditLedger }

func TestImageTypeSingleAndBatchFreezeInputsAndReplayAfterSettingsChange(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "batch"}[batch], func(t *testing.T) {
			ctx := context.Background()
			s, _, p := p5Prepared(t)
			p.OwnerID = "composition-fixture"
			p.ProjectSettings = &d.ProjectSettings{AspectRatio: "9:16"}
			p.Style = &d.ProjectStyle{Prompts: d.StylePrompts{ZH: "固定风格测试"}}
			p, err := s.commit(ctx, p, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			store := &compositionTasks{previewTaskStore: previewTaskStore{inputs: map[string]td.Snapshot{}}}
			s.MediaTasks = &taskapp.Service{Store: store, Resolver: mediaResolver{}, Provider: mockmedia.TaskProvider{}, Results: unusedCompositionResults{}, Queue: compositionQueue{}, Policy: td.Policy{PollMillis: 1, RetryMillis: 1, MaxSubmissions: 1}, ObserveTimeout: time.Minute, SaveTimeout: time.Minute}
			s.Prices = resolutionPrices{c.PriceRule{UnitPrice: 0, ResolutionPrices: map[string]int64{"4K": 0}}}
			s.Credits = unusedCompositionLedger{}
			in := MediaCommand{CommandID: "composition", Revision: p.Revision, ModelID: md.MockImageModelID, Target: MediaTarget{AssetID: "c"}, ImageType: "closeup_three_view", AspectRatio: "16:9", Options: &md.MediaOptions{Resolution: "4K"}}
			q, _, err := s.mediaQuote(ctx, p, in.ModelID, in.Target, in.ImageType, in.AspectRatio, in.Options, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			in.BillingBasis = q.Basis
			changed := in
			changed.ImageType = "closeup"
			_, err = s.startMedia(ctx, p, changed, batch)
			expectCode(t, err, "BILLING_BASIS_CHANGED")
			var view MediaView
			if batch {
				items, e := s.StartImageBatch(ctx, p.ID, []MediaCommand{in})
				err = e
				if len(items) != 1 || items[0].Err != nil {
					t.Fatal(items, err)
				}
				view = *items[0].Task
			} else {
				view, err = s.StartMedia(ctx, p.ID, in)
			}
			if err != nil {
				t.Fatal(err)
			}
			raw := store.inputs[view.ID].Input
			var frozen struct{ Prompt, AspectRatio, Resolution string }
			if json.Unmarshal(raw, &frozen) != nil || frozen.AspectRatio != "16:9" || frozen.Resolution != "4K" || strings.Count(frozen.Prompt, "画面类型：") != 1 || strings.Count(frozen.Prompt, "固定风格测试") != 1 || !strings.Contains(frozen.Prompt, "四列") {
				t.Fatal("wrong frozen transport", string(raw))
			}
			if view.ImageType != in.ImageType || view.AspectRatio != "16:9" || view.FillMissingImage != batch {
				t.Fatal(view)
			}
			p.ProjectSettings.AspectRatio = "1:1"
			p, err = s.commit(ctx, p, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			// 新服务对象模拟重开应用，仍只按原快照回读，不重新准备默认值。
			s.MediaTasks = &taskapp.Service{Store: store}
			replayed, err := s.startMedia(ctx, p, in, batch)
			if err != nil || replayed.ID != view.ID || replayed.AspectRatio != "16:9" || replayed.Stale || len(store.rows) != 1 || string(raw) != string(store.inputs[view.ID].Input) {
				t.Fatal("replay changed accepted input", replayed, err)
			}
			changed = in
			changed.ImageType = "three_view"
			_, err = s.startMedia(ctx, p, changed, batch)
			expectCode(t, err, "COMMAND_ID_CONFLICT")
			if p.ProjectSettings.AspectRatio != "1:1" || p.Prompt(d.VisualTarget{AssetID: "c"}).Text != "有效完整外观" {
				t.Fatal("generation overwrote project settings or prompt")
			}
		})
	}
}
