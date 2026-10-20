package project

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func TestTaskDialogueKeepsCompletePairsAndRejectsMissingOrChangedObservation(t *testing.T) {
	p := d.Project{Runs: []d.Run{{ID: "old", Calls: []d.Call{{Text: "another Run"}}}, {ID: "current", Contract: d.CatalogExtractionContract, Stage: "correct", Cursor: 2, End: 3, Calls: []d.Call{
		{Stage: "extract", Start: 2, End: 3, Text: "completed stage"},
		{Stage: "correct", Start: 0, End: 2, Text: "completed batch"},
		{Stage: "correct", Start: 2, End: 3, Text: "current", Reasoning: "current reasoning", FinishReason: "tool_calls", Actions: []d.Action{{ID: "one", Name: "save_catalog", Arguments: `{"assets":[]}`, Status: "rejected", Result: `{"ok":false,"error":"invalid reference"}`}, {ID: "two", Name: "finish_batch", Arguments: "{}", Status: "rejected", Result: `{"ok":false}`}}},
	}}}}
	expected, err := taskDialogue(p)
	if err != nil || len(expected) != 3 || expected[0].Reasoning != "current reasoning" || expected[1].ToolCallID != "one" || expected[2].ToolCallID != "two" {
		t.Fatal("incomplete pair or old task included", err, expected)
	}
	incoming := append([]ports.Message{{Role: "assistant", Content: "completed", Actions: []d.Action{{ID: "old"}}}, {Role: "tool", ToolCallID: "old", Content: "old receipt"}}, expected...)
	if got, err := selectTaskDialogue(p, incoming); err != nil || len(got) != 3 {
		t.Fatal("current Eino dialogue not forwarded", err)
	}
	if _, err := selectTaskDialogue(p, expected[:2]); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("orphaned tool group accepted")
	}
	changed := append([]ports.Message{}, expected...)
	changed[1].Content = `{"ok":true}`
	if _, err := selectTaskDialogue(p, changed); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("invented tool receipt accepted")
	}
}

func TestSignedProviderStateSurvivesStoredDialogueAndCannotBeInvented(t *testing.T) {
	state := `{"role":"model", "parts":[{"functionCall":{"name":"save_catalog","args":{}},"thoughtSignature":"c2ln"}]}`
	p := d.Project{Runs: []d.Run{{ID: "native", Contract: d.CatalogExtractionContract, Stage: "extract", End: 1, Calls: []d.Call{{Stage: "extract", End: 1, FinishReason: "tool_calls", ProviderState: state, Actions: []d.Action{{ID: "signed", Name: "save_catalog", Arguments: "{}", Status: "applied", Result: `{"ok":true}`}}}}}}}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var reopened d.Project
	if err = json.Unmarshal(raw, &reopened); err != nil {
		t.Fatal(err)
	}
	dialogue, err := taskDialogue(reopened)
	if err != nil || dialogue[0].ProviderState != state {
		t.Fatal("stored signature was rewritten or lost", err)
	}
	if _, err = selectTaskDialogue(reopened, dialogue); err != nil {
		t.Fatal(err)
	}
	dialogue[0].ProviderState = `{"role":"model","parts":[]}`
	if _, err = selectTaskDialogue(reopened, dialogue); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("altered signed history accepted", err)
	}
}

func TestHistoryConsumesCapacityIncludingReasoningArgumentsAndReceipts(t *testing.T) {
	m := &fakeModel{}
	s, p := fixture(t, m)
	req, _, err := s.request(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	m.window = inputSize(req) + 2000
	m.output = 1000
	p.LatestRun().End = 1
	p.LatestRun().Calls = []d.Call{{Stage: p.LatestRun().Stage, End: 1, FinishReason: "tool_calls", Reasoning: strings.Repeat("思考", 1500), Actions: []d.Action{{ID: "history", Name: "save_catalog", Arguments: `{"assets":[]}`, Status: "applied", Result: `{"ok":true}`}}}}
	if _, _, err := s.request(context.Background(), p); !errors.Is(err, fault.ErrCapacity) {
		t.Fatal("reasoning/history was silently dropped to fit", err)
	}
}
