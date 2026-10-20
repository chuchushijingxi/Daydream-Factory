package modelregistry

import (
	"encoding/json"
	"fmt"
	"net/http"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	d "drama-agent/internal/domain/project"
)

// FreeVisualReply explicitly completes the new visual phase for tests focused
// on catalog behavior. Zero tokens represent this local fixture, not a provider.
// Full pipeline/content tests provide their own model and assertions.
func FreeVisualReply(req ports.Request) (ports.Response, bool) {
	visual := false
	for _, tool := range req.Tools {
		visual = visual || tool.Name == "save_visual_prompts"
	}
	if !visual {
		return ports.Response{}, false
	}
	var input struct {
		Targets []d.VisualTarget `json:"targets"`
	}
	if err := json.Unmarshal([]byte(req.Input), &input); err != nil {
		panic(err)
	}
	name, args := "save_visual_prompts", ""
	inspected := false
	for _, m := range req.Dialogue {
		inspected = inspected || m.Role == "tool" && m.ToolCallID == "fixture-visual-save"
	}
	if inspected {
		name, args = "finish_visual_prompts", "{}"
	} else {
		prompts := []d.VisualPromptInput{}
		for _, target := range input.Targets {
			prompts = append(prompts, d.VisualPromptInput{Target: target, Text: fmt.Sprintf("免费端口夹具正文 %s %s", target.AssetID, target.LookID), DesignNotes: "仅供提取端口测试，不代表模型质量"})
		}
		raw, _ := json.Marshal(map[string]any{"prompts": prompts})
		args = string(raw)
	}
	id := "fixture-visual-save"
	if inspected {
		id = "fixture-visual-finish"
	}
	return ports.Response{Actions: []d.Action{{ID: id, Name: name, Arguments: args, Status: "pending"}}, FinishReason: "tool_calls", Raw: json.RawMessage(`{"freeVisualFixture":true}`), Usage: billing.Usage{Known: true}}, true
}

// FreeVisualWireReply handles only the explicit visual tool contract in local
// OpenAI-wire protocol tests; catalog request assertions remain unchanged.
func FreeVisualWireReply(w http.ResponseWriter, wire any) bool {
	raw, err := json.Marshal(wire)
	if err != nil {
		panic(err)
	}
	var body struct {
		Tools    []struct{ Function struct{ Name string } } `json:"tools"`
		Messages []struct {
			Role, Content string
			ToolID        string `json:"tool_call_id"`
		} `json:"messages"`
	}
	if err = json.Unmarshal(raw, &body); err != nil {
		panic(err)
	}
	req := ports.Request{}
	for _, t := range body.Tools {
		req.Tools = append(req.Tools, ports.ToolDefinition{Name: t.Function.Name})
	}
	for _, m := range body.Messages {
		if m.Role == "user" {
			req.Input = m.Content
		}
		req.Dialogue = append(req.Dialogue, ports.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolID})
	}
	response, ok := FreeVisualReply(req)
	if !ok {
		return false
	}
	calls := []any{}
	for _, a := range response.Actions {
		calls = append(calls, map[string]any{"id": a.ID, "type": "function", "function": map[string]string{"name": a.Name, "arguments": a.Arguments}})
	}
	w.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(w).Encode(map[string]any{"id": "free-visual-wire-fixture", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": calls}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 0, "completion_tokens": 0}}); err != nil {
		panic(err)
	}
	return true
}
