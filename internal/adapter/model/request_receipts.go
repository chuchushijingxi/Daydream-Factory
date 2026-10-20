package model

import (
	"bytes"
	"encoding/json"
	"reflect"

	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/project"
)

// ContainsToolReceipt 只核对请求中已发送的工具回执；供应商格式留在适配层。
func ContainsToolReceipt(raw json.RawMessage, a d.Action) bool {
	var request struct {
		Dialogue []ports.Message
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return false
	}
	for _, m := range request.Dialogue {
		if m.Role == "tool" && m.ToolCallID == a.ID && m.Content == a.Result {
			return true
		}
	}
	for _, m := range request.Messages {
		if m.Role == "tool" && m.ToolCallID == a.ID && m.Content == a.Result {
			return true
		}
	}
	return false
}

func (m *chatModel) ContainsToolReceipt(raw json.RawMessage, a d.Action, _ d.Call) bool {
	return ContainsToolReceipt(raw, a)
}

// 原生回执按原生 ID 或已保存签名内容中的 action 顺序配对。
// 无原生 ID 时必须同时核对完整 model content 和持久化 action 位置，不能只匹配同名工具。
func (m *Gemini) ContainsToolReceipt(raw json.RawMessage, a d.Action, origin d.Call) bool {
	var request struct {
		Contents []json.RawMessage `json:"contents"`
	}
	if a.ID == "" || json.Unmarshal(raw, &request) != nil || !json.Valid([]byte(a.Result)) {
		return false
	}
	type response struct {
		ID, Name string
		Response json.RawMessage `json:"response"`
	}
	type part struct {
		Call     *nativeFunctionCall `json:"functionCall"`
		Response *response           `json:"functionResponse"`
	}
	var calls []nativeFunctionCall
	position, originPosition := 0, -1
	for i, saved := range origin.Actions {
		if saved.ID == a.ID && saved.Name == a.Name {
			originPosition = i
			break
		}
	}
	originMatches := false
	for _, rawContent := range request.Contents {
		var content struct {
			Role  string `json:"role"`
			Parts []part `json:"parts"`
		}
		if json.Unmarshal(rawContent, &content) != nil {
			return false
		}
		if content.Role == "model" {
			calls, position = nil, 0
			originMatches = originPosition >= 0 && sameReceiptJSON(rawContent, []byte(origin.ProviderState))
			for _, part := range content.Parts {
				if part.Call != nil {
					calls = append(calls, *part.Call)
				}
			}
			if originMatches {
				if len(calls) != len(origin.Actions) {
					return false
				}
				for i, call := range calls {
					if call.Name != origin.Actions[i].Name || origin.Actions[i].ID == "" {
						return false
					}
				}
			}
			continue
		}
		for _, part := range content.Parts {
			r := part.Response
			if r == nil {
				continue
			}
			if position >= len(calls) || r.ID != calls[position].ID || r.Name != calls[position].Name {
				return false
			}
			matched := r.ID != "" && r.ID == a.ID || r.ID == "" && originMatches && position == originPosition
			position++
			if matched && r.Name == a.Name {
				return sameReceiptJSON(r.Response, []byte(a.Result))
			}
		}
	}
	return false
}
func sameReceiptJSON(a, b []byte) bool {
	if !json.Valid(a) || !json.Valid(b) {
		return false
	}
	decode := func(raw []byte) (any, error) {
		var v any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		err := decoder.Decode(&v)
		return v, err
	}
	x, errX := decode(a)
	y, errY := decode(b)
	return errX == nil && errY == nil && reflect.DeepEqual(x, y)
}

func (m *chatModel) ResponseRequestID(raw json.RawMessage) string {
	var v struct {
		ID         string `json:"id"`
		ResponseID string `json:"responseId"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	id := v.ID
	if id == "" {
		id = v.ResponseID
	}
	if len(id) > 256 {
		return ""
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':') {
			return ""
		}
	}
	return id
}
