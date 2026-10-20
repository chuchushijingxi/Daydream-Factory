package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	d "drama-agent/internal/domain/project"
)

func TestModelMaterialStableSourcePrecedesChangingState(t *testing.T) {
	for _, kind := range []string{"extract", "correct", "outline", "visual", "index", "storyboard"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s, p := fixture(t, &fakeModel{})
			p.Assets = []d.Asset{{ID: "stable-asset", Kind: "character", Name: "人物", Description: "旧说明", SourceIDs: []string{p.ScriptSource.Spans[0].ID}}}
			p.LatestRun().End = len(p.ScriptSource.Spans)
			field := "assets"
			var update func()
			switch kind {
			case "extract", "correct":
				p.LatestRun().Stage = kind
				update = func() { p.Assets[0].Description = "实际修订，原文 p1 字样仍是内容" }
			case "outline":
				p.LatestRun().Contract, p.LatestRun().Stage = d.OutlineContract, "outline"
				p.Outline = &d.Outline{OutlineInput: d.OutlineInput{Summary: "旧梗概"}}
				field, update = "outline", func() { p.Outline.Summary = "修订梗概" }
			case "visual":
				p.LatestRun().Contract, p.LatestRun().Stage = d.VisualContract, "visualPrompt"
				p.LatestRun().Targets = []d.VisualTarget{{AssetID: "stable-asset"}}
				p.VisualPrompts = []d.VisualPrompt{{VisualPromptInput: d.VisualPromptInput{Target: p.LatestRun().Targets[0], Text: "旧正文"}, ActionRef: d.ActionRef{RunID: p.LatestRun().ID}, SourceVersion: p.ScriptSource.Version}}
				field, update = "visualPrompts", func() { p.VisualPrompts[0].Text = "修订后的完整正文" }
			case "index":
				s, _, p = p5Prepared(t)
				p.LatestRun().Contract, p.LatestRun().Stage = d.EpisodeIndexContract, "episodeIndex"
				p.LatestRun().Status = d.RunRunning
				p.LatestRun().StoryboardScope = &d.StoryboardScope{Operation: "index"}
				field, update = "currentIndex", func() { p.Storyboard.Index.Episodes[0].Title = "修订集名" }
			case "storyboard":
				s, _, p = p5Prepared(t)
				p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))})
				var err error
				p, err = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
				if err != nil {
					t.Fatal(err)
				}
				field, update = "currentStoryboard", func() { p.Storyboard.Scenes[0].Summary = "实际修订场景说明" }
			}
			originalSource := d.Clone(p).ScriptSource
			before, _, err := s.request(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			update()
			after, _, err := s.request(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "index" || kind == "storyboard" {
				if before.Input != after.Input || before.CurrentState == after.CurrentState || before.System != after.System {
					t.Fatal("latest state broke stable input or disappeared")
				}
				if !strings.Contains(before.CurrentState, `"`+field+`":`) || strings.Contains(before.Input, `"`+field+`":`) {
					t.Fatal("mutable state not isolated from stable input")
				}
				var oldState, currentState map[string]json.RawMessage
				json.Unmarshal([]byte(before.CurrentState), &oldState)
				json.Unmarshal([]byte(after.CurrentState), &currentState)
				if string(oldState[field]) == string(currentState[field]) || !reflect.DeepEqual(originalSource, p.ScriptSource) {
					t.Fatal("current state disappeared or source was changed")
				}
				if kind == "index" {
					var stable struct {
						Source []d.SourceSpan `json:"source"`
					}
					if json.Unmarshal([]byte(before.Input), &stable) != nil || !reflect.DeepEqual(stable.Source, modelSpans(p.ScriptSource)) {
						t.Fatal("full source lost or shortened")
					}
				} else if !strings.Contains(before.Input, p.ScriptSource.Spans[0].Text) {
					t.Fatal("complete episode source lost")
				}
				writeMaterialEvidence(t, kind+"-prefix.json", map[string]any{"before": before, "after": after, "stablePrefixBytes": len(before.Input), "mutableField": field})
				return
			}
			marker := `"` + field + `":`
			i, j := strings.Index(before.Input, marker), strings.Index(after.Input, marker)
			if i <= 0 || j != i || before.Input[:i] != after.Input[:j] || before.Input == after.Input || before.System != after.System {
				t.Fatal("changing state broke the stable material prefix", kind, i, j)
			}
			var old, current map[string]json.RawMessage
			if json.Unmarshal([]byte(before.Input), &old) != nil || json.Unmarshal([]byte(after.Input), &current) != nil {
				t.Fatal("invalid material")
			}
			if string(old[field]) == string(current[field]) {
				t.Fatal("current state disappeared")
			}
			delete(old, field)
			delete(current, field)
			if !reflect.DeepEqual(old, current) || !reflect.DeepEqual(originalSource, p.ScriptSource) {
				t.Fatal("ordering dropped material or changed original source")
			}
			if kind != "storyboard" {
				var spans []d.SourceSpan
				if json.Unmarshal(old["source"], &spans) != nil || !reflect.DeepEqual(spans, modelSpans(p.ScriptSource)) {
					t.Fatal("full source lost or shortened")
				}
			} else if !strings.Contains(string(old["targetSource"]), p.ScriptSource.Spans[0].Text) {
				t.Fatal("complete episode source lost")
			}
			t.Logf("%s: %d bytes before changing %s retained exactly", kind, i, field)
			writeMaterialEvidence(t, kind+"-prefix.json", map[string]any{"before": before, "after": after, "stablePrefixBytes": i, "mutableField": field})
		})
	}
}

