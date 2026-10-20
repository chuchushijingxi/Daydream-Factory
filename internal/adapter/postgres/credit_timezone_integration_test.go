package postgres_test

import (
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"net/url"
	"testing"
	"time"

	c "drama-agent/internal/application/credits"
	"drama-agent/internal/domain/identity"
)

func TestDailyGiftUsesOneBusinessDayAcrossDatabaseTimezones(t *testing.T) {
	store, conn := identityDB(t)
	ctx := context.Background()
	policy, err := store.CreditGiftPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := conn.Exec(context.Background(), `UPDATE drama_catalog_v1.credit_gift_policy SET registration_gift=$1,daily_gift=$2 WHERE id`, policy.RegistrationGift, policy.DailyGift); e != nil {
			t.Error(e)
		}
	})
	if _, err = store.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: policy.Revision, RegistrationGift: 0, DailyGift: 11}); err != nil {
		t.Fatal(err)
	}
	user := createUser(t, store, identity.New("timezone_gift")+"@example.test")
	dates := map[string]int{}
	for _, zone := range []string{"UTC", "Pacific/Kiritimati", "America/Los_Angeles"} {
		// 修改各服务连接的会话时区，不修改共享服务器、正式库或宿主机时钟。
		connectionURL, err := url.Parse(conn.Config().ConnString())
		if err != nil {
			t.Fatal("isolated connection URL unavailable")
		}
		query := connectionURL.Query()
		query.Set("timezone", zone)
		connectionURL.RawQuery = query.Encode()
		instance, err := pgfixture.Open(t, ctx, connectionURL.String())
		if err != nil {
			t.Fatal(zone, err)
		}
		var before, after time.Time
		if err = conn.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&before); err != nil {
			instance.Close()
			t.Fatal(err)
		}
		receipt, claimErr := instance.ClaimDailyCreditGift(ctx, user.ID)
		instance.Close()
		if err = conn.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&after); err != nil || claimErr != nil {
			t.Fatal(zone, err, claimErr)
		}
		if receipt.Date != c.GiftDate(before) && receipt.Date != c.GiftDate(after) {
			t.Fatal("database session timezone changed the business date", zone, receipt.Date)
		}
		if !receipt.AlreadyClaimed {
			dates[receipt.Date]++
		} else if dates[receipt.Date] != 1 {
			t.Fatal("readback has no original claim", zone, receipt)
		}
		if receipt.Amount != 11 || dates[receipt.Date] != 1 {
			t.Fatal("changing timezone awarded the same day twice", zone, receipt)
		}
		entry, err := store.CreditOperation(ctx, "gift:daily:"+user.ID+":"+receipt.Date)
		midnight, parseErr := time.Parse(time.RFC3339, receipt.Date+"T00:00:00+08:00")
		if err != nil || parseErr != nil || entry.DailyExpiresAt == nil || !entry.DailyExpiresAt.Equal(midnight.AddDate(0, 0, 1)) {
			t.Fatal("session timezone changed the absolute deadline", zone, entry, err, parseErr)
		}
	}
	var count int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND source='daily-gift'`, user.ID).Scan(&count); err != nil || count != len(dates) {
		t.Fatal("timezone changes duplicated funding", count, len(dates), err)
	}
}
