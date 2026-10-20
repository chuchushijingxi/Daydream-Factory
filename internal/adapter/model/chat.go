package model

import (
	"bytes"
	"context"
	"drama-agent/internal/adapter/appconfig"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"

	"drama-agent/internal/adapter/transport"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	d "drama-agent/internal/domain/project"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

type chatModel struct {
	component       model.BaseChatModel
	key             string
	window, output  int
	outputOption    func(int) model.Option
	prepareMessages func([]*schema.Message, []ports.Message) error
	decodeResponse  func(*ports.Response) error
}

func newModelClient(settings ...appconfig.Models) *http.Client {
	client := transport.NewClient(settings...)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Transport = &callerContextTransport{base: client.Transport}
	return client
}

func (m *chatModel) Capacity() (int, int) { return m.window, m.output }

func (m *chatModel) Generate(ctx context.Context, req ports.Request, observe func(json.RawMessage) error) (ports.Response, error) {
	capture := &responseCapture{}
	ctx = context.WithValue(ctx, responseCaptureKey{}, capture)
	ctx = transport.WithRequestObserver(ctx, func(raw []byte) error { return observe(json.RawMessage(raw)) })
	ctx = context.WithValue(ctx, toolSchemasKey{}, req.Tools)
	ctx = context.WithValue(ctx, providerHistoryKey{}, req.Dialogue)
	ctx = context.WithValue(ctx, originalContextKey{}, ctx)
	options := []model.Option{m.outputOption(req.MaxOutputTokens)}
	infos := []*schema.ToolInfo{}
	for _, definition := range req.Tools {
		shape := &jsonschema.Schema{}
		if err := json.Unmarshal(definition.Parameters, shape); err != nil {
			return ports.Response{}, &notSent{err}
		}
		infos = append(infos, &schema.ToolInfo{Name: definition.Name, Desc: definition.Description, ParamsOneOf: schema.NewParamsOneOfByJSONSchema(shape)})
	}
	if len(infos) != 0 {
		options = append(options, model.WithTools(infos))
	}
	messages := []*schema.Message{schema.SystemMessage(req.System), schema.UserMessage(req.Input)}
	for _, entry := range req.Dialogue {
		message := &schema.Message{Role: schema.RoleType(entry.Role), Content: entry.Content, ReasoningContent: entry.Reasoning, ToolCallID: entry.ToolCallID}
		for _, action := range entry.Actions {
			message.ToolCalls = append(message.ToolCalls, schema.ToolCall{ID: action.ID, Type: "function", Function: schema.FunctionCall{Name: action.Name, Arguments: action.Arguments}})
		}
		messages = append(messages, message)
	}
	if req.CurrentState != "" {
		messages = append(messages, schema.UserMessage(req.CurrentState))
	}
	if m.prepareMessages != nil {
		if err := m.prepareMessages(messages, req.Dialogue); err != nil {
			return ports.Response{}, &notSent{err}
		}
	}
	msg, err := m.component.Generate(ctx, messages, options...)
	if err != nil {
		var unsent interface{ RequestNotSent() bool }
		safe := errors.New(strings.ReplaceAll(err.Error(), m.key, "[redacted]"))
		if errors.As(err, &unsent) && unsent.RequestNotSent() {
			return ports.Response{}, &notSent{safe}
		}
		if ctx.Err() == nil && (capture.status == 0 || capture.status < 400) && interruptedTransport(err) {
			safe = &responseInterrupted{safe}
		}
		return ports.Response{Raw: json.RawMessage(strings.ReplaceAll(capture.raw.String(), m.key, "[redacted]"))}, safe
	}
	if msg == nil {
		return ports.Response{}, errors.New("provider returned nil response")
	}
	out := ports.Response{Text: msg.Content, Reasoning: msg.ReasoningContent, Raw: append(json.RawMessage(nil), capture.raw.Bytes()...)}
	for _, call := range msg.ToolCalls {
		out.Actions = append(out.Actions, d.Action{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments, Status: "pending"})
	}
	if msg.ResponseMeta != nil {
		out.FinishReason = msg.ResponseMeta.FinishReason
	}
	if m.decodeResponse != nil {
		err := m.decodeResponse(&out)
		return out, err
	}
	// Eino SDK 把缺失的用量映射为零值；必须检查原始响应，
	// 区分「真实零用量」与「计费信息不可用」。
	var wire struct {
		Usage *struct {
			Input    *int `json:"prompt_tokens"`
			Output   *int `json:"completion_tokens"`
			CacheHit int  `json:"prompt_cache_hit_tokens"`
			Details  struct {
				Cached int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(out.Raw, &wire) == nil && wire.Usage != nil && wire.Usage.Input != nil && wire.Usage.Output != nil && *wire.Usage.Input >= 0 && *wire.Usage.Output >= 0 {
		u := wire.Usage
		out.Usage = billing.Usage{Input: *u.Input, Output: *u.Output, CacheHit: max(u.CacheHit, u.Details.Cached), Known: true}
	}
	return out, nil
}

type notSent struct{ error }

func (*notSent) RequestNotSent() bool { return true }

func (e *notSent) Unwrap() error { return e.error }

// 先对类型化运输失败分类，再对 SDK 错误脱敏。无效 JSON、HTTP 拒绝、
// 取消和模型输出都不是重试信号。
func interruptedTransport(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var network net.Error
	return errors.As(err, &network) && (network.Timeout() || network.Temporary())
}

type responseInterrupted struct{ error }

func (*responseInterrupted) ResponseInterrupted() bool { return true }

type originalContextKey struct{}

type providerHistoryKey struct{}

type toolSchemasKey struct{}

type callerContextTransport struct{ base http.RoundTripper }

type responseCaptureKey struct{}

type responseCapture struct {
	raw    bytes.Buffer
	status int
}

type capturedBody struct {
	io.ReadCloser
	capture *responseCapture
}

func (b *capturedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		_, _ = b.capture.raw.Write(p[:n])
	}
	return n, err
}

// 保留调用者的取消语义；SDK 的默认超时不得截断
// 已获许可的响应，进而诱发重复付费请求。
func (t *callerContextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if ctx, ok := req.Context().Value(originalContextKey{}).(context.Context); ok {
		req = req.Clone(ctx)
	}
	// 工具声明是执行与采用的共同合同。SDK 可能转换或排序 schema，
	// 在持久观察与网络传输前恢复原始声明，避免供应商与解析器分叉。
	if definitions, ok := req.Context().Value(toolSchemasKey{}).([]ports.ToolDefinition); ok && len(definitions) > 0 {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, &notSent{err}
		}
		_ = req.Body.Close()
		var body map[string]json.RawMessage
		if err = json.Unmarshal(raw, &body); err != nil {
			return nil, &notSent{err}
		}
		tools := []any{}
		for _, definition := range definitions {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": definition.Name, "description": definition.Description, "parameters": definition.Parameters}})
		}
		body["tools"], err = json.Marshal(tools)
		if err != nil {
			return nil, &notSent{err}
		}
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, &notSent{err}
		}
		req.Body = io.NopCloser(bytes.NewReader(raw))
		req.ContentLength = int64(len(raw))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	}
	response, err := t.base.RoundTrip(req)
	if response != nil && response.Body != nil {
		if capture, ok := req.Context().Value(responseCaptureKey{}).(*responseCapture); ok {
			capture.status = response.StatusCode
			response.Body = &capturedBody{ReadCloser: response.Body, capture: capture}
		}
	}
	return response, err
}