func TestModelMaterialOrderPreservesAdditionalFieldsNumbersAndReceiptBytes(t *testing.T) {
	raw := `{"assets":[],"future":{"integer":9007199254740991,"text":"p1😀"},"source":[{"id":"p1","text":"完整原文"}],"sourceVersion":7}`
	ordered, err := orderModelMaterial(raw)
	if err != nil || !strings.HasPrefix(ordered, `{"sourceVersion":7,"source":`) || !strings.Contains(ordered, `"integer":9007199254740991`) {
		t.Fatal("lost fields or numeric precision", ordered, err)
	}
	var before, after map[string]json.RawMessage
	json.Unmarshal([]byte(raw), &before)
	json.Unmarshal([]byte(ordered), &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("field order changed values")
	}
	receipt := `{"changes":[],"ok":true,"sourceVersion":7}`
	got, err := modelSourceReceipt(d.Project{}, d.Call{SourceReferenceFormat: modelSourceReferenceFormat}, receipt)
	if err != nil || got != receipt {
		t.Fatal("request ordering leaked into tool receipt", err)
	}
	for _, invalid := range []string{"null", "[]", "not-json"} {
		if _, err := orderModelMaterial(invalid); err == nil {
			t.Fatal("invalid material changed to empty success", invalid)
		}
	}
}

