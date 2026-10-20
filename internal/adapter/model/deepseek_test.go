package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/billing"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

func TestConfiguredEndpointDoesNotFollowRedirects(t *testing.T) {
	redirected := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected++; w.WriteHeader(401) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	m, err := modeladapter.New(context.Background(), "dummy-redirect-key", origin.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Generate(context.Background(), ports.Request{System: "虚构测试", Input: "虚构材料", MaxOutputTokens: 32}, func(json.RawMessage) error { return nil })
	if err == nil || redirected != 0 {
		t.Fatal("redirect created a second unrecorded request", redirected, err)
	}
}

func TestActualProviderRequestMatchesParserAndAdoption(t *testing.T) {
	var observed []byte
	received := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		var body struct {
			Model          string                           `json:"model"`
			Messages       []struct{ Role, Content string } `json:"messages"`
			Tools          json.RawMessage                  `json:"tools"`
			MaxTokens      int                              `json:"max_tokens"`
			ResponseFormat struct{ Type string }            `json:"response_format"`
			Thinking       struct{ Type string }            `json:"thinking"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "deepseek-flash" || len(body.Messages) != 2 || body.MaxTokens != 393216 || body.ResponseFormat.Type != "" || body.Thinking.Type != "enabled" || !strings.Contains(string(body.Tools), "save_catalog") || !strings.Contains(string(body.Tools), "sourceIds") {
			t.Errorf("actual request differs from the shared schema: %+v", body)
		}
		var tools []struct {
			Function struct {
				Name, Description string
				Parameters        any
			}
		}
		if err := json.Unmarshal(body.Tools, &tools); err != nil || len(tools) != len(app.Tools()) {
			t.Fatal("actual tool list changed", err)
		}
		for i, definition := range app.Tools() {
			var expected any
			if err := json.Unmarshal(definition.Parameters, &expected); err != nil {
				t.Fatal(err)
			}
			if tools[i].Function.Name != definition.Name || tools[i].Function.Description != definition.Description || !reflect.DeepEqual(tools[i].Function.Parameters, expected) {
				t.Errorf("provider schema differs from execution schema: actual=%+v expected=%+v description=%q expectedDesc=%q", tools[i].Function.Parameters, expected, tools[i].Function.Description, definition.Description)
			}
		}
		text := `{"assets":[{"kind":"prop","name":"不同实物的铜钥匙","aliases":null,"description":null,"sourceIds":["s1","s2"]}]}`
		json.NewEncoder(w).Encode(map[string]any{"id": "wire", "model": "deepseek-flash", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "save", "type": "function", "function": map[string]string{"name": "save_catalog", "arguments": text}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150, "prompt_cache_hit_tokens": 80, "prompt_cache_miss_tokens": 20}})
	}))
	defer server.Close()
	m, err := modeladapter.New(context.Background(), "only-local-secret", server.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.Generate(context.Background(), ports.Request{System: app.ExtractPrompt, Input: `{"source":[]}`, MaxOutputTokens: 393216, Tools: app.Tools()}, func(raw json.RawMessage) error { observed = append([]byte(nil), raw...); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if received != 1 || len(observed) == 0 || strings.Contains(string(observed), "only-local-secret") {
		t.Fatal("request not recorded safely before sending")
	}
	if r.Usage.CacheHit != 80 || !r.Usage.Known || r.Usage.Input != 100 || r.FinishReason != "tool_calls" || len(r.Actions) != 1 {
		t.Fatalf("usage mapping changed: %+v", r)
	}
	draft, err := app.Decode(r.Actions[0].Arguments)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := d.AdoptCatalogChanges(draft.DomainAssets(), draft.RemoveIDs, nil, d.ScriptSource{Spans: []d.SourceSpan{{ID: "s1"}, {ID: "s2"}}})
	if err != nil || len(assets) != 1 || assets[0].Name != "不同实物的铜钥匙" {
		t.Fatal("legal actual output rejected", err)
	}
}

func TestMissingUsageIsUnknownAndActualMalformedResponseIsRetained(t *testing.T) {
	for _, body := range []string{`{"choices":[{"message":{"role":"assistant","content":"{\"assets\":[]}"},"finish_reason":"stop"}]}`, `{malformed response`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		m, err := modeladapter.New(context.Background(), "local-secret", server.URL, md.DeepSeekModelID)
		if err != nil {
			t.Fatal(err)
		}
		result, err := m.Generate(context.Background(), ports.Request{System: "Return JSON", Input: "{}", MaxOutputTokens: 100}, func(json.RawMessage) error { return nil })
		server.Close()
		if result.Usage.Known || string(result.Raw) != body {
			t.Fatalf("billing fabricated or response lost: known=%v raw=%q err=%v", result.Usage.Known, result.Raw, err)
		}
	}
}

func TestActualResponseInterruptionIsTypedAndPartialToolsAreDiscarded(t *testing.T) {
	for _, scenario := range []string{"tcp-cut", "malformed-json", "http-401", "http-401-cut", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			partial := `{"choices":[{"message":{"role":"assistant","tool_calls":[`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch scenario {
				case "http-401":
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"error":{"message":"invalid credentials"}}`))
				case "malformed-json":
					w.Write([]byte("{malformed response"))
				default:
					w.Header().Set("Content-Length", "10000")
					if scenario == "http-401-cut" {
						w.WriteHeader(http.StatusUnauthorized)
					}
					w.Write([]byte(partial))
					w.(http.Flusher).Flush()
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			model, err := modeladapter.New(context.Background(), "local-only", server.URL, md.DeepSeekModelID)
			if err != nil {
				t.Fatal(err)
			}
			result, err := model.Generate(ctx, ports.Request{System: "test", Input: "{}", MaxOutputTokens: 32, Tools: app.Tools()}, func(json.RawMessage) error { return nil })
			var interrupted interface{ ResponseInterrupted() bool }
			marked := errors.As(err, &interrupted) && interrupted.ResponseInterrupted()
			if err == nil || marked != (scenario == "tcp-cut") || len(result.Actions) != 0 || result.Usage.Known {
				t.Fatalf("incorrect failure classification: marked=%v err=%v response=%+v", marked, err, result)
			}
			if scenario == "tcp-cut" && string(result.Raw) != partial {
				t.Fatal("partial wire evidence lost")
			}
		})
	}
}

func TestOpenAICompatibleConnectionUsesUpstreamIDAndNoDeepSeekFields(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fictional-key" {
			t.Error("wrong destination or credential")
		}
		var body map[string]json.RawMessage
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		var id string
		_ = json.Unmarshal(body["model"], &id)
		if id != "custom-upstream" || body["thinking"] != nil {
			t.Error("alias leaked or DeepSeek-only parameter sent", string(body["model"]), string(body["thinking"]))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"fictional-response","model":"custom-upstream","choices":[{"message":{"role":"assistant","content":"虚构回复"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":10}}`))
	}))
	defer server.Close()
	price := billing.TokenPricing{Input: .25, Output: .5}
	m, e := modeladapter.NewConfigured(context.Background(), "fictional-key", server.URL+"/v1", "openai-chat", md.ConfiguredModel{ModelID: "custom-upstream", Alias: "用户别名", Category: "analysis", ContextWindow: 8192, MaxOutputTokens: 1024, Pricing: &price})
	if e != nil {
		t.Fatal(e)
	}
	window, output := m.Capacity()
	if window != 8192 || output != 1024 || count != 0 {
		t.Fatal("constructor called supplier or used fixed capacity")
	}
	resp, e := m.Generate(context.Background(), ports.Request{System: "虚构系统", Input: "虚构测试材料", MaxOutputTokens: 32}, func(json.RawMessage) error { return nil })
	if e != nil || resp.Text != "虚构回复" || count != 1 || !resp.Usage.Known {
		t.Fatal(resp, e, count)
	}
}

func TestCompatibleConnectionRequiresExplicitEndpoint(t *testing.T) {
	price := billing.TokenPricing{Input: .25, Output: .5}
	profile := md.ConfiguredModel{ModelID: "configured-model", Category: "analysis", ContextWindow: 8192, MaxOutputTokens: 1024, Pricing: &price}
	for _, protocol := range []string{"deepseek", "openai-chat"} {
		if _, err := modeladapter.NewConfigured(context.Background(), "fictional-key", "", protocol, profile); err == nil {
			t.Fatalf("%s silently used an SDK default supplier endpoint", protocol)
		}
	}
}

func TestDeepSeekRetainsReasoningAndToolHistoryOnTheWire(t *testing.T) {
	var received, observed []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		received, err = io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var body struct {
			Messages []struct {
				Role, Content string
				Reasoning     string                `json:"reasoning_content"`
				ToolCallID    string                `json:"tool_call_id"`
				ToolCalls     []struct{ ID string } `json:"tool_calls"`
			}
		}
		if err := json.Unmarshal(received, &body); err != nil {
			t.Error(err)
		}
		if len(body.Messages) != 4 {
			t.Errorf("tool conversation lost: %s", received)
		} else {
			assistant, tool := body.Messages[2], body.Messages[3]
			if assistant.Role != "assistant" || assistant.Reasoning != "上一轮思考" || len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call-1" || tool.Role != "tool" || tool.ToolCallID != "call-1" || tool.Content != `{"saved":true}` {
				t.Errorf("tool or reasoning history changed: %s", received)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"继续处理","reasoning_content":"本轮思考","tool_calls":[{"id":"call-2","type":"function","function":{"name":"finish","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"prompt_cache_hit_tokens":4}}`))
	}))
	defer server.Close()
	m, err := modeladapter.New(context.Background(), "fictional-key", server.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Generate(context.Background(), ports.Request{
		System: "虚构系统", Input: "虚构材料", MaxOutputTokens: 32,
		Dialogue: []ports.Message{
			{Role: "assistant", Reasoning: "上一轮思考", Actions: []d.Action{{ID: "call-1", Name: "save", Arguments: `{}`}}},
			{Role: "tool", ToolCallID: "call-1", Content: `{"saved":true}`},
		},
	}, func(raw json.RawMessage) error { observed = append([]byte(nil), raw...); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if string(observed) != string(received) || len(observed) == 0 {
		t.Fatal("saved request differs from the actual supplier request")
	}
	if result.Reasoning != "本轮思考" || result.Text != "继续处理" || result.FinishReason != "tool_calls" || len(result.Actions) != 1 || result.Actions[0].ID != "call-2" || !result.Usage.Known || result.Usage.Input != 20 || result.Usage.Output != 10 || result.Usage.CacheHit != 4 {
		t.Fatalf("reasoning, action, or usage mapping changed: %+v", result)
	}
}
