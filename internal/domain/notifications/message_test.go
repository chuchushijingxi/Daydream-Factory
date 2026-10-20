package notifications

import "testing"

func TestNotificationLifecycleKeepsVersionedIdempotency(t *testing.T) {
	draft := Message{Status: "draft", Revision: 3, Body: "通知正文"}
	if needed, err := draft.PublicationNeeded(3); err != nil || !needed {
		t.Fatal(needed, err)
	}
	if _, err := draft.PublicationNeeded(2); err == nil {
		t.Fatal("stale version published")
	}
	draft.Body = " \n\t"
	if _, err := draft.PublicationNeeded(3); err == nil {
		t.Fatal("blank body published")
	}
	published := Message{Status: "published", Revision: 4}
	if needed, err := published.PublicationNeeded(3); err != nil || needed {
		t.Fatal("same publication was not idempotent", needed, err)
	}
	if published.RequireDraft(4) == nil {
		t.Fatal("published notification editable")
	}
	if needed, err := published.WithdrawalNeeded(4); err != nil || !needed {
		t.Fatal(needed, err)
	}
	withdrawn := Message{Status: "withdrawn", Revision: 5}
	if needed, err := withdrawn.WithdrawalNeeded(4); err != nil || needed {
		t.Fatal("withdrawal replay changed state", needed, err)
	}
	if _, err := withdrawn.PublicationNeeded(5); err == nil {
		t.Fatal("withdrawn notification republished")
	}
	if _, err := draft.WithdrawalNeeded(3); err == nil {
		t.Fatal("draft withdrawn")
	}
}
