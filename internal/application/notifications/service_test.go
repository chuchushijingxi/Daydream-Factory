package notifications

import (
	"context"
	"drama-agent/internal/application/ports"
	"testing"
)

func TestNotificationsRequireSharedActorBeforeStore(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	if _, e := s.List(ctx, ports.Actor{}, Query{Page: 1, PageSize: 20, Status: "all"}); e == nil {
		t.Fatal("empty actor admitted")
	}
	if _, e := s.Create(ctx, ports.Actor{UserID: "ordinary", Role: ports.RoleUser}, Input{}); e == nil {
		t.Fatal("ordinary operator admitted")
	}
	if _, e := s.OperatorList(ctx, ports.Actor{UserID: "op", Role: ports.RoleOperator}, Query{Page: 1, PageSize: 20, Status: "unread"}); e == nil {
		t.Fatal("ignored operator filter")
	}
}
func TestNotificationContentValidation(t *testing.T) {
	for _, v := range []Input{{Title: "ok", Body: "body", Audience: "all", RecipientUserID: "other"}, {Title: "bad\nline", Body: "body", Audience: "all"}, {Title: "ok", Body: "body", Audience: "user"}} {
		if _, e := v.Normalize(); e == nil {
			t.Fatalf("accepted invalid %+v", v)
		}
	}
	if v, e := (Input{Title: " 通知 ", Body: " ## 内容 ", Audience: "all"}).Normalize(); e != nil || v.Title != "通知" || v.Body != " ## 内容 " {
		t.Fatal(v, e)
	}
	if _, e := (Input{Title: "空正文草稿", Audience: "all"}).Normalize(); e != nil {
		t.Fatal("image-first draft must allow empty body", e)
	}
}
