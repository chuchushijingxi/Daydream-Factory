package ports

import (
	"context"
	"encoding/json"

	"drama-agent/internal/domain/billing"
	d "drama-agent/internal/domain/project"
)

type Request struct {
	System string
	Input  string
	// CurrentState 是会变化的实际结果，运输时置于完整工具对话之后，保护稳定材料与历史的缓存前缀。
	CurrentState    string
	MaxOutputTokens int
	Tools           []ToolDefinition
	Dialogue        []Message
}

// Message 承载当前任务对话；思考属于供应商协议数据，不作为剧情证据。
type Message struct {
	Role          string
	Content       string
	Reasoning     string
	ProviderState string
	Actions       []d.Action
	ToolCallID    string
}

type ToolDefinition struct {
	Name, Description string
	Parameters        json.RawMessage
}

type Response struct {
	Text          string
	Reasoning     string
	ProviderState string
	Raw           json.RawMessage
	FinishReason  string
	Usage         billing.Usage
	Actions       []d.Action
}

// Eino 管理模型/工具迭代；回调只暴露已记账调用和原子工具执行，
// 应用层不依赖框架消息或 Runner 类型。
type AgentSession struct {
	RejectUnknownTools bool
	Tools              []ToolDefinition
	Instruction        string
	Dialogue           []Message
	Generate           func(context.Context, []Message) (Response, error)
	Execute            func(context.Context, string) (ToolResult, error)
}

type ToolResult struct {
	Text     string
	Finished bool
}

type Agent interface {
	Run(context.Context, AgentSession) error
}

type Model interface {
	// ContainsToolReceipt 核对实际请求与已保存来源 Call，返回回执是否实际送达。
	// 无原生工具 ID 的协议可用原生内容及 action 顺序定位；应用层不解析厂商格式。
	ContainsToolReceipt(json.RawMessage, d.Action, d.Call) bool
	// 仅 ResponseInterrupted() 明确分类的中断允许限次重试。
	// 不完整原答只作证据，不能成为可执行业务输出。
	Generate(context.Context, Request, func(json.RawMessage) error) (Response, error)
	Capacity() (contextWindow, maxOutput int)
}

type Importer interface {
	Parse(string, []byte, uint64) (d.ScriptSource, error)
}

// ResponseMetadata reads provider metadata without changing the model's business DTO.
type ResponseMetadata interface{ ResponseRequestID(json.RawMessage) string }
