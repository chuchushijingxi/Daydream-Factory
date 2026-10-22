package postgres

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	app "drama-agent/internal/application/notifications"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	domain "drama-agent/internal/domain/notifications"
)

func systemNotificationFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("isolated notification PG opt-in")
	}
	target, err := url.Parse(database)
	if err != nil || target.Hostname() != "127.0.0.1" || !strings.HasPrefix(target.Path, "/drama_accounts_test_") {
		t.Fatal("system notification test requires isolated loopback accounts database")
	}
	ctx := context.Background()
	store, err := Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	recipient, other := identity.New("system_notice_user"), identity.New("system_notice_other")
	for _, id := range []string{recipient, other} {
		if _, err = store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,'original','user',$3)`, id, id+"@example.test", []byte("unused-notification-fixture")); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.notification_receipts WHERE user_id=ANY($1)`, []string{recipient, other})
		store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.notification_messages WHERE recipient_user_id=ANY($1)`, []string{recipient, other})
		store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.runtime_events WHERE key=ANY($1)`, []string{"notifications:" + recipient, "notifications:" + other})
		store.pool.Exec(ctx, `DELETE FROM drama_catalog_v1.users WHERE id=ANY($1)`, []string{recipient, other})
	})
	return store, recipient, other
}
func TestSystemNotificationsPGIdempotencyIsolationAndOperatorBoundary(t *testing.T) {
	store, recipient, other := systemNotificationFixture(t)
	ctx := context.Background()
	input := domain.SystemInput{OperationID: identity.New("team_invite"), RecipientUserID: recipient, Title: "团队邀请", Body: "你收到一条团队邀请。", Link: domain.Link{Type: "invite", SpaceID: "team_fixture", TargetID: "invite_fixture"}}
	const attempts = 8
	messages := make(chan domain.Message, attempts)
	failures := make(chan error, attempts)
	var workers sync.WaitGroup
	for range attempts {
		workers.Add(1)
		go func() {
			defer workers.Done()
			message, err := store.SystemNotification(ctx, input)
			messages <- message
			failures <- err
		}()
	}
	workers.Wait()
	close(messages)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var message domain.Message
	for result := range messages {
		if message.ID != "" && message.ID != result.ID {
			t.Fatal("retry created another message", message.ID, result.ID)
		}
		message = result
	}
	if message.Kind != domain.System || message.AuthorID != "" || message.AuthorName != "" || message.Status != "published" || message.Link == nil || *message.Link != input.Link || message.RecipientCount != 1 {
		t.Fatal(message)
	}
	var count int
	var authorIsNull bool
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.notification_messages WHERE kind='system' AND command_id=$1 AND recipient_user_id=$2`, input.OperationID, recipient).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT author_id IS NULL FROM drama_catalog_v1.notification_messages WHERE id=$1`, message.ID).Scan(&authorIsNull); err != nil || !authorIsNull {
		t.Fatal(authorIsNull, err)
	}
	detail, err := store.NotificationDetail(ctx, recipient, message.ID)
	if err != nil || detail.Kind != domain.System || detail.Link == nil || *detail.Link != input.Link || detail.Body != input.Body {
		t.Fatal(detail, err)
	}
	if _, err = store.NotificationDetail(ctx, other, message.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("system message escaped recipient", err)
	}
	page, err := store.NotificationList(ctx, recipient, app.Query{Page: 1, PageSize: 20, Status: "unread"})
	if err != nil || page.Unread < 1 || len(page.Items) < 1 || page.Items[0].Kind != domain.System || page.Items[0].Link == nil {
		t.Fatal(page, err)
	}
	if unread, err := store.loadNotificationUnread(ctx, recipient); err != nil || unread < 1 {
		t.Fatal(unread, err)
	}
	for _, action := range []func() error{
		func() error { _, err := store.OperatorNotification(ctx, recipient, message.ID); return err },
		func() error {
			_, err := store.UpdateNotification(ctx, recipient, message.ID, app.Input{Revision: 1, Title: "changed", Body: "changed", Audience: "all"})
			return err
		},
		func() error { _, err := store.PublishNotification(ctx, recipient, message.ID, 1); return err },
		func() error { _, err := store.WithdrawNotification(ctx, recipient, message.ID, 1); return err },
		func() error {
			_, err := store.SaveNotificationImage(ctx, recipient, message.ID, app.Image{})
			return err
		},
	} {
		if err := action(); !errors.Is(err, fault.ErrNotFound) {
			t.Fatal("operator path reached a system fact", err)
		}
	}
	for _, statement := range []string{
		`UPDATE drama_catalog_v1.notification_messages SET status='withdrawn',withdrawn_at=now() WHERE id=$1`,
		`UPDATE drama_catalog_v1.notification_messages SET audience='all',recipient_user_id=NULL WHERE id=$1`,
		`UPDATE drama_catalog_v1.notification_messages SET author_id=recipient_user_id WHERE id=$1`,
	} {
		if _, err := store.pool.Exec(ctx, statement, message.ID); err == nil {
			t.Fatal("database accepted invalid system fact", statement)
		}
	}
	changed := input
	changed.Body = "a different fact"
	if _, err := store.SystemNotification(ctx, changed); err == nil {
		t.Fatal("same durable action changed message")
	}
	input.RecipientUserID = other
	otherMessage, err := store.SystemNotification(ctx, input)
	if err != nil || otherMessage.ID == message.ID {
		t.Fatal("different recipient shared identity", otherMessage, err)
	}
	var queued bool
	if err := store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.runtime_events WHERE key=$1)`, "notifications:"+recipient).Scan(&queued); err != nil || !queued {
		t.Fatal("committed message lost unread event", queued, err)
	}
}
func TestSystemNotificationsPGShareCallerRollbackAndDisabledRecipient(t *testing.T) {
	store, recipient, other := systemNotificationFixture(t)
	ctx := context.Background()
	input := domain.SystemInput{OperationID: identity.New("team_removed"), RecipientUserID: recipient, Title: "成员变更", Body: "你已离开团队。", Link: domain.Link{Type: "team", SpaceID: "team_fixture"}}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE drama_catalog_v1.users SET username='pending change' WHERE id=$1`, recipient); err != nil {
		t.Fatal(err)
	}
	message, err := systemNotifyTx(ctx, tx, input)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var name string
	var queued bool
	if err = store.pool.QueryRow(ctx, `SELECT username FROM drama_catalog_v1.users WHERE id=$1`, recipient).Scan(&name); err != nil || name != "original" {
		t.Fatal(name, err)
	}
	if _, err = store.NotificationDetail(ctx, recipient, message.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("rolled back message visible", err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.runtime_events WHERE key=$1)`, "notifications:"+recipient).Scan(&queued); err != nil || queued {
		t.Fatal("rollback left an unread event", queued, err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE drama_catalog_v1.users SET enabled=false WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	input.RecipientUserID = other
	message, err = store.SystemNotification(ctx, input)
	if err != nil {
		t.Fatal("disabled recipient blocked durable management fact", err)
	}
	if _, err = store.NotificationDetail(ctx, other, message.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("disabled account read message", err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE drama_catalog_v1.users SET enabled=true WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	if _, err = store.NotificationDetail(ctx, other, message.ID); err != nil {
		t.Fatal("restored account lost own message", err)
	}
}
