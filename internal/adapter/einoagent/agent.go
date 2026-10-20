package einoagent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync/atomic"

	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/project"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

type Agent struct{}

func New() *Agent { return &Agent{} }

func (a *Agent) Run(ctx context.Context, session ports.AgentSession) error {
	model := &recordedModel{session: session}
	tools := []tool.BaseTool{}
	for _, definition := range session.Tools {
		shape := &jsonschema.Schema{}
		if err := json.Unmarshal(definition.Parameters, shape); err != nil {
			return err
		}
		tools = append(tools, &sessionTool{session: session, model: model, info: &schema.ToolInfo{Name: definition.Name, Desc: definition.Description, ParamsOneOf: schema.NewParamsOneOfByJSONSchema(shape)}})
	}
	toolsConfig := compose.ToolsNodeConfig{Tools: tools, ExecuteSequentially: true}
	// 新任务为越界工具写入持久化拒绝回执；P3 保留已验收的未知工具行为。
	if session.RejectUnknownTools {
		toolsConfig.UnknownToolsHandler = func(ctx context.Context, name, input string) (string, error) {
			id := compose.GetToolCallID(ctx)
			if id == "" {
				return "", fmt.Errorf("missing provider tool call ID")
			}
			result, err := session.Execute(ctx, id)
			return result.Text, err
		}
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "catalog", Model: model,
		// 停止由取消、真实容量和本次额度决定，保持现有无固定轮数流程。
		MaxIterations: math.MaxInt,
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: toolsConfig},
	})
	if err != nil {
		return err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	input := []*schema.Message{schema.UserMessage(session.Instruction)}
	for _, message := range session.Dialogue {
		input = append(input, toSchema(message))
	}
	iterator := runner.Run(ctx, input)
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return event.Err
		}
	}
	return nil
}

type recordedModel struct {
	session  ports.AgentSession
	finished atomic.Bool
}

func (m *recordedModel) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	// finish 工具已原子提交完成，通过 Eino 正常结束图，避免多发请求。
	if m.finished.Load() {
		return schema.AssistantMessage("任务结果已保存。", nil), nil
	}
	dialogue := []ports.Message{}
	for _, message := range input {
		if message.Role != schema.Assistant && message.Role != schema.Tool {
			continue
		}
		entry := ports.Message{Role: string(message.Role), Content: message.Content, Reasoning: message.ReasoningContent, ToolCallID: message.ToolCallID}
		if state, ok := message.Extra["drama_provider_state"].(string); ok {
			entry.ProviderState = state
		}
		for _, action := range message.ToolCalls {
			entry.Actions = append(entry.Actions, d.Action{ID: action.ID, Name: action.Function.Name, Arguments: action.Function.Arguments})
		}
		dialogue = append(dialogue, entry)
	}
	response, err := m.session.Generate(ctx, dialogue)
	if err != nil {
		return nil, err
	}
	msg := &schema.Message{Role: schema.Assistant, Content: response.Text, ReasoningContent: response.Reasoning}
	if response.ProviderState != "" {
		msg.Extra = map[string]any{"drama_provider_state": response.ProviderState}
	}
	for _, action := range response.Actions {
		msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{ID: action.ID, Type: "function", Function: schema.FunctionCall{Name: action.Name, Arguments: action.Arguments}})
	}
	return msg, nil
}

func toSchema(entry ports.Message) *schema.Message {
	msg := &schema.Message{Role: schema.RoleType(entry.Role), Content: entry.Content, ReasoningContent: entry.Reasoning, ToolCallID: entry.ToolCallID}
	if entry.ProviderState != "" {
		msg.Extra = map[string]any{"drama_provider_state": entry.ProviderState}
	}
	for _, action := range entry.Actions {
		msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{ID: action.ID, Type: "function", Function: schema.FunctionCall{Name: action.Name, Arguments: action.Arguments}})
	}
	return msg
}

func (m *recordedModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

type sessionTool struct {
	info    *schema.ToolInfo
	session ports.AgentSession
	model   *recordedModel
}

func (t *sessionTool) Info(context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *sessionTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	id := compose.GetToolCallID(ctx)
	if id == "" {
		return "", fmt.Errorf("missing provider tool call ID")
	}
	result, err := t.session.Execute(ctx, id)
	if err != nil {
		return "", err
	}
	if result.Finished {
		t.model.finished.Store(true)
	}
	return result.Text, nil
}
