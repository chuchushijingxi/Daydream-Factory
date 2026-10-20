package project

import (
	d "drama-agent/internal/domain/project"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStoryboardProjectionScaling(t *testing.T) {
	for _, spans := range []int{100, 1000, 5000} {
		p := d.Project{ID: "project", Revision: 1, ScriptSource: d.ScriptSource{Version: 1}, Storyboard: d.NewStoryboard(1)}
		for i := 0; i < spans; i++ {
			p.ScriptSource.Spans = append(p.ScriptSource.Spans, d.SourceSpan{ID: fmt.Sprint(i), Text: strings.Repeat("甲", 100)})
		}
		all := []d.SourceRange{{Start: d.SourcePoint{SpanID: "0"}, End: d.SourcePoint{SpanID: fmt.Sprint(spans - 1), Offset: 100}}}
		segmentation := "whole"
		p.Storyboard.Index.Segmentation = &segmentation
		p.Storyboard.Index.Episodes = []d.Episode{{ID: "episode", EpisodeValue: d.EpisodeValue{Title: "whole", SourceRanges: all}, SceneIDs: []string{"scene"}}}
		basis := p.EpisodeSceneBasis(&p.Storyboard.Index.Episodes[0])
		scene := d.Scene{ID: "scene", EpisodeID: "episode", SceneValue: d.SceneValue{Title: "scene", SourceRanges: all}, Basis: basis}
		for i := 0; i < 100; i++ {
			id := fmt.Sprint(i)
			scene.ShotIDs = append(scene.ShotIDs, id)
			p.Storyboard.Shots = append(p.Storyboard.Shots, d.Shot{ID: id, SceneID: "scene", ShotValue: d.ShotValue{SourceRanges: []d.SourceRange{{Start: d.SourcePoint{SpanID: id}, End: d.SourcePoint{SpanID: id, Offset: 100}}}}, Basis: d.ShotBasis{SceneBasis: basis, SceneRangeDigest: d.Digest(all)}})
		}
		p.Storyboard.Scenes = []d.Scene{scene}
		p.Runs = []d.Run{{ID: "index", Contract: d.EpisodeIndexContract, SourceVersion: 1, Status: d.RunCompleted}}
		s := &Service{}
		for i := 0; i < 2; i++ {
			_ = s.StoryboardIndex(p)
		}
		samples := []time.Duration{}
		for i := 0; i < 20; i++ {
			start := time.Now()
			v := s.StoryboardIndex(p)
			samples = append(samples, time.Since(start))
			if v.Index == nil || len(v.Index.Episodes) != 1 || v.Index.Episodes[0].ShotCount != 100 {
				t.Fatal("invalid fixture")
			}
		}
		allocs := testing.AllocsPerRun(5, func() { _ = s.StoryboardIndex(p) })
		t.Logf("spans=%d source_chars=%d shots=100 samples=20 hot_serial allocs/op=%.0f durations=%v", spans, spans*100, allocs, samples)
	}
}
