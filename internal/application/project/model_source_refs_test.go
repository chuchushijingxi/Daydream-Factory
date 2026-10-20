package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/adapter/einoagent"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

func modelSpans(source d.ScriptSource) []d.SourceSpan {
	spans := append([]d.SourceSpan{}, source.Spans...)
	for i := range spans {
		spans[i].ID = fmt.Sprintf("p%d", i+1)
	}
	return spans
}

func TestModelSourceRefsOnlyTransformsStructuredReferences(t *testing.T) {
	source := d.ScriptSource{Version: 7, Spans: []d.SourceSpan{{ID: "v7-p000120", Text: "p1 与 v7-p000120 是原文内容😀"}, {ID: "opaque-B", Text: "第二段"}}}
	refs := sourceRefs(source)
	untouched := `{"ok":true,"changes":[{"revision":2,"text":"p1"}]}`
	for _, toModel := range []bool{true, false} {
		got, err := refs.rewrite(untouched, toModel)
		if err != nil || got != untouched {
			t.Fatal("receipt without source references was rewritten", err)
		}
	}
	raw := `{"source":[{"id":"v7-p000120","text":"p1 与 v7-p000120 是原文内容😀"}],"targetSpanIds":["opaque-B"],"asset":{"id":"asset_keep","name":"v7-p000120","description":"p1","sourceIds":["v7-p000120"]},"range":{"start":{"spanId":"opaque-B","offset":9007199254740991}},"query":"v7-p000120","providerState":"signed v7-p000120"}`
	encoded, err := refs.rewrite(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"id":"p1"`, `"targetSpanIds":["p2"]`, `"sourceIds":["p1"]`, `"spanId":"p2"`, `"offset":9007199254740991`, `"text":"p1 与 v7-p000120 是原文内容😀"`, `"name":"v7-p000120"`, `"providerState":"signed v7-p000120"`, `"id":"asset_keep"`} {
		if !strings.Contains(encoded, part) {
			t.Fatal("changed content or lost reference", part, encoded)
		}
	}
	decoded, err := refs.rewrite(encoded, false)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	json.Unmarshal([]byte(raw), &a)
	json.Unmarshal([]byte(decoded), &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("reference roundtrip lost data")
	}
	for _, bad := range []string{`{"spanIds":["p3"]}`, `{"sourceIds":["v8-p000120"]}`, `{"sourceIds":["other-project"]}`} {
		if _, err = refs.rewrite(bad, false); !errors.Is(err, fault.ErrInvalid) {
			t.Fatal("unknown reference accepted", bad, err)
		}
	}
	legacy := d.Call{}
	actual, err := sourceArguments(d.Project{ScriptSource: source}, legacy, raw)
	if err != nil || actual != raw {
		t.Fatal("legacy request rewritten")
	}
	if _, err = sourceArguments(d.Project{}, d.Call{SourceReferenceFormat: "unknown-format"}, `{}`); err == nil {
		t.Fatal("unknown format silently adopted")
	}
	second := sourceRefs(d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "another-project-source"}}})
	decoded, err = second.rewrite(`{"spanId":"p1"}`, false)
	if err != nil || !strings.Contains(decoded, "another-project-source") {
		t.Fatal("mapping leaked across project")
	}
}

func TestModelSourceRefsReceivedRecoveryPreservesHistoryAndStableIDs(t *testing.T) {
	for _, format := range []string{"", modelSourceReferenceFormat} {
		t.Run(fmt.Sprintf("format-%s", format), func(t *testing.T) {
			ctx := context.Background()
			m := &fakeModel{}
			s, p := fixture(t, m)
			p.ScriptSource.Spans = []d.SourceSpan{{ID: "opaque-A", Text: "第一段"}, {ID: "opaque-B", Text: "第二段"}}
			ref := "opaque-B"
			if format != "" {
				ref = "p2"
			}
			args := fmt.Sprintf(`{"assets":[{"kind":"character","name":"未命名人物","description":"正确共同动作","sourceIds":[%q]}]}`, ref)
			r := p.LatestRun()
			r.End = 2
			r.Calls = []d.Call{{ID: "received", Stage: "extract", Start: 0, End: 2, Status: "received", SourceReferenceFormat: format, ProviderState: `signed:{"p2":"opaque-B"}`, Request: json.RawMessage(`{"originalRequest":true}`), Response: `{"originalResponse":true}`, FinishReason: "tool_calls", Actions: []d.Action{{ID: "original-action", Name: "save_catalog", Arguments: args, Status: "pending"}}}}
			p, err := s.commit(ctx, p, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			m.generate = func(req ports.Request) (ports.Response, error) {
				current, _ := s.Project(ctx, p.ID)
				if len(current.Assets) != 1 || strings.Join(current.Assets[0].SourceIDs, ",") != "opaque-B" {
					t.Fatal("received alias not restored before continuing")
				}
				if m.count == 1 {
					if len(req.Dialogue) != 2 || req.Dialogue[0].Actions[0].Arguments != args || req.Dialogue[0].ProviderState != `signed:{"p2":"opaque-B"}` {
						t.Fatal("raw assistant arguments changed")
					}
					want := "opaque-B"
					if format != "" {
						want = "p2"
					}
					if !strings.Contains(req.Dialogue[1].Content, want) {
						t.Fatal("wrong original receipt view")
					}
				}
				return response(`{"assets":[]}`), nil
			}
			if err = s.Execute(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Project(ctx, p.ID)
			first := got.LatestRun().Calls[0]
			if first.Request == nil || string(first.Request) != `{"originalRequest":true}` || first.Response != `{"originalResponse":true}` || first.Actions[0].Arguments != args || !strings.Contains(first.Actions[0].Result, "opaque-B") || got.LatestRun().Status != d.RunCompleted {
				t.Fatal("history, internal source or completion lost")
			}
			id := got.Assets[0].ID
			calls := m.count
			if err = s.Execute(ctx, p.ID); err != nil || m.count != calls {
				t.Fatal("completed recovery resent request", err)
			}
			got, _ = s.Project(ctx, p.ID)
			if got.Assets[0].ID != id {
				t.Fatal("recovery allocated duplicate asset")
			}
		})
	}
}

func TestModelSourceRefsUnknownAliasRejectsWholeChange(t *testing.T) {
	ctx := context.Background()
	s, p := fixture(t, &fakeModel{})
	p.Assets = []d.Asset{{ID: "keep", Kind: "character", Name: "正确人物", SourceIDs: []string{"s1"}}}
	before := append([]d.Asset{}, p.Assets...)
	p.LatestRun().End = 2
	p.LatestRun().Calls = []d.Call{{ID: "alias-call", Stage: "extract", Start: 0, End: 2, Status: "received", SourceReferenceFormat: modelSourceReferenceFormat, FinishReason: "tool_calls", Actions: []d.Action{action("save_catalog", CatalogChangeSet{Assets: []CatalogAssetInput{{Kind: "prop", Name: "可用对象", SourceIDs: []string{"p1"}}, {Kind: "character", Name: "错误来源", SourceIDs: []string{"p999"}}}})}}}
	p, err := s.commit(ctx, p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	a := p.LatestRun().Calls[0].Actions[0]
	out, err := s.executeTool(ctx, p.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Project(ctx, p.ID)
	if !strings.Contains(out.Text, `"ok":false`) || !strings.Contains(out.Text, "p999") || !reflect.DeepEqual(before, got.Assets) || got.LatestRun().Calls[0].Actions[0].Arguments != a.Arguments {
		t.Fatal("invalid alias partially adopted or raw evidence changed", out.Text)
	}
}

func TestModelInputCompactionHTTPActualEinoKeepsCanonicalData(t *testing.T) {
	ctx := context.Background()
	m := &fakeModel{}
	s, p := fixture(t, m)
	s.Agent = einoagent.New()
	p.ScriptSource = d.ScriptSource{Version: 9, Spans: []d.SourceSpan{{ID: "v9-p000120", Text: "甲与乙共同搬运物件，原文含 p1 和 v9-p000120。"}, {ID: "opaque-middle", Text: "完整补查段落😀，不删减。"}, {ID: "v9-p000188", Text: "两人再次交接。"}}}
	p.LatestRun().SourceVersion = 9
	var err error
	p, err = s.commit(ctx, p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	originalSource := p.ScriptSource
	evidence := &fixtureEvidence{hits: []ports.EvidenceHit{{SpanID: "opaque-middle", Score: .9}}}
	s.Evidence = evidence
	var wireMu sync.Mutex
	calls := 0
	var wireRequests []json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		wireMu.Lock()
		defer wireMu.Unlock()
		wireRequests = append(wireRequests, append(json.RawMessage(nil), raw...))
		calls++
		var wire struct {
			Messages []struct {
				Role, Content string
				Reasoning     string `json:"reasoning_content"`
			}
		}
		if json.Unmarshal(raw, &wire) != nil || len(wire.Messages) < 2 {
			http.Error(w, "invalid wire", 500)
			return
		}
		var input struct {
			Source        []d.SourceSpan
			Assets        []d.Asset
			SourceVersion uint64
			Feedback      []any            `json:"lastToolResults"`
			VisualPrompts []map[string]any `json:"visualPrompts"`
		}
		if json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil {
			http.Error(w, "invalid material", 500)
			return
		}
		if input.SourceVersion != 9 || !strings.HasSuffix(wire.Messages[0].Content, modelSourceInstruction) || strings.Contains(wire.Messages[1].Content, `"original"`) {
			t.Error("actual model material missing format or deduplication")
		}
		if strings.Index(wire.Messages[1].Content, `"source":`) > strings.Index(wire.Messages[1].Content, `"assets":`) {
			t.Error("changing assets precede full source in actual HTTP wire")
		}
		for _, span := range input.Source {
			index := map[string]int{"p1": 0, "p2": 1, "p3": 2}[span.ID]
			if span.ID != "p1" && span.ID != "p2" && span.ID != "p3" || span.Text != originalSource.Spans[index].Text {
				t.Error("source identity or full text lost")
			}
		}
		current, _ := s.Project(ctx, p.ID)
		tool := ""
		var arguments any
		switch calls {
		case 1:
			tool = "save_catalog"
			arguments = CatalogChangeSet{Assets: []CatalogAssetInput{{Kind: "character", Name: "人物甲", Description: "共同动作 p1 v9-p000120", SourceIDs: []string{"p1", "p3"}}}}
		case 2, 4:
			if len(input.Feedback) != 0 || len(wire.Messages) != 4 {
				t.Error("receipt duplicated or actual dialogue missing")
			}
			if !strings.Contains(wire.Messages[3].Content, `"sourceIds":["p1","p3"]`) {
				t.Error("actual receipt not shortened")
			}
			tool = "finish_batch"
			arguments = struct{}{}
		case 3:
			a := current.Assets[0]
			if strings.Join(a.SourceIDs, ",") != "v9-p000120,v9-p000188" {
				t.Error("stored source IDs changed")
			}
			tool = "save_catalog"
			arguments = CatalogChangeSet{Assets: []CatalogAssetInput{{ID: a.ID, Kind: a.Kind, Name: a.Name, Description: "修正说明，保留 p1 v9-p000120 原文内容", SourceIDs: []string{"p1", "p3"}}}}
		case 5:
			if len(input.Source) != 2 || input.Source[0].ID != "p1" || input.Source[1].ID != "p3" {
				t.Error("linked evidence renumbered or scope lost")
			}
			tool = "search_script_evidence"
			arguments = SearchScriptEvidence{Query: "p1 v9-p000120 是查询内容"}
		case 6:
			if !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, `"spanId":"p2"`) {
				t.Error("search did not return stable global paragraph reference")
			}
			tool = "read_script_evidence"
			arguments = ReadScriptEvidence{SpanIDs: []string{"p2"}, After: 12}
		case 7:
			last := wire.Messages[len(wire.Messages)-1].Content
			if !strings.Contains(last, `"ok":false`) || !strings.Contains(last, "after=12") || !strings.Contains(last, "0～8") || strings.Contains(last, `"source":`) {
				t.Error("actual rejection omitted correction context or returned partial source", last)
			}
			tool = "read_script_evidence"
			arguments = ReadScriptEvidence{SpanIDs: []string{"p2"}, After: 1}
		case 8:
			if !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, originalSource.Spans[1].Text) {
				t.Error("read evidence was truncated")
			}
			tool = "read_script_evidence" // 明确重读仍执行，不能用降本规则抑制真实工具。
			arguments = ReadScriptEvidence{SpanIDs: []string{"p2"}}
		case 9:
			if !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, originalSource.Spans[1].Text) {
				t.Error("explicit reread lost complete actual receipt")
			}
			tool = "save_visual_prompts"
			arguments = SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput{AssetID: current.Assets[0].ID}, Text: "可用正文，p1 和 v9-p000120 是文字。"}}}
		case 10:
			if len(input.VisualPrompts) != 1 || input.VisualPrompts[0]["valid"] != true || input.VisualPrompts[0]["text"] != "可用正文，p1 和 v9-p000120 是文字。" {
				t.Error("compact visual input lost full text or current-task validity")
			}
			for _, prompt := range input.VisualPrompts {
				rejectMaterialFields(t, prompt, "runId", "callId", "actionId", "basePrompt")
			}
			if !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, `"actionId":`) || !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, `"basePrompt":null`) {
				t.Error("actual visual save receipt lost its full internal provenance")
			}
			tool = "finish_visual_prompts"
			arguments = struct{}{}
		default:
			http.Error(w, "unexpected extra request", 500)
			return
		}
		args := string(mustJSON(arguments))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": fmt.Sprintf("free-%d", calls), "choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "reasoning_content": "原始思考 p1 v9-p000120", "tool_calls": []any{map[string]any{"id": fmt.Sprintf("tool-%d", calls), "type": "function", "function": map[string]any{"name": tool, "arguments": args}}}}}}, "usage": map[string]int{"prompt_tokens": 0, "completion_tokens": 0}})
	}))
	defer server.Close()
	provider, err := modeladapter.New(ctx, "free-loopback-fixture", server.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	registry(s).Model = provider
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	wireMu.Lock()
	defer wireMu.Unlock()
	got, _ := s.Project(ctx, p.ID)
	if calls != 10 || got.LatestRun().Status != d.RunCompleted || got.UsedMicroCNY() != 0 || !reflect.DeepEqual(got.ScriptSource, originalSource) || evidence.query != "p1 v9-p000120 是查询内容" || !strings.Contains(got.Assets[0].Description, "p1 v9-p000120") {
		t.Fatal("pipeline lost data, changed query, failed or incurred cost", got.LatestRun().Error)
	}
	for _, c := range got.LatestRun().Calls {
		if c.SourceReferenceFormat != modelSourceReferenceFormat || c.Reasoning != "原始思考 p1 v9-p000120" {
			t.Fatal("format or raw reasoning not retained")
		}
	}
	if !strings.Contains(got.LatestRun().Calls[0].Actions[0].Arguments, `"p1"`) || !strings.Contains(got.LatestRun().Calls[0].Actions[0].Result, "v9-p000120") {
		t.Fatal("raw argument or canonical receipt lost")
	}
	if root := os.Getenv("DRAMA_TEST_MODEL_INPUT_EVIDENCE_DIR"); root != "" {
		if err = os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, "http-eino.json"), mustJSON(map[string]any{"project": got, "actualWireRequests": wireRequests, "freeFixture": true}), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestModelInputCompactionDeduplicatesEvidenceWithoutDroppingText(t *testing.T) {
	m := &fakeModel{}
	s, p := fixture(t, m)
	p.ScriptSource.Spans = nil
	for i := 0; i < 50; i++ {
		p.ScriptSource.Spans = append(p.ScriptSource.Spans, d.SourceSpan{ID: fmt.Sprintf("v1-p%06d", i+1), Text: fmt.Sprintf("第%d段：", i+1) + strings.Repeat("完整原文😀，p1 是内容。", 25)})
	}
	p.Assets = nil
	for i := 0; i < 20; i++ {
		p.Assets = append(p.Assets, d.Asset{ID: fmt.Sprintf("asset-%d", i), Kind: "character", Name: fmt.Sprintf("人物%d", i), SourceIDs: []string{p.ScriptSource.Spans[5].ID, p.ScriptSource.Spans[25].ID}})
	}
	p.LatestRun().Stage = "correct"
	p.LatestRun().End = 50
	req, _, err := s.request(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var material struct {
		Source []d.SourceSpan
		Assets []d.Asset
	}
	if err = json.Unmarshal([]byte(req.Input), &material); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(material.Source, modelSpans(p.ScriptSource)) || len(material.Assets) != 20 || strings.Contains(req.Input, `"original"`) {
		t.Fatal("deduplication removed complete text or retained original copies")
	}
	oldAssets := []map[string]any{}
	for _, a := range p.Assets {
		v := map[string]any{}
		_ = json.Unmarshal(mustJSON(a), &v)
		v["original"] = []d.SourceSpan{p.ScriptSource.Spans[5], p.ScriptSource.Spans[25]}
		oldAssets = append(oldAssets, v)
	}
	ids := []string{}
	for _, span := range p.ScriptSource.Spans {
		ids = append(ids, span.ID)
	}
	legacy := req
	legacy.System = CorrectPrompt
	legacy.Input = string(mustJSON(map[string]any{"targetSpanIds": ids, "source": p.ScriptSource.Spans, "assets": oldAssets}))
	oldSize, newSize := inputSize(legacy)+len(mustJSON(legacy.Tools)), inputSize(req)+len(mustJSON(req.Tools))
	if newSize >= oldSize {
		t.Fatal("actual compact input did not reduce complete request", oldSize, newSize)
	}
	t.Logf("equivalent fixture complete input+tools: old shape=%d bytes, compact=%d bytes; source 50/50 retained", oldSize, newSize)
	if root := os.Getenv("DRAMA_TEST_MODEL_INPUT_EVIDENCE_DIR"); root != "" {
		if err = os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, "size-comparison.json"), mustJSON(map[string]any{"oldShapeBytes": oldSize, "compactBytes": newSize, "sourceSpansBefore": 50, "sourceSpansAfter": len(material.Source), "actualCompactRequest": req, "unit": "UTF-8 byte based conservative input size, not provider tokens or currency"}), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestModelSourceRefsPendingAndUnknownNeverResend(t *testing.T) {
	for _, status := range []d.CallStatus{"pending", "unknown"} {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			m := &fakeModel{}
			s, p := fixture(t, m)
			p.LatestRun().Calls = []d.Call{{ID: "unresolved", Stage: "extract", Start: 0, End: 2, Status: status, SourceReferenceFormat: modelSourceReferenceFormat, Request: json.RawMessage(`{"keep":"original request"}`), Response: `original response`, ReservedMicroCNY: 777}}
			p, err := s.commit(ctx, p, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			before := p.UsedMicroCNY()
			if err = s.Execute(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Project(ctx, p.ID)
			c := got.LatestRun().Calls[0]
			if m.count != 0 || got.UsedMicroCNY() != before || got.LatestRun().ErrorCode != "MODEL_RESULT_UNKNOWN" || string(c.Request) != `{"keep":"original request"}` || c.Response != "original response" || c.SourceReferenceFormat != modelSourceReferenceFormat {
				t.Fatal("unknown call resent or original evidence/reservation lost")
			}
		})
	}
}
