package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/application/ports"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

func geminiProfile(t *testing.T, id string) md.ConfiguredModel {
	t.Helper()
	c, _ := md.OfficialPreset("google")
	for _, m := range c.Models {
		if m.ModelID == id {
			return m
		}
	}
	t.Fatal("missing native Gemini profile")
	return md.ConfiguredModel{}
}

func TestGeminiNativeCallsKeepSignedPartsParallelIDsSchemaAndThinkingUsage(t *testing.T) {
	for _, id := range []string{md.GeminiArgonModelID, md.GeminiFlashModelID, md.GeminiLiteModelID} {
		t.Run(id, func(t *testing.T) {
			calls, observations := 0, 0
			const signedContent = `{"role":"model","parts":[{"text":"reason one","thought":true},{"functionCall":{"id":"native-a","name":"inspect","args":{}},"thoughtSignature":"c2lnLW9uZQ=="},{"text":"interleaved text","thoughtSignature":"dGV4dC1zaWc="},{"functionCall":{"id":"native-b","name":"inspect","args":{}},"thoughtSignature":"c2lnLXR3bw=="}]}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if observations != calls || r.Method != "POST" || r.URL.Path != "/native/v1beta/models/"+id+":generateContent" || r.URL.RawQuery != "" || r.Header.Get("x-goog-api-key") != "fictional-gemini-key" {
					t.Error("wrong native route/auth or missing durable observation", r.URL)
				}
				var body struct {
					Contents   []json.RawMessage `json:"contents"`
					Generation struct {
						Max      int `json:"maxOutputTokens"`
						Thinking struct {
							Level string `json:"thinkingLevel"`
						} `json:"thinkingConfig"`
						Modalities []string `json:"responseModalities"`
					} `json:"generationConfig"`
					Tools []struct {
						Declarations []struct {
							Parameters map[string]any `json:"parametersJsonSchema"`
						} `json:"functionDeclarations"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Generation.Max != 64 || body.Generation.Thinking.Level != "HIGH" || !reflect.DeepEqual(body.Generation.Modalities, []string{"TEXT"}) || len(body.Tools) != 1 || len(body.Tools[0].Declarations) != 1 || body.Tools[0].Declarations[0].Parameters["additionalProperties"] != false {
					t.Error("native schema, text modality or total output budget lost", body)
				}
				w.Header().Set("Content-Type", "application/json")
				if calls == 1 {
					_, _ = w.Write([]byte(`{"candidates":[{"content":` + signedContent + `,"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":12,"thoughtsTokenCount":8,"cachedContentTokenCount":40,"totalTokenCount":120}}`))
					return
				}
				if len(body.Contents) != 3 {
					t.Fatal("parallel tool results not kept", len(body.Contents))
				}
				var actual, expected any
				_ = json.Unmarshal(body.Contents[1], &actual)
				_ = json.Unmarshal([]byte(signedContent), &expected)
				if !reflect.DeepEqual(actual, expected) {
					t.Error("native signed parts were reordered, dropped or reconstructed", string(body.Contents[1]))
				}
				var receipts struct {
					Parts []struct {
						Response struct{ ID, Name string } `json:"functionResponse"`
					} `json:"parts"`
				}
				_ = json.Unmarshal(body.Contents[2], &receipts)
				if len(receipts.Parts) != 2 || receipts.Parts[0].Response.Name != "inspect" || receipts.Parts[0].Response.ID != "native-a" || receipts.Parts[1].Response.ID != "native-b" {
					t.Error("same-name parallel results lost upstream identity", receipts)
				}
				_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"done","thoughtSignature":"ZmluYWwtc2ln"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":110,"candidatesTokenCount":3,"thoughtsTokenCount":2,"totalTokenCount":115}}`))
			}))
			defer server.Close()
			profile := geminiProfile(t, id)
			profile.Alias = "Alias never routes requests"
			m, err := modeladapter.NewConfigured(context.Background(), "fictional-gemini-key", server.URL+"/native/v1beta", md.GoogleProtocol, profile)
			if err != nil || calls != 0 {
				t.Fatal(err, calls)
			}
			req := ports.Request{System: "test system", Input: "test input", MaxOutputTokens: 64, Tools: []ports.ToolDefinition{{Name: "inspect", Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}}
			observe := func(raw json.RawMessage) error {
				observations++
				if !json.Valid(raw) || strings.Contains(string(raw), "fictional-gemini-key") {
					t.Error("request record invalid or leaked key")
				}
				return nil
			}
			out, err := m.Generate(context.Background(), req, observe)
			if err != nil || !out.Usage.Known || out.Usage.Input != 100 || out.Usage.Output != 20 || out.Usage.CacheHit != 40 || out.FinishReason != "tool_calls" || len(out.Actions) != 2 || out.Actions[0].ID != "native-a" || out.Actions[1].ID != "native-b" || out.Reasoning != "reason one" || out.ProviderState != signedContent {
				t.Fatal("invalid native response/accounting", out, err)
			}
			// A JSON round-trip models persistence of the opaque state as a string.
			raw, _ := json.Marshal(ports.Message{Role: "assistant", Content: out.Text, Reasoning: out.Reasoning, Actions: out.Actions, ProviderState: out.ProviderState})
			var saved ports.Message
			_ = json.Unmarshal(raw, &saved)
			req.Dialogue = []ports.Message{saved, {Role: "tool", ToolCallID: "native-a", Content: `{"result":"one"}`}, {Role: "tool", ToolCallID: "native-b", Content: `{"result":"two"}`}}
			out, err = m.Generate(context.Background(), req, observe)
			if err != nil || calls != 2 || !out.Usage.Known || out.Usage.Output != 5 || out.FinishReason != "stop" || out.Text != "done" || !strings.Contains(out.ProviderState, "ZmluYWwtc2ln") {
				t.Fatal(out, err, calls)
			}
		})
	}
}

