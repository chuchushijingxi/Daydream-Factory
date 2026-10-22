package notifications

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SystemInput 是业务事实通知。OperationID来自已持久化动作，同一动作与接收人只投递一次。
// 团队事实、操作记录与本通知由调用方在同一个事务提交。
type SystemInput struct {
	OperationID     string
	RecipientUserID string
	Title           string
	Body            string
	Link            Link
}

func (v SystemInput) Normalize() (SystemInput, error) {
	draft, err := (Draft{Title: v.Title, Body: v.Body, Audience: "user", RecipientUserID: v.RecipientUserID}).Normalize()
	if err != nil {
		return SystemInput{}, err
	}
	if err = (Message{Body: draft.Body}).RequirePublishableBody(); err != nil {
		return SystemInput{}, err
	}
	if !validSystemField(v.OperationID, 256, true) || !validSystemField(v.RecipientUserID, 128, true) || !validSystemField(v.Link.Type, 64, true) || !validSystemField(v.Link.SpaceID, 128, false) || !validSystemField(v.Link.TargetID, 256, false) {
		return SystemInput{}, invalidContent()
	}
	v.Title, v.Body = draft.Title, draft.Body
	return v, nil
}
func validSystemField(value string, limit int, required bool) bool {
	return (!required || value != "") && len(value) <= limit && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}
