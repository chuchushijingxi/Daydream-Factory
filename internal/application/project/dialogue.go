package project

import (
	"encoding/json"
	"fmt"
	"reflect"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// 只恢复当前任务尚未结束的对话；已完成批次通过最新目录和原文体现。
// 不重喂全部历史，避免旧行动挤占当前任务材料。
func taskDialogue(p d.Project) ([]ports.Message, error) {
	out := []ports.Message{}
	r := p.LatestRun()
	for _, c := range r.Calls {
		if interruptedCall(c) {
			continue // 中断的部分响应只是记账证据，不回到对话
		}
		if c.Stage != r.Stage || c.Start != r.Cursor || c.End != r.End {
			continue
		}
		if !validTaskActions(r.ExecutionContract(), c.FinishReason, c.Actions) {
			return nil, fmt.Errorf("%w: incomplete task response", fault.ErrInvalid)
		}
		assistant := ports.Message{Role: "assistant", Content: c.Text, Reasoning: c.Reasoning, ProviderState: c.ProviderState}
		for _, a := range c.Actions {
			assistant.Actions = append(assistant.Actions, d.Action{ID: a.ID, Name: a.Name, Arguments: a.Arguments})
		}
		out = append(out, assistant)
		for _, a := range c.Actions {
			if a.Status == "pending" || a.Result == "" {
				return nil, fmt.Errorf("%w: task tool has no committed receipt", fault.ErrInvalid)
			}
			result, err := modelSourceReceipt(p, c, a.Result)
			if err != nil {
				return nil, err
			}
			out = append(out, ports.Message{Role: "tool", Content: result, ToolCallID: a.ID})
		}
	}
	return out, nil
}

// 转发 Eino 的真实对话，完整保留当前任务的调用与工具回执配对。
// 已落库 ID 确定范围并核对恢复连续性，不能拼造历史消息。
func selectTaskDialogue(p d.Project, incoming []ports.Message) ([]ports.Message, error) {
	expected, err := taskDialogue(p)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, message := range expected {
		for _, a := range message.Actions {
			ids[a.ID] = true
		}
	}
	selected := []ports.Message{}
	for _, message := range incoming {
		if message.Role == "assistant" && len(message.Actions) > 0 && ids[message.Actions[0].ID] {
			selected = append(selected, message)
		}
		if message.Role == "tool" && ids[message.ToolCallID] {
			selected = append(selected, message)
		}
	}
	if !reflect.DeepEqual(expected, selected) {
		return nil, fmt.Errorf("%w: current assistant/tool dialogue differs from committed response and receipts", fault.ErrInvalid)
	}
	return selected, nil
}

// 容量估计覆盖实际运输的所有字段、思考、工具参数、回执及消息开销。
func inputSize(req ports.Request) int {
	raw, _ := json.Marshal(req.Dialogue)
	messages := 2 + len(req.Dialogue)
	if req.CurrentState != "" {
		messages++
	}
	return len(req.System) + len(req.Input) + len(req.CurrentState) + len(raw) + 128*messages
}
