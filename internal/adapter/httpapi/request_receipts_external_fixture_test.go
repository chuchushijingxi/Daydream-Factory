package httpapi_test

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
func (m *continuingModel) ContainsToolReceipt(raw json.RawMessage, a d.Action, origin d.Call) bool {
	return testRequestContainsReceipt(raw, a)
}
func (m *contentScriptModel) ContainsToolReceipt(raw json.RawMessage, a d.Action, origin d.Call) bool {
	return testRequestContainsReceipt(raw, a)
}
func (m *protocolModel) ContainsToolReceipt(raw json.RawMessage, a d.Action, origin d.Call) bool {
	return testRequestContainsReceipt(raw, a)
}
func (m *slowFirstModel) ContainsToolReceipt(raw json.RawMessage, a d.Action, origin d.Call) bool {
	return testRequestContainsReceipt(raw, a)
}
func (m interfaceOnlyModel) ContainsToolReceipt(raw json.RawMessage, a d.Action, origin d.Call) bool {
	return testRequestContainsReceipt(raw, a)
}
