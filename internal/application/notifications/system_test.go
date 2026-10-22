package notifications

import (
	"context"
	"testing"

	domain "drama-agent/internal/domain/notifications"
)

type systemStore struct {
	Store
	called int
	input  SystemInput
}

func (s *systemStore) SystemNotification(_ context.Context, in SystemInput) (Message, error) {
	s.called++
	s.input = in
	return Message{Kind: domain.System, Link: &in.Link, Title: in.Title, Body: in.Body, Status: "published", RecipientUserID: in.RecipientUserID}, nil
}
func TestSystemNotifyKeepsActionIdentityAndRejectsIncompleteInput(t *testing.T) {
	ctx := context.Background()
	store := &systemStore{}
	service := &Service{Store: store}
	input := SystemInput{OperationID: "join-approved:request_1", RecipientUserID: "user", Title: " 申请通过 ", Body: "你已加入团队。", Link: Link{Type: "team", SpaceID: "team"}}
	message, err := service.SystemNotify(ctx, input)
	if err != nil || store.called != 1 || store.input.OperationID != input.OperationID || message.Kind != domain.System || message.Link == nil || message.Link.SpaceID != "team" || message.Title != "申请通过" {
		t.Fatal(message, store.input, err)
	}
	input.OperationID = ""
	if _, err := service.SystemNotify(ctx, input); err == nil || store.called != 1 {
		t.Fatal("invalid action reached store", err)
	}
	input.OperationID = "event"
	var absent *Service
	if _, err := absent.SystemNotify(ctx, input); err == nil {
		t.Fatal("missing store accepted system notification")
	}
}
