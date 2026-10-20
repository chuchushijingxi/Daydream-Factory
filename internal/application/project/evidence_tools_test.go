package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"drama-agent/internal/adapter/milvusevidence"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

type fixtureEvidence struct {
	doc                  ports.EvidenceDocument
	query                string
	limit, offset, calls int
	hits                 []ports.EvidenceHit
	more                 bool
	err                  error
	before               func()
}
type evidenceCommitFailure struct{ ports.ProjectStore }

func (e evidenceCommitFailure) Commit(context.Context, d.Project, uint64) error {
	return errors.New("injected evidence receipt commit failure")
}
func (e *fixtureEvidence) Ensure(context.Context, ports.EvidenceDocument) error { return e.err }
func (e *fixtureEvidence) Search(_ context.Context, doc ports.EvidenceDocument, q string, limit, offset int) ([]ports.EvidenceHit, bool, error) {
	e.doc, e.query, e.limit, e.offset = doc, q, limit, offset
	e.calls++
	if e.before != nil {
		e.before()
	}
	return e.hits, e.more, e.err
}

func evidenceFixture(t *testing.T) (*Service, d.Project) {
	t.Helper()
	s, p := contentFixture(t, contentAssets())
	p.ScriptSource.Spans = append(p.ScriptSource.Spans, d.SourceSpan{ID: "s2", Text: "她脱下帽子，左眉的旧伤清晰可见。完整补查证据。"}, d.SourceSpan{ID: "s3", Text: "另一处完全无关的庭院里，陌生人整理花盆。"})
	p.Assets[0].Looks = append(p.Assets[0].Looks, d.Look{ID: "other-look", Name: "不应提供的另一造型", Description: "无关服装"})
	p, err := s.commit(context.Background(), p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestEvidenceSingleMaterialContainsOnlyTargetAndLinkedSource(t *testing.T) {
	s, p := evidenceFixture(t)
	before := d.Clone(p)
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	req, _, err := s.contentRequest(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var material struct {
		Source          []d.SourceSpan
		Assets          []d.Asset
		SourceCoverage  string
		SourceSpanCount int
	}
	if err = json.Unmarshal([]byte(req.Input), &material); err != nil {
		t.Fatal(err)
	}
	if len(material.Source) != 1 || len(material.Assets) != 1 || len(material.Assets[0].Looks) != 0 || material.SourceSpanCount != 3 || material.SourceCoverage != "linked_evidence_only_not_complete" || strings.Contains(req.Input, "完整补查证据") || strings.Contains(req.Input, "庭院") {
		t.Fatal("single task leaked unrelated original or lost material scope", req.Input)
	}
	if !reflect.DeepEqual(before.Assets, p.Assets) || !reflect.DeepEqual(before.ScriptSource, p.ScriptSource) {
		t.Fatal("request projection changed saved data")
	}
	if len(req.Tools) != 4 || len(Tools()) != 2 || len(TaskTools(d.OutlineContract)) != 2 {
		t.Fatal("tools not isolated by task")
	}
}

func TestEvidenceSearchReadReceiptsAndIdempotentRecovery(t *testing.T) {
	s, p := evidenceFixture(t)
	e := &fixtureEvidence{hits: []ports.EvidenceHit{{SpanID: "s2", Score: .8}}, more: true}
	s.Evidence = e
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	search := action("search_script_evidence", SearchScriptEvidence{Query: "此人的旧伤和服装变化", Limit: 2, Offset: 1})
	p, res := tool(t, s, p, search)
	if !strings.Contains(res.Text, `"ok":true`) || !strings.Contains(res.Text, `"nextOffset":2`) || e.doc.ProjectID != p.ID || e.doc.Source.Version != 1 || e.query != "此人的旧伤和服装变化" || e.limit != 2 || e.offset != 1 {
		t.Fatal(res.Text, e)
	}
	if _, err := s.executeTool(context.Background(), p.ID, search.ID); err != nil || e.calls != 1 {
		t.Fatal("committed query repeated on recovery", err, e.calls)
	}
	p, res = tool(t, s, p, action("read_script_evidence", ReadScriptEvidence{SpanIDs: []string{"s2"}, Before: 1}))
	if !strings.Contains(res.Text, "完整补查证据") || !strings.Contains(res.Text, p.ScriptSource.Spans[0].Text) || strings.Contains(res.Text, "庭院") {
		t.Fatal(res.Text)
	}
	req, _, err := s.contentRequest(context.Background(), p)
	if err != nil || len(req.Dialogue) != 4 || req.Dialogue[1].Content == "" || req.Dialogue[3].Content != res.Text {
		t.Fatal("actual observations missing in next request", err)
	}
	if strings.Contains(req.Input, "完整补查证据") {
		t.Fatal("retrieved evidence copied into a second competing material store")
	}
	p = singleSave(t, s, p, d.VisualTarget{AssetID: "c"}, "修正后保留左眉疤、黑发和其余正确特征")
	p = singleFinish(t, s, p)
	if p.LatestRun().Status != "completed" || p.Assets[0].Description != "完整 P3 说明" || p.UsedMicroCNY() != 999 {
		t.Fatal("query changed assets/history", p.LatestRun())
	}
}

func TestEvidenceRejectsForeignStaleUnknownAndUnavailableWithoutPartialRead(t *testing.T) {
	for _, arguments := range []string{`{"spanIds":["s","foreign"]}`, `{"spanIds":["v99-p000001"]}`, `{"spanIds":["s"],"projectId":"other"}`, `{"spanIds":[],"before":0}`, `{"spanIds":["s"],"after":9}`, `{"spanIds":null}`} {
		s, p := evidenceFixture(t)
		p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
		before := d.Clone(p)
		a := action("read_script_evidence", nil)
		a.Arguments = arguments
		p, res := tool(t, s, p, a)
		if !strings.Contains(res.Text, `"ok":false`) || strings.Contains(res.Text, `"source":`) || !reflect.DeepEqual(before.Assets, p.Assets) || !reflect.DeepEqual(before.VisualPrompts, p.VisualPrompts) {
			t.Fatal(arguments, res.Text)
		}
	}
	for _, e := range []*fixtureEvidence{{err: fault.New("EVIDENCE_UNAVAILABLE", "offline")}, {hits: []ports.EvidenceHit{{SpanID: "foreign"}}}} {
		s, p := evidenceFixture(t)
		s.Evidence = e
		p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
		p, res := tool(t, s, p, action("search_script_evidence", SearchScriptEvidence{Query: "衣服"}))
		if !strings.Contains(res.Text, `"ok":false`) || strings.Contains(res.Text, `"matches":`) || p.LatestRun().Status != "running" {
			t.Fatal(res.Text)
		}
	}
}

func TestEvidenceSameResponseObservationCannotBeSkippedAndReadCapacityIsExplicit(t *testing.T) {
	s, p := evidenceFixture(t)
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	p = singleSave(t, s, p, d.VisualTarget{AssetID: "c"}, "已保存正确正文")
	read := action("read_script_evidence", ReadScriptEvidence{SpanIDs: []string{"s2"}})
	finish := action("finish_visual_prompts", struct{}{})
	p = recordProjectContent(t, s, p, read, finish)
	if _, err := s.executeTool(context.Background(), p.ID, read.ID); err != nil {
		t.Fatal(err)
	}
	res, err := s.executeTool(context.Background(), p.ID, finish.ID)
	if err != nil || !strings.Contains(res.Text, `"ok":false`) || res.Finished {
		t.Fatal("read receipt skipped", res, err)
	}
	p, _ = s.Store.Snapshot(context.Background(), p.ID)
	p = singleFinish(t, s, p)
	if p.LatestRun().Status != "completed" {
		t.Fatal("receipt loop cannot recover")
	}

	s, p = evidenceFixture(t)
	p.ScriptSource.Spans[1].Text = strings.Repeat("完整不能截断", 10000)
	p, err = s.commit(context.Background(), p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	registry(s).Model = &fakeModel{window: 30000, output: 1000}
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	p, res = tool(t, s, p, action("read_script_evidence", ReadScriptEvidence{SpanIDs: []string{"s2"}}))
	if !strings.Contains(res.Text, "EVIDENCE_CAPACITY_EXCEEDED") || strings.Contains(res.Text, `"source":`) || p.LatestRun().Status != "running" {
		t.Fatal(res.Text)
	}
}

func TestEvidenceCancelledAndRollbackNeverAdoptObservation(t *testing.T) {
	s, p := evidenceFixture(t)
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	a := action("read_script_evidence", ReadScriptEvidence{SpanIDs: []string{"s2"}})
	p = recordProjectContent(t, s, p, a)
	p, err := s.Cancel(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.executeTool(context.Background(), p.ID, a.ID); !errors.Is(err, fault.ErrCancelled) {
		t.Fatal(err)
	}
	if p.LatestRun().Calls[0].Actions[0].Result != "" {
		t.Fatal("cancelled observation adopted")
	}

	s, p = evidenceFixture(t)
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	p = recordProjectContent(t, s, p, a)
	before := d.Clone(p)
	s.Store = evidenceCommitFailure{ProjectStore: s.Store}
	if _, err = s.executeTool(context.Background(), p.ID, a.ID); err == nil {
		t.Fatal("failed commit reported as successful read")
	}
	after, _ := s.Store.Snapshot(context.Background(), p.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read receipt partially saved")
	}
}

func TestEvidenceCancellationDuringSearchCannotCommitLateResult(t *testing.T) {
	s, p := evidenceFixture(t)
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	a := action("search_script_evidence", SearchScriptEvidence{Query: "旧伤"})
	p = recordProjectContent(t, s, p, a)
	s.Evidence = &fixtureEvidence{hits: []ports.EvidenceHit{{SpanID: "s2", Score: .8}}, before: func() {
		latest, err := s.Store.Snapshot(context.Background(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Cancel(context.Background(), p.ID, latest.Revision); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := s.executeTool(context.Background(), p.ID, a.ID); !errors.Is(err, fault.ErrConflict) {
		t.Fatal("late observation bypassed cancellation/CAS", err)
	}
	latest, err := s.Store.Snapshot(context.Background(), p.ID)
	if err != nil || latest.LatestRun().Status != "cancelled" || latest.LatestRun().Calls[0].Actions[0].Result != "" {
		t.Fatal("late search result adopted", err)
	}
}

func TestEvidenceIndexFailureBeforeTextCallAndStrictQueryArguments(t *testing.T) {
	s, p := evidenceFixture(t)
	s.Evidence = &fixtureEvidence{err: fault.New("EVIDENCE_UNAVAILABLE", "injected encoder failure")}
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	if err := s.Execute(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	p, err := s.Store.Snapshot(context.Background(), p.ID)
	if err != nil || p.LatestRun().Status != "failed" || p.LatestRun().ErrorCode != "EVIDENCE_UNAVAILABLE" || len(p.LatestRun().Calls) != 0 || p.UsedMicroCNY() != 999 {
		t.Fatal("failed index sent text or changed accounting", err)
	}
	for _, raw := range []string{`{"query":"衣服","limit":0}`, `{"query":"衣服","limit":null}`, `{"query":"衣服","offset":null}`, `{"query":"衣服","offset":16363}`, `{"query":"衣服","sourceVersion":2}`} {
		s, p = evidenceFixture(t)
		e := &fixtureEvidence{}
		s.Evidence = e
		p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
		a := action("search_script_evidence", nil)
		a.Arguments = raw
		_, res := tool(t, s, p, a)
		if !strings.Contains(res.Text, `"ok":false`) || e.calls != 0 {
			t.Fatal("query schema/parse mismatch", raw, res.Text)
		}
	}
}

func TestEvidenceEscapedReceiptRejectedBeforeNextRequestOverflows(t *testing.T) {
	for _, pattern := range []string{`"`, `\`, "\n\t"} {
		t.Run(fmt.Sprintf("escaped-%x", pattern), func(t *testing.T) {
			s, p := evidenceFixture(t)
			p.ScriptSource.Spans[1].Text = strings.Repeat(pattern, 3000)
			p, err := s.commit(context.Background(), p, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
			read := action("read_script_evidence", ReadScriptEvidence{SpanIDs: []string{"s2"}})
			p = recordProjectContent(t, s, p, read)
			base := d.Clone(p)
			base.LatestRun().Calls = nil
			req, _, err := s.contentRequest(context.Background(), base)
			if err != nil {
				t.Fatal(err)
			}
			observation := map[string]any{"sourceVersion": p.ScriptSource.Version, "source": []d.SourceSpan{p.ScriptSource.Spans[1]}, "coverage": "requested_paragraphs_only"}
			raw, _ := json.Marshal(observation)
			tools, _ := json.Marshal(req.Tools)
			call, _ := json.Marshal(p.LatestRun().Calls[0].Actions)
			// Reproduce the old acceptance threshold, then compare with the real
			// next Dialogue containing the complete, twice-encoded receipt.
			window := inputSize(req) + len(tools) + len(raw) + len(call) + 1024 + 128 + 1
			candidate := d.Clone(p)
			observation["ok"], observation["status"], observation["stage"], observation["revision"] = true, "running", candidate.LatestRun().Stage, candidate.Revision+1
			raw, _ = json.Marshal(observation)
			candidate.LatestRun().Calls[0].Actions[0].Status = "applied"
			candidate.LatestRun().Calls[0].Actions[0].Result = string(raw)
			registry(s).Model = &fakeModel{window: window, output: 1000}
			if _, _, err = s.contentRequest(context.Background(), candidate); !errors.Is(err, fault.ErrCapacity) {
				t.Fatal("fixture does not reproduce the escaped-receipt overflow", err)
			}
			res, err := s.executeTool(context.Background(), p.ID, read.ID)
			if err != nil || !strings.Contains(res.Text, "EVIDENCE_CAPACITY_EXCEEDED") || strings.Contains(res.Text, `"source":`) {
				t.Fatalf("oversized success receipt adopted: receiptBytes=%d err=%v", len(res.Text), err)
			}
			p, _ = s.Store.Snapshot(context.Background(), p.ID)
			req, _, err = s.contentRequest(context.Background(), p)
			if err != nil || req.Dialogue[len(req.Dialogue)-1].Content != res.Text || p.LatestRun().Status != "running" {
				t.Fatal("capacity rejection cannot reach the next model request", err)
			}
			p, res = tool(t, s, p, action("read_script_evidence", ReadScriptEvidence{SpanIDs: []string{"s"}}))
			if !strings.Contains(res.Text, `"ok":true`) || !reflect.DeepEqual(p.Assets, base.Assets) || p.ScriptSource.Spans[1].Text != strings.Repeat(pattern, 3000) || registry(s).Model.(*fakeModel).count != 0 {
				t.Fatal("smaller read or original data preservation failed", res.Text)
			}
		})
	}
}

func TestEvidenceCapacityIncludesSiblingReceiptsInSameResponse(t *testing.T) {
	s, p := evidenceFixture(t)
	p.ScriptSource.Spans[2].Text = strings.Repeat(`\"`, 3000)
	p, err := s.commit(context.Background(), p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
	first := action("read_script_evidence", ReadScriptEvidence{SpanIDs: []string{"s2"}})
	second := action("read_script_evidence", ReadScriptEvidence{SpanIDs: []string{"s3"}})
	p = recordProjectContent(t, s, p, first, second)
	candidate := d.Clone(p)
	for i, span := range p.ScriptSource.Spans[1:] {
		raw, _ := json.Marshal(map[string]any{"ok": true, "status": "running", "stage": candidate.LatestRun().Stage, "revision": candidate.Revision + uint64(i) + 1, "sourceVersion": p.ScriptSource.Version, "source": []d.SourceSpan{span}, "coverage": "requested_paragraphs_only"})
		candidate.LatestRun().Calls[0].Actions[i].Status = "applied"
		candidate.LatestRun().Calls[0].Actions[i].Result = string(raw)
	}
	req, _, err := s.contentRequest(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := json.Marshal(req.Tools)
	registry(s).Model = &fakeModel{window: inputSize(req) + len(tools) - 1, output: 1000}
	firstResult, err := s.executeTool(context.Background(), p.ID, first.ID)
	if err != nil || !strings.Contains(firstResult.Text, `"ok":true`) {
		t.Fatal(firstResult.Text, err)
	}
	secondResult, err := s.executeTool(context.Background(), p.ID, second.ID)
	if err != nil || !strings.Contains(secondResult.Text, "EVIDENCE_CAPACITY_EXCEEDED") || strings.Contains(secondResult.Text, `"source":`) {
		t.Fatalf("second oversized receipt adopted: receiptBytes=%d err=%v", len(secondResult.Text), err)
	}
	p, _ = s.Store.Snapshot(context.Background(), p.ID)
	req, _, err = s.contentRequest(context.Background(), p)
	if err != nil || len(req.Dialogue) != 3 || req.Dialogue[1].Content != firstResult.Text || req.Dialogue[2].Content != secondResult.Text {
		t.Fatal("sibling success/rejection receipts lost or unable to fit", err)
	}
}

func TestUnavailableEvidencePreservesCatalogAndBlocksVisualPhase(t *testing.T) {
	var initialized int
	model := &fakeModel{}
	model.generate = func(req ports.Request) (ports.Response, error) {
		var input struct{ Assets []d.Asset }
		if err := json.Unmarshal([]byte(req.Input), &input); err != nil {
			t.Fatal(err)
		}
		draft := CatalogChangeSet{Assets: []CatalogAssetInput{{Kind: "character", Name: "祖父", Description: "虚构原文人物", SourceIDs: []string{"s1"}}}}
		if len(input.Assets) > 0 {
			draft.Assets = catalogInputs(input.Assets)
		}
		raw, _ := json.Marshal(draft)
		return response(string(raw)), nil
	}
	s, p := fixture(t, model)
	s.Evidence = milvusevidence.NewLazy(func() (ports.EvidenceIndex, error) {
		initialized++
		return nil, errors.New("fictional missing embedding runtime")
	})
	s.Importer = scriptimport.Importer{}
	ctx := context.Background()
	uploaded, err := s.Create(ctx, "虚构上传", "fictional.txt", []byte("守门人在小院里休息。"), 0)
	if err != nil {
		t.Fatal("upload depended on embedding", err)
	}
	if read, err := s.Project(ctx, uploaded.ID); err != nil || read.ScriptSource.Spans[0].Text != "守门人在小院里休息。" {
		t.Fatal("read depended on embedding", err)
	}
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal("P3 depended on embedding", err)
	}
	p, _ = s.Project(ctx, p.ID)
	if p.LatestRun().Status != "failed" || p.LatestRun().Stage != "visualPrompt" || p.LatestRun().ErrorCode != "EVIDENCE_UNAVAILABLE" || model.count != 2 || initialized != 1 || len(p.Assets) != 1 {
		t.Fatal("P3 flow changed or initialized evidence", model.count, initialized)
	}
	asset := p.Assets[0]
	p, err = s.SaveAsset(ctx, p.ID, p.Revision, asset.ID, d.AssetInput{Kind: asset.Kind, Name: asset.Name, Aliases: []string{}, Character: &d.Character{}, Description: "用户编辑的完整说明"})
	if err != nil || initialized != 1 || model.count != 2 {
		t.Fatal("ordinary edit depended on embedding", err)
	}
	before := d.Clone(p)
	p = singleStart(t, s, p, d.VisualTarget{AssetID: asset.ID})
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Project(ctx, p.ID)
	if initialized != 2 || model.count != 2 || p.LatestRun().Status != "failed" || p.LatestRun().ErrorCode != "EVIDENCE_UNAVAILABLE" || len(p.LatestRun().Calls) != 0 || p.UsedMicroCNY() != before.UsedMicroCNY() || !reflect.DeepEqual(before.Assets, p.Assets) || !reflect.DeepEqual(before.ScriptSource, p.ScriptSource) {
		t.Fatal("visual failure created calls/fees or changed data", initialized, model.count)
	}
}
