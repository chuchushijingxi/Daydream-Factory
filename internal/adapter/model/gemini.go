package model

import (
	"bytes"
	"context"
	"drama-agent/internal/adapter/appconfig"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"drama-agent/internal/adapter/transport"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"

	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/genai"
)

type Gemini struct{ *chatModel }

func NewGemini(ctx context.Context, key, endpoint string, profile md.ConfiguredModel, settings ...appconfig.Models) (*Gemini, error) {
	if key == "" || profile.ModelID == "" || !profile.Executable() {
		return nil, fmt.Errorf("model configuration is incomplete")
	}
	client := transport.NewClient(settings...)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Transport = &geminiTransport{base: client.Transport}
	// 单次 SDK 请求不启用上传重试或自动函数执行；迭代由 Eino Runner 管理。
	native, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: key, Backend: genai.BackendGeminiAPI, HTTPClient: client,
		HTTPOptions: genai.HTTPOptions{BaseURL: strings.TrimSuffix(strings.TrimRight(endpoint, "/"), "/v1beta"), APIVersion: "v1beta"},
	})
	if err != nil {
		return nil, fmt.Errorf("model configuration failed")
	}
	component, err := gemini.NewChatModel(ctx, &gemini.Config{
		Client: native, Model: profile.ModelID, ResponseModalities: []gemini.GeminiResponseModality{gemini.GeminiResponseModalityText},
		ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh, IncludeThoughts: true},
	})
	if err != nil {
		return nil, fmt.Errorf("model configuration failed")
	}
	return &Gemini{&chatModel{component: component, key: key, window: profile.ContextWindow, output: profile.MaxOutputTokens,
		outputOption: model.WithMaxTokens, prepareMessages: prepareGeminiMessages, decodeResponse: decodeGeminiResponse}}, nil
}

type nativeFunctionCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type nativeGeminiContent struct {
	Role  string `json:"role"`
	Parts []struct {
		Text         string              `json:"text"`
		Thought      bool                `json:"thought"`
		FunctionCall *nativeFunctionCall `json:"functionCall"`
	} `json:"parts"`
}

type geminiCallRef struct{ name, id string }

// 不从可见思考或工具参数重建签名内容；PG 与 Eino 间保留全部原生 part。
// 文本、并行函数调用和签名按原始顺序恢复，保证供应商原生协议连续性。
func geminiHistoryRefs(history []ports.Message) (map[string]geminiCallRef, error) {
	refs := map[string]geminiCallRef{}
	for _, entry := range history {
		if entry.Role != "assistant" {
			continue
		}
		var content nativeGeminiContent
		if json.Unmarshal([]byte(entry.ProviderState), &content) != nil || content.Role != "model" || len(content.Parts) == 0 {
			return nil, fmt.Errorf("Gemini conversation is missing its native provider state")
		}
		index := 0
		for _, part := range content.Parts {
			if part.FunctionCall == nil {
				continue
			}
			if index >= len(entry.Actions) || entry.Actions[index].Name != part.FunctionCall.Name || entry.Actions[index].ID == "" {
				return nil, fmt.Errorf("Gemini function call differs from its saved native state")
			}
			refs[entry.Actions[index].ID] = geminiCallRef{name: part.FunctionCall.Name, id: part.FunctionCall.ID}
			index++
		}
		if index != len(entry.Actions) {
			return nil, fmt.Errorf("Gemini function call count differs from its saved state")
		}
	}
	return refs, nil
}

func prepareGeminiMessages(messages []*schema.Message, history []ports.Message) error {
	refs, err := geminiHistoryRefs(history)
	if err != nil {
		return err
	}
	for _, msg := range messages {
		if msg.Role == schema.Tool {
			ref, exists := refs[msg.ToolCallID]
			if !exists {
				return fmt.Errorf("Gemini tool receipt has no saved function call")
			}
			msg.ToolName = ref.name
		}
	}
	return nil
}

type geminiTransport struct{ base http.RoundTripper }

