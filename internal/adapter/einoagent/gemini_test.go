package einoagent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"drama-agent/internal/adapter/einoagent"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/application/ports"
	md "drama-agent/internal/domain/modelconfig"
)

func TestEinoRunnerKeepsNativeSignedConversationWithoutAnotherPlanningLoop(t *testing.T) {
	calls, executed := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if calls == 2 && (!strings.Contains(string(body), "c2lnLTE=") || !strings.Contains(string(body), `"functionResponse"`) || !strings.Contains(string(body), `"id":"native-one"`)) {
			t.Error("Runner lost the exact signed state/tool receipt", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"native-one","name":"inspect","args":{}},"thoughtSignature":"c2lnLTE="}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":3,"thoughtsTokenCount":2,"totalTokenCount":105}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"native-two","name":"finish","args":{}},"thoughtSignature":"c2lnLTI="}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":120,"candidatesTokenCount":3,"thoughtsTokenCount":2,"totalTokenCount":125}}`))
	}))
	defer server.Close()
	preset, _ := md.OfficialPreset("google")
	m, err := modeladapter.NewGemini(context.Background(), "fictional-eino-key", server.URL+"/v1beta", preset.Models[5])
	if err != nil {
		t.Fatal(err)
	}
	tools := []ports.ToolDefinition{{Name: "inspect", Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}, {Name: "finish", Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}
	states := []string{}
	session := ports.AgentSession{Instruction: "local fixture", Tools: tools, Generate: func(ctx context.Context, history []ports.Message) (ports.Response, error) {
		if calls > 0 && (len(history) != 2 || history[0].ProviderState != states[0] || history[1].ToolCallID != "native-one") {
			t.Fatal("framework history differs from the recorded call", history)
		}
		response, err := m.Generate(ctx, ports.Request{System: "local fixture", Input: "fictional task", MaxOutputTokens: 64, Tools: tools, Dialogue: history}, func(json.RawMessage) error { return nil })
		states = append(states, response.ProviderState)
		return response, err
	}, Execute: func(ctx context.Context, id string) (ports.ToolResult, error) {
		executed++
		if id != "native-one" && id != "native-two" {
			t.Fatal("tool ID changed", id)
		}
		return ports.ToolResult{Text: `{"saved":true}`, Finished: id == "native-two"}, nil
	}}
	if err = einoagent.New().Run(context.Background(), session); err != nil || calls != 2 || executed != 2 {
		t.Fatal(err, calls, executed)
	}
}
