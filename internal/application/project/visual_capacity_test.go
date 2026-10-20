package project

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func capacitySave(text string) d.Action {
	return action("save_visual_prompts", SaveVisualPrompts{Prompts: []VisualPromptInput{{Target: VisualTargetInput{AssetID: "c"}, Text: text, DesignNotes: "fictional capacity fixture"}}})
}

func previewCapacityAction(t *testing.T, p *d.Project, index int) {
	t.Helper()
	r := p.LatestRun()
	c := &r.Calls[len(r.Calls)-1]
	a := &c.Actions[index]
	payload := map[string]any{}
	if a.Name == "read_script_evidence" {
		payload = map[string]any{"sourceVersion": p.ScriptSource.Version, "source": []d.SourceSpan{p.ScriptSource.Spans[1]}, "coverage": "requested_paragraphs_only"}
	} else {
		var draft SaveVisualPrompts
		if err := decodeTool(a.Arguments, &draft); err != nil {
			t.Fatal(err)
		}
		changes, invalidated, err := p.SaveVisualPrompts(r, d.ActionRef{RunID: r.ID, CallID: c.ID, ActionID: a.ID}, draft.Contents())
		if err != nil {
			t.Fatal(err)
		}
		payload["changes"], payload["invalidatedTargets"] = changes, invalidated
	}
	setContentToolReceipt(p, a, payload, nil)
}

