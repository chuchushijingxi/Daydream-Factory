package project

import (
	d "drama-agent/internal/domain/project"
	"fmt"
	"strings"
	"testing"
)

func TestStoryboardProjectionKeepsShotCountsAcrossSourceSizes(t *testing.T) {
	for _, spans := range []int{100, 1000, 5000} {
		t.Run(fmt.Sprintf("spans_%d", spans), func(t *testing.T) {
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
			v := s.StoryboardIndex(p)
			if v.Index == nil || len(v.Index.Episodes) != 1 || v.Index.Episodes[0].ShotCount != 100 {
				t.Fatal("source size changed projected shot count")
			}
		})
	}
}
