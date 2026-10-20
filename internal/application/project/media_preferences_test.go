package project

import (
	"context"
	"drama-agent/internal/domain/credits"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

type preferenceOnlyModel struct{}

func (preferenceOnlyModel) Capacity() (int, int) { return 1000000, 393216 }

func (preferenceOnlyModel) Generate(context.Context, ports.Request, func(json.RawMessage) error) (ports.Response, error) {
	panic("model execution forbidden in preference checks")
}

func TestMediaPreferencesCandidatesAndFixedSaveWithoutModelExecution(t *testing.T) {
	ctx := context.Background()
	projects, config := newMemory(), newConfigMemory()
	factoryCalls := 0
	c := &modelconfig.Service{Store: config, Projects: projects, Cipher: fixtureCipher{}, Factory: func(_ context.Context, _, _, _ string, m md.ConfiguredModel) (ports.Model, error) {
		factoryCalls++
		if m.Category != "analysis" {
			t.Fatal("media preference constructed an execution adapter")
		}
		return preferenceOnlyModel{}, nil
	}}
	s := &Service{Store: projects, Models: c}
	// Use program-owned metadata and fictional credentials, without any model
	// response, Runner, execution permit or provider endpoint.
	config.cfg.Connections[0].Models[0].Enabled = true
	config.cfg.Connections[0].Secret = []byte("cipher:fictional-analysis")
	config.cfg.Connections[1].Secret = []byte("cipher:fictional-media")
	var imageID, videoID string
	for i := range config.cfg.Connections[1].Models {
		m := &config.cfg.Connections[1].Models[i]
		m.CreditPrice = &credits.PriceRule{}
		if m.Category == "image" && imageID == "" {
			m.Enabled, m.Alias, imageID = true, "虚构画面别名", m.ID
		}
		if m.Category == "video" && videoID == "" {
			m.Enabled, m.Alias, videoID = true, "虚构视频别名", m.ID
		}
	}
	opts, err := s.AvailableModels(ctx)
	if err != nil || len(opts.Analysis) != 1 || len(opts.Image) != 1 || len(opts.Video) != 1 || opts.Image[0].ID != imageID || opts.Image[0].Name != "虚构画面别名" || opts.Video[0].ID != videoID || factoryCalls != 0 {
		t.Fatal("metadata candidates are incomplete or executed", opts, err)
	}
	view, err := c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).View(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range view.Connections[1].Models {
		if m.Category != "analysis" && (m.Available || m.Executable()) {
			t.Fatal("selection advertised media execution")
		}
	}
	p := d.Project{ID: "fictional-media-project", Revision: 1, ScriptSource: d.ScriptSource{Version: 1}, Original: []byte("Full fictional original"), Assets: []d.Asset{}, Runs: []d.Run{}}
	if err = projects.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	settings := d.ProjectSettings{AspectRatio: "16:9", LLMModel: md.DeepSeekModelID, ImageModel: imageID, VideoModel: videoID}
	saved, err := s.SaveSettings(ctx, p.ID, 1, settings)
	if err != nil || saved.ProjectSettings == nil || *saved.ProjectSettings != settings || saved.Revision != 2 || len(saved.Runs) != 0 || saved.UsedMicroCNY() != 0 {
		t.Fatal("preferences not saved without fees", saved, err)
	}
	readback, _ := projects.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(saved, readback) {
		t.Fatal("saved preference readback differs")
	}
	changed := settings
	changed.VideoModel = ""
	_, err = s.SaveSettings(ctx, p.ID, 2, changed)
	expectCode(t, err, "SETTINGS_LOCKED")
	_, err = s.SaveSettings(ctx, p.ID, 1, settings)
	if !errors.Is(err, fault.ErrConflict) {
		t.Fatal("stale preference version accepted", err)
	}
	for i := range config.cfg.Connections[1].Models {
		if config.cfg.Connections[1].Models[i].ID == imageID {
			config.cfg.Connections[1].Models[i].Enabled = false
		}
	}
	opts, err = s.AvailableModels(ctx)
	if err != nil || len(opts.Image) != 0 || len(opts.Video) != 1 {
		t.Fatal("closed image remains selectable", opts, err)
	}
	if _, err = s.SaveSettings(ctx, p.ID, 2, settings); err != nil {
		t.Fatal("same confirmed preference failed after closure", err)
	}
	config.cfg.Connections[1].Secret = nil
	opts, err = s.AvailableModels(ctx)
	if err != nil || len(opts.Video) != 0 || len(opts.Analysis) != 1 {
		t.Fatal("missing credential offered media preference", opts, err)
	}
	c.ReadOnly = true
	opts, err = s.AvailableModels(ctx)
	if err != nil || len(opts.Analysis)+len(opts.Image)+len(opts.Video) != 0 || len(opts.Catalog) == 0 {
		t.Fatal("readonly candidates or display catalog incorrect", opts, err)
	}
	if factoryCalls != 1 {
		t.Fatal("ordinary reads or fixed settings constructed extra adapters", factoryCalls)
	}
}
