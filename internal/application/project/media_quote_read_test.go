package project

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

type mediaQuoteReadStore struct {
	ports.ProjectStore
	fullReads, mediaReads int
	err                   error
	deleted               bool
}

func (s *mediaQuoteReadStore) Snapshot(ctx context.Context, id string) (d.Project, error) {
	s.fullReads++
	return s.ProjectStore.Snapshot(ctx, id)
}

func (s *mediaQuoteReadStore) MediaProject(ctx context.Context, id string) (d.Project, error) {
	s.mediaReads++
	if s.err != nil {
		return d.Project{}, s.err
	}
	p, err := s.ProjectStore.Snapshot(ctx, id)
	p = d.Clone(p)
	p.Original = nil
	for i := range p.Runs {
		p.Runs[i].Calls = nil
	}
	if s.deleted {
		now := time.Now()
		p.DeletedAt = &now
	}
	return p, err
}

func TestMediaQuoteUsesReadProjectionWithoutChangingBasisOrScopeChecks(t *testing.T) {
	s, _, p := p5Prepared(t)
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(p.Storyboard.Index.Episodes[0])})
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Scenes[0].ID, "value": p5Shot(p.Storyboard.Index.Episodes[0])})
	s.MediaTasks = &taskapp.Service{}
	s.Prices = resolutionPrices{c.PriceRule{UnitPrice: 10, ResolutionPrices: map[string]int64{"720p": 6}}}
	ctx := context.Background()
	fullStore := s.Store
	before, err := fullStore.Snapshot(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader := &mediaQuoteReadStore{ProjectStore: fullStore}
	for _, input := range []QuoteInput{
		{Operation: "media", Revision: p.Revision, ModelID: md.MockImageModelID, Target: &MediaTarget{AssetID: "c"}},
		{Operation: "media", Revision: p.Revision, ModelID: md.MockVideoModelID, Target: &MediaTarget{EpisodeID: "e1", ShotID: p.Storyboard.Shots[0].ID}, Options: &md.MediaOptions{Resolution: "720p", DurationSeconds: 6}},
	} {
		s.Store = fullStore
		want, err := s.Quote(ctx, p.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		s.Store = reader
		got, err := s.Quote(ctx, p.ID, input)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("media read changed price or basis", got, want, err)
		}
	}
	if reader.fullReads != 0 || reader.mediaReads != 2 {
		t.Fatal("media quote loaded full history", reader.fullReads, reader.mediaReads)
	}
	input := QuoteInput{Operation: "media", Revision: p.Revision - 1, ModelID: md.MockImageModelID, Target: &MediaTarget{AssetID: "c"}}
	if _, err := s.Quote(ctx, p.ID, input); !errors.Is(err, fault.ErrConflict) {
		t.Fatal("stale revision accepted", err)
	}
	input.Revision = p.Revision
	reader.deleted = true
	if _, err := s.Quote(ctx, p.ID, input); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("deleted project quoted", err)
	}
	reader.deleted = false
	reader.err = fault.ErrNotFound
	if _, err := s.Quote(ctx, p.ID, input); !errors.Is(err, fault.ErrNotFound) || reader.fullReads != 0 {
		t.Fatal("projection rejection bypassed", err, reader.fullReads)
	}
	after, err := fullStore.Snapshot(ctx, p.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("quote changed original content/history", err)
	}
}