func TestGeminiNativeObserverStopsSendingAndNoSDKRejectionRetries(t *testing.T) {
	for _, status := range []int{401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"code":500,"message":"fictional-key rejected","status":"INTERNAL"}}`))
			}))
			defer server.Close()
			m, err := modeladapter.NewGemini(context.Background(), "fictional-key", server.URL+"/v1beta", geminiProfile(t, md.GeminiFlashModelID))
			if err != nil {
				t.Fatal(err)
			}
			req := ports.Request{Input: "fixture", MaxOutputTokens: 16}
			out, err := m.Generate(context.Background(), req, func(json.RawMessage) error { return nil })
			if err == nil || calls != 1 || strings.Contains(err.Error(), "fictional-key") || strings.Contains(string(out.Raw), "fictional-key") {
				t.Fatal("rejection retried or key leaked", calls, err)
			}
			_, err = m.Generate(context.Background(), req, func(json.RawMessage) error { return errors.New("record unavailable") })
			var unsent interface{ RequestNotSent() bool }
			if err == nil || calls != 1 || !errors.As(err, &unsent) || !unsent.RequestNotSent() {
				t.Fatal("observer failure sent a request", err, calls)
			}
			req.Dialogue = []ports.Message{{Role: "assistant", Actions: []d.Action{{ID: "a", Name: "inspect", Arguments: "{}"}}}, {Role: "tool", ToolCallID: "a", Content: "{}"}}
			_, err = m.Generate(context.Background(), req, func(json.RawMessage) error { return nil })
			if err == nil || calls != 1 || !errors.As(err, &unsent) {
				t.Fatal("lost signed history was sent", err, calls)
			}
		})
	}
}

func TestGeminiUsageNeverTreatsMissingOrContradictoryMetadataAsFree(t *testing.T) {
	for index, metadata := range []string{``, `,"usageMetadata":{"promptTokenCount":100}`, `,"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":12,"thoughtsTokenCount":8,"totalTokenCount":101}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"fixture"}]},"finishReason":"STOP"}]` + metadata + `}`))
		}))
		m, err := modeladapter.NewGemini(context.Background(), "fictional-key", server.URL+"/v1beta", geminiProfile(t, md.GeminiLiteModelID))
		if err != nil {
			t.Fatal(err)
		}
		out, err := m.Generate(context.Background(), ports.Request{Input: "fixture", MaxOutputTokens: 16}, func(json.RawMessage) error { return nil })
		server.Close()
		if out.Usage.Known || (index < 2 && err != nil) || (index == 2 && (err == nil || !strings.Contains(err.Error(), "MODEL_USAGE_INVALID"))) {
			t.Fatal("missing/contradictory usage became known zero", out, err)
		}
	}
}

func TestGeminiReceiptRequiresNativeIdentityOrderAndCompleteJSON(t *testing.T) {
	m := &modeladapter.Gemini{}
	const receipt = `{"ok":true,"revision":9007199254740993,"changes":[{"id":"p1","text":"完整原文😀"}]}`
	a := d.Action{ID: "native-b", Name: "save_visual_prompts", Result: receipt}
	request := func(first, second string) json.RawMessage {
		return json.RawMessage(`{"contents":[{"role":"model","parts":[{"functionCall":{"id":"native-a","name":"read_assets"}},{"functionCall":{"id":"native-b","name":"save_visual_prompts"}}]},{"role":"user","parts":[` + first + `,` + second + `]}]}`)
	}
	const first = `{"functionResponse":{"id":"native-a","name":"read_assets","response":{"assets":[]}}}`
	second := `{"functionResponse":{"id":"native-b","name":"save_visual_prompts","response":` + receipt + `}}`
	for name, raw := range map[string]json.RawMessage{
		"complete":  request(first, second),
		"key order": request(first, `{"functionResponse":{"name":"save_visual_prompts","response":{"changes":[{"text":"完整原文😀","id":"p1"}],"revision":9007199254740993,"ok":true},"id":"native-b"}}`),
	} {
		if !m.ContainsToolReceipt(raw, a, d.Call{}) {
			t.Fatal(name, "actual native receipt was not observed")
		}
	}
	for name, raw := range map[string]json.RawMessage{
		"wrong id":          request(first, strings.ReplaceAll(second, "native-b", "foreign")),
		"wrong name":        request(first, strings.ReplaceAll(second, "save_visual_prompts", "read_assets")),
		"wrong order":       request(second, first),
		"missing field":     request(first, strings.ReplaceAll(second, `"ok":true,`, "")),
		"changed number":    request(first, strings.ReplaceAll(second, "9007199254740993", "9007199254740992")),
		"changed content":   request(first, strings.ReplaceAll(second, "完整原文😀", "局部原文")),
		"unpaired response": json.RawMessage(`{"contents":[{"role":"user","parts":[` + second + `]}]}`),
		"invalid JSON":      json.RawMessage(`{"contents":`),
	} {
		if m.ContainsToolReceipt(raw, a, d.Call{}) {
			t.Fatal(name, "accepted an unproven or incomplete receipt")
		}
	}
}

func TestGeminiReceiptWithoutNativeIDUsesSavedSignedContentAndActionPosition(t *testing.T) {
	m := &modeladapter.Gemini{}
	state := `{"role":"model","parts":[{"thoughtSignature":"c2ln","functionCall":{"name":"save_visual_prompts","args":{"slot":1}}},{"functionCall":{"name":"save_visual_prompts","args":{"slot":2}}}]}`
	a := d.Action{ID: "internal-second", Name: "save_visual_prompts", Result: `{"ok":true,"saved":2}`}
	origin := d.Call{ProviderState: state, Actions: []d.Action{{ID: "internal-first", Name: a.Name}, a}}
	raw := json.RawMessage(`{"contents":[` + state + `,{"role":"user","parts":[{"functionResponse":{"name":"save_visual_prompts","response":{"ok":true,"saved":1}}},{"functionResponse":{"name":"save_visual_prompts","response":{"ok":true,"saved":2}}}]}]}`)
	if !m.ContainsToolReceipt(raw, a, origin) {
		t.Fatal("saved native order not observed")
	}
	if m.ContainsToolReceipt(raw, a, d.Call{}) {
		t.Fatal("name-only association accepted")
	}
	changed := origin
	changed.ProviderState = strings.Replace(state, "c2ln", "changed-signature", 1)
	if m.ContainsToolReceipt(raw, a, changed) {
		t.Fatal("different native content accepted")
	}
	changed = origin
	changed.Actions = []d.Action{a, {ID: "internal-first", Name: a.Name}}
	if m.ContainsToolReceipt(raw, a, changed) {
		t.Fatal("wrong saved action order accepted")
	}
	changed = origin
	changed.Actions = []d.Action{a}
	if m.ContainsToolReceipt(raw, a, changed) {
		t.Fatal("incomplete saved actions accepted")
	}
}
