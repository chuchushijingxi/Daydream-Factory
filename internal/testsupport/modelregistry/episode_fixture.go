package modelregistry

import (
	"encoding/json"
	"strings"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	d "drama-agent/internal/domain/project"
)

// FreeEpisodeIndexReply explicitly handles automatic indexing in earlier HTTP transport fixtures.
// Its whole-work output proves transport only, not semantic segmentation quality.
func FreeEpisodeIndexReply(req ports.Request) (ports.Response, bool) {
	found := false
	for _, t := range req.Tools {
		found = found || t.Name == "save_episode_ranges"
	}
	if !found {
		return ports.Response{}, false
	}
	name, args := "finish_episode_index", "{}"
	if len(req.Dialogue) == 0 && !strings.Contains(req.System, "这是独立复核上下文") {
		var input struct {
			Source []d.SourceSpan `json:"source"`
		}
		if json.Unmarshal([]byte(req.Input), &input) != nil || len(input.Source) == 0 {
			panic("missing complete source in free index fixture")
		}
		start, end := input.Source[0], input.Source[len(input.Source)-1]
		value := d.EpisodeValue{Title: "免费整篇范围夹具", SourceRanges: []d.SourceRange{{Start: d.SourcePoint{SpanID: start.ID}, End: d.SourcePoint{SpanID: end.ID, Offset: len([]rune(end.Text))}}}}
		body := map[string]any{"operations": []any{
			map[string]any{"op": "setSegmentation", "segmentation": "whole"},
			map[string]any{"op": "setSupportRanges", "ranges": []d.SourceRange{}},
			map[string]any{"op": "createEpisode", "value": value},
		}}
		raw, _ := json.Marshal(body)
		name, args = "save_episode_ranges", string(raw)
	}
	return ports.Response{Actions: []d.Action{{ID: "free-index-" + name, Name: name, Arguments: args, Status: "pending"}}, Raw: json.RawMessage(`{"freeIndexFixture":true}`), FinishReason: "tool_calls", Usage: billing.Usage{Known: true}}, true
}
