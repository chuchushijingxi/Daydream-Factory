package project

import (
	"encoding/json"
	"strings"
	"testing"

	d "drama-agent/internal/domain/project"
)

func TestModelShotDurationRoundingPreservesOtherFieldsAndPatchScope(t *testing.T) {
	operations := []d.StoryboardOperation{
		{Op: "createShot", Value: json.RawMessage(`{"durationSeconds":5.5,"action":"停顿 5.5 秒的原文不改写","camera":{"size":"近景"}}`)},
		{Op: "updateShot", Patch: json.RawMessage(`{"durationSeconds":0.01,"designNotes":"完整保留"}`)},
		{Op: "updateShot", Patch: json.RawMessage(`{"durationSeconds":6}`)},
		{Op: "updateShot", Patch: json.RawMessage(`{"videoPrompt":null}`)},
		{Op: "createShot", Value: json.RawMessage(`{"durationSeconds":null}`)},
	}
	if err := roundModelShotDurations(operations); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{`"durationSeconds":6`, `"durationSeconds":1`, `"durationSeconds":6`, `{"videoPrompt":null}`, `{"durationSeconds":null}`} {
		raw := operations[i].Value
		if operations[i].Op == "updateShot" {
			raw = operations[i].Patch
		}
		if !strings.Contains(string(raw), want) {
			t.Fatal("wrong adopted integer or expanded patch", i, string(raw))
		}
	}
	if !strings.Contains(string(operations[0].Value), "停顿 5.5 秒的原文不改写") || !strings.Contains(string(operations[0].Value), `"camera":{"size":"近景"}`) {
		t.Fatal("normalization changed content")
	}
	base := StoryboardSchema("SaveStoryboard")
	compact := storyboardSaveSchema("SaveStoryboard", nil)
	var tool struct {
		Properties struct {
			Operations struct {
				Items struct {
					OneOf []struct {
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"oneOf"`
				} `json:"items"`
			} `json:"operations"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(compact, &tool); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, branch := range tool.Properties.Operations.Items.OneOf {
		for _, field := range []string{"value", "patch"} {
			var content struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if raw := branch.Properties[field]; raw != nil {
				if err := json.Unmarshal(raw, &content); err != nil {
					t.Fatal(err)
				}
				if duration := content.Properties["durationSeconds"]; duration != nil {
					if !strings.Contains(string(duration), `"type":"integer"`) || strings.Contains(string(duration), `"type":"number"`) {
						t.Fatal("model shot value and patch must both request integer seconds", string(duration))
					}
					checked++
				}
			}
		}
	}
	if checked != 2 {
		t.Fatal("createShot and updateShot schemas not both inspected", checked)
	}
	if len(compact) >= len(base) || strings.Contains(string(compact), `"description":`) || !strings.Contains(string(StoryboardSchema("SaveStoryboard")), `"description":`) {
		t.Fatal("model schema must remove duplicate prose without mutating the shared contract")
	}
	t.Logf("save_storyboard schema UTF-8 bytes: %d -> %d", len(base), len(compact))
}

func TestStoryboardRejectedArgumentsIdentifyActualFieldAndSyntax(t *testing.T) {
	var decoded any
	for _, tc := range []struct{ raw, want string }{
		{`{"operations":[{"op":"updateShot","shotId":"shot","patch":{"videoPrompt":{"parts":[{"type":"asset"}]}}}]}`, "operations: [0]: patch: videoPrompt: parts: [0]: missing assetId"},
		{`{"arguments":"{}"}`, "不包装 arguments"},
	} {
		err := DecodeStoryboard([]byte(tc.raw), "SaveStoryboard", &decoded)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatal("rejection lacks actionable field path", err)
		}
	}
	err := storyboardArgumentError(`{"operations":[{"op":"createShot", {]}`, nil)
	if err == nil || !strings.Contains(err.Error(), "JSON byte") || !strings.Contains(err.Error(), "near") {
		t.Fatal("malformed JSON lacks its exact location", err)
	}
}