func TestModelMaterialOrderRetainsLongPrefixAcrossCatalogUpdate(t *testing.T) {
	s, p := fixture(t, &fakeModel{})
	p.ScriptSource.Spans = nil
	for i := 0; i < 30; i++ {
		p.ScriptSource.Spans = append(p.ScriptSource.Spans, d.SourceSpan{ID: fmt.Sprintf("v1-p%06d", i+1), Text: strings.Repeat("完整原文😀，包含动作与对白。", 40)})
	}
	p.LatestRun().End, p.LatestRun().Stage = 30, "correct"
	p.Assets = []d.Asset{{ID: "stable-id", Kind: "character", Name: "具体人物", Description: "旧说明", SourceIDs: []string{p.ScriptSource.Spans[0].ID}}}
	before, _, err := s.request(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	p.Assets[0].Description = "本轮实际修订说明"
	after, _, err := s.request(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	prefix := func(a, b string) int {
		i := 0
		for i < len(a) && i < len(b) && a[i] == b[i] {
			i++
		}
		return i
	}
	alphabetical := func(input string) string {
		var value map[string]json.RawMessage
		if err := json.Unmarshal([]byte(input), &value); err != nil {
			t.Fatal(err)
		}
		return string(mustJSON(value))
	}
	oldPrefix, newPrefix := prefix(alphabetical(before.Input), alphabetical(after.Input)), prefix(before.Input, after.Input)
	if newPrefix < 30000 || newPrefix <= oldPrefix || len(before.Input) != len(alphabetical(before.Input)) {
		t.Fatal("source order did not retain complete long material or changed its size", oldPrefix, newPrefix)
	}
	t.Logf("same complete material, asset description changes: alphabetical root prefix=%d bytes; stable source prefix=%d bytes (not provider cache tokens)", oldPrefix, newPrefix)
	writeMaterialEvidence(t, "long-prefix.json", map[string]any{"before": before, "after": after, "alphabeticalPrefixBytes": oldPrefix, "stablePrefixBytes": newPrefix, "scope": "actual application requests; UTF-8 prefix comparison, not real provider cache hit or money saved"})
}

func TestEvidenceParameterFeedbackPreservesBoundariesAndOriginalArguments(t *testing.T) {
	for _, tc := range []struct {
		name, tool, args string
		want             []string
	}{
		{"after-high", "read_script_evidence", `{"spanIds":["s"],"after":12}`, []string{"after=12", "0～8"}},
		{"before-negative", "read_script_evidence", `{"spanIds":["s"],"before":-1}`, []string{"before=-1", "0～8"}},
		{"before-null", "read_script_evidence", `{"spanIds":["s"],"before":null}`, []string{"before=null", "0～8"}},
		{"after-type", "read_script_evidence", `{"spanIds":["s"],"after":"bad"}`, []string{"after", "int", "0～8"}},
		{"no-spans", "read_script_evidence", `{"spanIds":[]}`, []string{"spanIds", "数量=0", "1～32"}},
		{"too-many-spans", "read_script_evidence", string(mustJSON(map[string]any{"spanIds": strings.Fields(strings.Repeat("s ", 33))})), []string{"spanIds", "数量=33", "1～32"}},
		{"limit-zero", "search_script_evidence", `{"query":"原文","limit":0}`, []string{"limit=0", "1～20"}},
		{"limit-high", "search_script_evidence", `{"query":"原文","limit":21}`, []string{"limit=21", "1～20"}},
		{"limit-null", "search_script_evidence", `{"query":"原文","limit":null}`, []string{"limit=null", "1～20"}},
		{"offset-high", "search_script_evidence", `{"query":"原文","offset":16363}`, []string{"offset=16363", "0～16362"}},
		{"offset-null", "search_script_evidence", `{"query":"原文","offset":null}`, []string{"offset=null", "0～16362"}},
		{"blank-query", "search_script_evidence", `{"query":" "}`, []string{"query", "非空白", "1～1024"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p := evidenceFixture(t)
			e := &fixtureEvidence{}
			s.Evidence = e
			p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
			before := d.Clone(p)
			a := action(tc.tool, nil)
			a.Arguments = tc.args
			p, result := tool(t, s, p, a)
			var receipt struct {
				OK          bool
				Code, Error string
			}
			if json.Unmarshal([]byte(result.Text), &receipt) != nil || receipt.OK || receipt.Code != "INVALID_INPUT" || e.calls != 0 || strings.Contains(result.Text, `"source":`) {
				t.Fatal("rejection read partial data or called index", result.Text)
			}
			for _, want := range tc.want {
				if !strings.Contains(receipt.Error, want) {
					t.Fatal("feedback cannot guide correction", want, receipt.Error)
				}
			}
			stored := p.LatestRun().Calls[0].Actions[0]
			if stored.Arguments != tc.args || !reflect.DeepEqual(before.ScriptSource, p.ScriptSource) || !reflect.DeepEqual(before.Assets, p.Assets) || !reflect.DeepEqual(before.VisualPrompts, p.VisualPrompts) || before.UsedMicroCNY() != p.UsedMicroCNY() {
				t.Fatal("rejected request mutated inputs or historic fees")
			}
		})
	}
	// 同一整数边界保持：省略 limit 取 5，显式最大值及前后 8 段仍可用。
	s, p := evidenceFixture(t)
	e := &fixtureEvidence{}
	s.Evidence = e
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	p, result := tool(t, s, p, action("search_script_evidence", map[string]any{"query": "原文"}))
	if !strings.Contains(result.Text, `"ok":true`) || e.limit != 5 || e.offset != 0 {
		t.Fatal("omitted defaults changed", result.Text)
	}
	p, result = tool(t, s, p, action("search_script_evidence", map[string]any{"query": "原文", "limit": 20, "offset": 16362}))
	if !strings.Contains(result.Text, `"ok":true`) || e.limit != 20 || e.offset != 16362 {
		t.Fatal("valid upper search boundary changed", result.Text)
	}
	_, result = tool(t, s, p, action("read_script_evidence", map[string]any{"spanIds": []string{"s"}, "before": 8, "after": 8}))
	if !strings.Contains(result.Text, `"ok":true`) || !strings.Contains(result.Text, p.ScriptSource.Spans[2].Text) {
		t.Fatal("valid upper read boundary changed", result.Text)
	}
}

func writeMaterialEvidence(t *testing.T, name string, value any) {
	t.Helper()
	if dir := os.Getenv("DRAMA_TEST_MODEL_MATERIAL_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), mustJSON(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEvidenceParameterStoryboardSchemaFeedbackRetainsAtomicRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, tool, args string
		want             []string
	}{
		{"after-high", "read_script_evidence", `{"spanIds":["s1"],"after":12}`, []string{"after", "actual=12", "minimum=0", "maximum=8"}},
		{"before-null", "read_script_evidence", `{"spanIds":["s1"],"before":null}`, []string{"before", "type", "minimum=0", "maximum=8"}},
		{"limit-zero", "search_script_evidence", `{"query":"原文","limit":0}`, []string{"limit", "actual=0", "minimum=1", "maximum=20"}},
		{"offset-negative", "search_script_evidence", `{"query":"原文","offset":-1}`, []string{"offset", "actual=-1", "minimum=0", "maximum=16362"}},
		{"empty-spans", "read_script_evidence", `{"spanIds":[]}`, []string{"spanIds", "actual=0", "minItems=1", "maxItems=32"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, p := p5Prepared(t)
			var err error
			p, err = s.StartEpisode(context.Background(), p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
			if err != nil {
				t.Fatal(err)
			}
			index := &fixtureEvidence{}
			s.Evidence = index
			before := d.Clone(p)
			a := action(tc.tool, nil)
			a.Arguments = tc.args
			p, result := tool(t, s, p, a)
			var receipt struct {
				OK            bool
				Code, Message string
			}
			if json.Unmarshal([]byte(result.Text), &receipt) != nil || receipt.OK || receipt.Code != "INVALID_INPUT" || index.calls != 0 || strings.Contains(result.Text, `"source":`) {
				t.Fatal("P5 schema refusal performed partial evidence access", result.Text)
			}
			for _, want := range tc.want {
				if !strings.Contains(receipt.Message, want) {
					t.Fatal("P5 schema hid actual field/constraint", want, receipt.Message)
				}
			}
			if !reflect.DeepEqual(before.ScriptSource, p.ScriptSource) || !reflect.DeepEqual(before.Storyboard, p.Storyboard) || !reflect.DeepEqual(before.Assets, p.Assets) || p.LatestRun().Calls[0].Actions[0].Arguments != tc.args {
				t.Fatal("P5 refusal changed original content, scope or raw arguments")
			}
		})
	}
}

func TestModelMaterialInstructionsKeepRequiredReadsAndReview(t *testing.T) {
	if !strings.Contains(runTaskTools(&d.Run{Contract: d.EpisodeIndexContract})[0].Description, "不作为开始工作的必经步骤") || !strings.Contains(evidenceTools()[0].Description, "未提供的命中段") || !strings.Contains(evidenceTools()[1].Description, "合并同批补读") {
		t.Fatal("actual tool descriptions omit direct use / necessary reads")
	}
}
