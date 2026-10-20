package project

import (
	"encoding/json"

	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/project"
)

func testRequestContainsReceipt(raw json.RawMessage, a d.Action) bool {
	var r ports.Request
	if json.Unmarshal(raw, &r) != nil {
		return false
	}
	for _, m := range r.Dialogue {
		if m.Role == "tool" && m.ToolCallID == a.ID && m.Content == a.Result {
			return true
		}
	}
	return false
}
func (m *fakeModel) ContainsToolReceipt(raw json.RawMessage, a d.Action, origin d.Call) bool {
	return testRequestContainsReceipt(raw, a)
}
func (m preferenceOnlyModel) ContainsToolReceipt(raw json.RawMessage, a d.Action, origin d.Call) bool {
	return testRequestContainsReceipt(raw, a)
}