func (t *geminiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if ctx, ok := req.Context().Value(originalContextKey{}).(context.Context); ok {
		req = req.Clone(ctx)
	}
	history, _ := req.Context().Value(providerHistoryKey{}).([]ports.Message)
	refs, err := geminiHistoryRefs(history)
	if err != nil {
		return nil, &notSent{err}
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, &notSent{err}
	}
	_ = req.Body.Close()
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return nil, &notSent{fmt.Errorf("invalid serialized Gemini request")}
	}
	// 实际请求记录使用完整声明 schema，避免 SDK 投影丢失协议约束。
	if definitions, ok := req.Context().Value(toolSchemasKey{}).([]ports.ToolDefinition); ok && len(definitions) > 0 {
		declarations := []any{}
		for _, definition := range definitions {
			declarations = append(declarations, map[string]any{"name": definition.Name, "description": definition.Description, "parametersJsonSchema": definition.Parameters})
		}
		body["tools"], err = json.Marshal([]any{map[string]any{"functionDeclarations": declarations}})
		if err != nil {
			return nil, &notSent{err}
		}
	}
	var contents []json.RawMessage
	if json.Unmarshal(body["contents"], &contents) != nil {
		return nil, &notSent{fmt.Errorf("Gemini request has no contents")}
	}
	assistants := []string{}
	toolIDs := []string{}
	for _, entry := range history {
		if entry.Role == "assistant" {
			assistants = append(assistants, entry.ProviderState)
		} else if entry.Role == "tool" {
			toolIDs = append(toolIDs, entry.ToolCallID)
		}
	}
	ai, ti := 0, 0
	for i, content := range contents {
		var value map[string]json.RawMessage
		if json.Unmarshal(content, &value) != nil {
			return nil, &notSent{fmt.Errorf("invalid Gemini content")}
		}
		var role string
		_ = json.Unmarshal(value["role"], &role)
		if role == "model" {
			if ai >= len(assistants) {
				return nil, &notSent{fmt.Errorf("Gemini assistant history differs from recorded history")}
			}
			contents[i] = json.RawMessage(assistants[ai])
			ai++
			continue
		}
		var parts []map[string]json.RawMessage
		_ = json.Unmarshal(value["parts"], &parts)
		changed := false
		for _, part := range parts {
			if part["functionResponse"] == nil {
				continue
			}
			if ti >= len(toolIDs) {
				return nil, &notSent{fmt.Errorf("Gemini tool history differs from recorded history")}
			}
			ref, ok := refs[toolIDs[ti]]
			if !ok {
				return nil, &notSent{fmt.Errorf("Gemini tool receipt has no saved native call")}
			}
			var response map[string]json.RawMessage
			_ = json.Unmarshal(part["functionResponse"], &response)
			response["name"], _ = json.Marshal(ref.name)
			if ref.id != "" {
				response["id"], _ = json.Marshal(ref.id)
			}
			part["functionResponse"], _ = json.Marshal(response)
			ti++
			changed = true
		}
		if changed {
			value["parts"], _ = json.Marshal(parts)
			contents[i], _ = json.Marshal(value)
		}
	}
	if ai != len(assistants) || ti != len(toolIDs) {
		return nil, &notSent{fmt.Errorf("Gemini native conversation is incomplete")}
	}
	body["contents"], _ = json.Marshal(contents)
	raw, err = json.Marshal(body)
	if err != nil {
		return nil, &notSent{err}
	}
	req.Body = io.NopCloser(bytes.NewReader(raw))
	req.ContentLength = int64(len(raw))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	response, err := t.base.RoundTrip(req)
	if response != nil && response.Body != nil {
		if capture, ok := req.Context().Value(responseCaptureKey{}).(*responseCapture); ok {
			capture.status = response.StatusCode
			response.Body = &capturedBody{ReadCloser: response.Body, capture: capture}
		}
	}
	return response, err
}

func decodeGeminiResponse(out *ports.Response) error {
	var wire struct {
		Candidates []struct {
			Content      json.RawMessage `json:"content"`
			FinishReason string          `json:"finishReason"`
		} `json:"candidates"`
		Usage *struct {
			Input    *int `json:"promptTokenCount"`
			Output   *int `json:"candidatesTokenCount"`
			Thoughts int  `json:"thoughtsTokenCount"`
			Cached   int  `json:"cachedContentTokenCount"`
			Total    *int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(out.Raw, &wire) != nil || len(wire.Candidates) == 0 {
		return fmt.Errorf("Gemini response has no candidate")
	}
	if u := wire.Usage; u != nil && u.Input != nil && u.Output != nil {
		if *u.Input < 0 || *u.Output < 0 || u.Thoughts < 0 || u.Cached < 0 || u.Cached > *u.Input || u.Thoughts > int(^uint(0)>>1)-*u.Output {
			return fault.New("MODEL_USAGE_INVALID", "Gemini 用量无法可靠计价，保留本次预留")
		}
		output := *u.Output + u.Thoughts
		if u.Total != nil && (output > int(^uint(0)>>1)-*u.Input || *u.Total != *u.Input+output) {
			return fault.New("MODEL_USAGE_INVALID", "Gemini 总用量与输入、回答和思考 token 不一致，保留本次预留")
		}
		out.Usage = billing.Usage{Input: *u.Input, Output: output, CacheHit: u.Cached, Known: true}
	}
	var content nativeGeminiContent
	if json.Unmarshal(wire.Candidates[0].Content, &content) != nil || content.Role != "model" || len(content.Parts) == 0 {
		return fmt.Errorf("Gemini response has no native model content")
	}
	out.ProviderState = string(wire.Candidates[0].Content)
	out.Reasoning = ""
	index := 0
	for _, part := range content.Parts {
		if part.Thought {
			out.Reasoning += part.Text
		}
		if part.FunctionCall != nil {
			if index >= len(out.Actions) || out.Actions[index].Name != part.FunctionCall.Name {
				return fmt.Errorf("Gemini decoded function call differs from its native response")
			}
			if part.FunctionCall.ID != "" {
				out.Actions[index].ID = part.FunctionCall.ID
			}
			index++
		}
	}
	if index != len(out.Actions) {
		return fmt.Errorf("Gemini decoded function call count differs from its native response")
	}
	switch wire.Candidates[0].FinishReason {
	case "STOP":
		out.FinishReason = "stop"
		if len(out.Actions) > 0 {
			out.FinishReason = "tool_calls"
		}
	case "MAX_TOKENS":
		out.FinishReason = "length"
	default:
		return fmt.Errorf("Gemini generation stopped with %s", wire.Candidates[0].FinishReason)
	}
	return nil
}
