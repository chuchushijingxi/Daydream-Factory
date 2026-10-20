package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/application/ports"
	md "drama-agent/internal/domain/modelconfig"
)

func arkProfile(t *testing.T) md.ConfiguredModel {
	t.Helper()
	c, ok := md.OfficialPreset("volcengine")
	if !ok {
		t.Fatal("Ark preset missing")
	}
	return c.Models[0]
}

func TestVolcengineNativeTransportPreservesToolsUsageAndBudget(t *testing.T) {
	for _, id := range []string{md.DoubaoEvolvingModelID, md.DoubaoProModelID, md.DoubaoTurboModelID, md.DoubaoLiteModelID} {
		t.Run(id, func(t *testing.T) {
			count, observed := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				if observed != 1 || r.URL.Path != "/api/v3/chat/completions" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fictional-ark-key" {
					t.Error("unexpected Ark transport or missing durable observation")
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				var modelID string
				_ = json.Unmarshal(body["model"], &modelID)
				var limit int
				_ = json.Unmarshal(body["max_completion_tokens"], &limit)
				if modelID != id || limit != 64 || body["max_tokens"] != nil || string(body["thinking"]) != `{"type":"enabled"}` {
					t.Error("incorrect provider ID, thinking or output budget", string(body["model"]), string(body["max_completion_tokens"]), string(body["thinking"]))
				}
				var tools []struct {
					Function struct {
						Parameters map[string]any `json:"parameters"`
					} `json:"function"`
				}
				_ = json.Unmarshal(body["tools"], &tools)
				if len(tools) != 1 || tools[0].Function.Parameters["additionalProperties"] != false {
					t.Error("tool schema changed")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"mock","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"mock reasoning","tool_calls":[{"id":"call-mock","type":"function","function":{"name":"inspect","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":12,"prompt_tokens_details":{"cached_tokens":40}}}`))
			}))
			defer server.Close()
			profile := arkProfile(t)
			profile.ModelID = id
			profile.Alias = "Alias must never route requests"
			m, err := modeladapter.NewConfigured(context.Background(), "fictional-ark-key", server.URL+"/api/v3", md.VolcengineProtocol, profile)
			if err != nil || count != 0 {
				t.Fatal(err, count)
			}
			resp, err := m.Generate(context.Background(), ports.Request{System: "mock system", Input: "mock material", MaxOutputTokens: 64, Tools: []ports.ToolDefinition{{Name: "inspect", Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}}, func(raw json.RawMessage) error {
				observed++
				if !json.Valid(raw) {
					t.Error("invalid observed request")
				}
				return nil
			})
			if err != nil || count != 1 || !resp.Usage.Known || resp.Usage.CacheHit != 40 || resp.Usage.Input != 100 || resp.Usage.Output != 12 || len(resp.Actions) != 1 || resp.Actions[0].Name != "inspect" || resp.Reasoning != "mock reasoning" || !json.Valid(resp.Raw) {
				t.Fatal(resp, err, count)
			}
		})
	}
}

func TestVolcengineDoesNotRetryPaidHTTPRejectionsAndObserverStopsSending(t *testing.T) {
	for _, status := range []int{401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"mock rejection","type":"mock","code":"mock"}}`))
			}))
			defer server.Close()
			m, err := modeladapter.NewVolcengine(context.Background(), "fictional-ark-key", server.URL+"/api/v3", arkProfile(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Generate(context.Background(), ports.Request{Input: "mock", MaxOutputTokens: 16}, func(json.RawMessage) error { return nil })
			if err == nil || calls != 1 {
				t.Fatal("SDK retried rejection", calls, err)
			}
			_, err = m.Generate(context.Background(), ports.Request{Input: "mock", MaxOutputTokens: 16}, func(json.RawMessage) error { return errors.New("durable observation unavailable") })
			var unsent interface{ RequestNotSent() bool }
			if err == nil || calls != 1 || !errors.As(err, &unsent) || !unsent.RequestNotSent() {
				t.Fatal("observer failure sent request or lost classification", calls, err)
			}
		})
	}
	if _, err := modeladapter.NewConfigured(context.Background(), "mock", md.VolcengineBaseURL, "zhipu", arkProfile(t)); err == nil {
		t.Fatal("unsupported Zhipu protocol accepted")
	}
}