func visualRequestBytes(t *testing.T, s *Service, p d.Project) int {
	t.Helper()
	req, _, err := s.contentRequest(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := json.Marshal(req.Tools)
	return inputSize(req) + len(tools)
}

func TestVisualCapacityMixedToolsRejectOnlyOverflowingActionAndContinue(t *testing.T) {
	for _, order := range []string{"read-save", "save-read", "save-save"} {
		t.Run(order, func(t *testing.T) {
			ctx := context.Background()
			s, p := evidenceFixture(t)
			p.ScriptSource.Spans[1].Text = strings.Repeat("fictional evidence ", 400)
			var err error
			p, err = s.commit(ctx, p, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
			original := d.Clone(p)
			read := action("read_script_evidence", ReadScriptEvidence{SpanIDs: []string{"s2"}})
			first, second := read, capacitySave(strings.Repeat("visual ", 800))
			if order == "save-read" {
				first, second = second, read
			} else if order == "save-save" {
				first = capacitySave("正确的初始正文")
			}
			p = recordProjectContent(t, s, p, first, second)
			preview := d.Clone(p)
			previewCapacityAction(t, &preview, 0)
			setContentToolReceipt(&preview, &preview.LatestRun().Calls[0].Actions[1], map[string]any{}, errEvidenceCapacity)
			window := visualRequestBytes(t, s, preview) + 2400
			model := &fakeModel{window: window, output: 16000}
			registry(s).Model = model
			result, err := s.executeTool(ctx, p.ID, first.ID)
			if err != nil || !strings.Contains(result.Text, `"ok":true`) {
				t.Fatal("first action should fit", err, len(result.Text))
			}
			beforeSecond, _ := s.Store.Snapshot(ctx, p.ID)
			result, err = s.executeTool(ctx, p.ID, second.ID)
			if err != nil || !strings.Contains(result.Text, `"ok":false`) || !strings.Contains(result.Text, "CAPACITY_EXCEEDED") || strings.Contains(result.Text, `"changes":`) || strings.Contains(result.Text, `"source":`) {
				t.Fatalf("overflowing %s adopted: receiptBytes=%d err=%v", second.Name, len(result.Text), err)
			}
			p, _ = s.Store.Snapshot(ctx, p.ID)
			if !reflect.DeepEqual(beforeSecond.VisualPrompts, p.VisualPrompts) || !reflect.DeepEqual(original.Assets, p.Assets) || !reflect.DeepEqual(original.ScriptSource, p.ScriptSource) || p.UsedMicroCNY() != original.UsedMicroCNY() {
				t.Fatal("rejection changed prior content/source/accounting")
			}
			req, _, err := s.contentRequest(ctx, p)
			if err != nil || req.Dialogue[len(req.Dialogue)-1].Content != result.Text || req.Dialogue[1].Content != beforeSecond.LatestRun().Calls[0].Actions[0].Result {
				t.Fatal("actual success/rejection receipts cannot reach next input", err)
			}
			if second.Name == "read_script_evidence" {
				p, result = tool(t, s, p, action(second.Name, ReadScriptEvidence{SpanIDs: []string{"s"}}))
			} else {
				p, result = tool(t, s, p, capacitySave("修正后的完整小范围正文"))
			}
			if !strings.Contains(result.Text, `"ok":true`) {
				t.Fatal("smaller action cannot repair the rejected operation", result.Text)
			}
			p = singleFinish(t, s, p)
			if p.LatestRun().Status != "completed" || model.count != 0 {
				t.Fatal("receipt loop did not complete without model execution")
			}
		})
	}
}

func TestVisualCapacityRejectedBaseRevisionRollsBackChildInvalidation(t *testing.T) {
	ctx := context.Background()
	s, p := evidenceFixture(t)
	base, look := d.VisualTarget{AssetID: "c"}, d.VisualTarget{AssetID: "c", LookID: "l"}
	p = singleStart(t, s, p, base)
	p = singleSave(t, s, p, base, "此前正确基础正文")
	p = singleFinish(t, s, p)
	p = singleStart(t, s, p, look)
	p = singleSave(t, s, p, look, "此前正确附加正文")
	p = singleFinish(t, s, p)
	p = singleStart(t, s, p, base)
	save := capacitySave(strings.Repeat(`\"`, 3000))
	p = recordProjectContent(t, s, p, save)
	before := d.Clone(p)
	rejected := d.Clone(p)
	setContentToolReceipt(&rejected, &rejected.LatestRun().Calls[0].Actions[0], map[string]any{}, fault.New("CAPACITY_EXCEEDED", "本次保存后的完整正文、依赖变化与回执无法放入下一轮上下文，请减少本次保存范围或正文长度"))
	registry(s).Model = &fakeModel{window: visualRequestBytes(t, s, rejected) + 2400, output: 16000}
	result, err := s.executeTool(ctx, p.ID, save.ID)
	if err != nil || !strings.Contains(result.Text, `"ok":false`) || !strings.Contains(result.Text, "CAPACITY_EXCEEDED") || strings.Contains(result.Text, "invalidatedTargets") {
		t.Fatalf("oversized base revision adopted: bytes=%d err=%v", len(result.Text), err)
	}
	p, _ = s.Store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(before.VisualPrompts, p.VisualPrompts) || !p.ValidPrompt(p.Prompt(look), "") || p.Prompt(look).Stale {
		t.Fatal("rejected base save changed the valid child or its dependency")
	}
	if _, _, err = s.contentRequest(ctx, p); err != nil {
		t.Fatal("rejection receipt cannot be consumed", err)
	}
}

type capacityCallbackModel struct {
	ports.Model
	before func()
}

func (m *capacityCallbackModel) Capacity() (int, int) {
	if m.before != nil {
		before := m.before
		m.before = nil
		before()
	}
	return m.Model.Capacity()
}

func TestVisualCapacityCommitFailureAndConcurrentCancellationDoNotAdopt(t *testing.T) {
	for _, scenario := range []string{"commit", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			s, p := evidenceFixture(t)
			p = singleStart(t, s, p, d.VisualTarget{AssetID: "c"})
			save := capacitySave("合法完整正文")
			p = recordProjectContent(t, s, p, save)
			before := d.Clone(p)
			if scenario == "commit" {
				s.Store = evidenceCommitFailure{ProjectStore: s.Store}
			} else {
				registry(s).Model = &capacityCallbackModel{Model: registry(s).Model, before: func() {
					latest, err := s.Store.Snapshot(ctx, p.ID)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = s.Cancel(ctx, p.ID, latest.Revision); err != nil {
						t.Fatal(err)
					}
				}}
			}
			result, err := s.executeTool(ctx, p.ID, save.ID)
			if err == nil || result.Text != "" || (scenario == "cancel" && !errors.Is(err, fault.ErrConflict)) {
				t.Fatal("uncommitted save returned adopted receipt", err)
			}
			after, _ := s.Store.Snapshot(ctx, p.ID)
			if !reflect.DeepEqual(before.VisualPrompts, after.VisualPrompts) || !reflect.DeepEqual(before.Assets, after.Assets) || after.LatestRun().Calls[0].Actions[0].Status != "pending" || after.UsedMicroCNY() != before.UsedMicroCNY() {
				t.Fatal("uncommitted save changed content/status/accounting")
			}
			if scenario == "commit" && !reflect.DeepEqual(before, after) {
				t.Fatal("failed commit changed snapshot")
			}
			if scenario == "cancel" && after.LatestRun().Status != "cancelled" {
				t.Fatal("late save revived cancellation")
			}
		})
	}
}
