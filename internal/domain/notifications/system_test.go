package notifications

import (
	"strings"
	"testing"
)

func TestSystemInputValidatesDurableActionAndTarget(t *testing.T) {
	input := SystemInput{OperationID: "team-invite:invite_1", RecipientUserID: "user_1", Title: " 团队邀请 ", Body: "完整事实正文\n下一行", Link: Link{Type: "invite", SpaceID: "team_1", TargetID: "invite_1"}}
	normalized, err := input.Normalize()
	if err != nil || normalized.Title != "团队邀请" || normalized.Body != input.Body || normalized.Link != input.Link {
		t.Fatal(normalized, err)
	}
	for _, mutate := range []func(*SystemInput){
		func(v *SystemInput) { v.OperationID = "" }, func(v *SystemInput) { v.OperationID = "event\n" },
		func(v *SystemInput) { v.RecipientUserID = "" }, func(v *SystemInput) { v.Body = " \n " },
		func(v *SystemInput) { v.Link.Type = "" }, func(v *SystemInput) { v.Link.SpaceID = "\x00secret" },
		func(v *SystemInput) { v.Link.TargetID = strings.Repeat("x", 257) },
	} {
		invalid := input
		mutate(&invalid)
		if _, err := invalid.Normalize(); err == nil {
			t.Fatal("invalid system notification accepted", invalid)
		}
	}
}
func TestSystemMessagesCannotUseOperatorLifecycle(t *testing.T) {
	message := Message{Kind: System, Status: "published", Revision: 1, Body: "系统事实"}
	if message.RequireDraft(1) == nil {
		t.Fatal("system message became a draft")
	}
	if _, err := message.PublicationNeeded(0); err == nil {
		t.Fatal("system notification used operator publication replay")
	}
	if _, err := message.WithdrawalNeeded(1); err == nil {
		t.Fatal("system notification withdrawn by operator")
	}
}
