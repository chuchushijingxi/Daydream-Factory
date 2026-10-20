package credits

import (
	"testing"
	"time"
)

func TestDailyExpiryBoundaryAndSpendingOrder(t *testing.T) {
	expiry := time.Date(2026, 10, 19, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 28800))
	w := Wallet{Balance: 100, GiftBalance: 60, RechargeBalance: 40, DailyBalance: 30, DailyExpiresAt: &expiry}
	if got, a := w.Expire(expiry.Add(-time.Nanosecond)); got.Balance != 100 || a.Total() != 0 {
		t.Fatal(got, a)
	}
	after, spent, err := w.Deduct(20)
	if err != nil || spent.Daily != 20 || spent.Gift != 20 || after.DailyBalance != 10 || !spent.DailyExpiresAt.Equal(expiry) {
		t.Fatal(after, spent, err)
	}
	final, reclaimed := after.Expire(expiry)
	if final.Balance != 70 || final.GiftBalance != 30 || final.RechargeBalance != 40 || final.DailyBalance != 0 || final.DailyExpiresAt != nil || reclaimed.Daily != 10 {
		t.Fatal(final, reclaimed)
	}
	if _, again := final.Expire(expiry.Add(time.Hour)); again.Total() != 0 {
		t.Fatal(again)
	}
	original := Entry{Kind: "deduct", Amount: 20, GiftAmount: 20, DailyAmount: 20, DailyExpiresAt: spent.DailyExpiresAt}
	allocation, err := original.RefundAllocation(20, Allocation{})
	if err != nil {
		t.Fatal(err)
	}
	restored, expired, err := after.Restore(allocation, expiry.Add(-time.Nanosecond))
	if err != nil || restored.Balance != 100 || restored.DailyBalance != 30 || !restored.DailyExpiresAt.Equal(expiry) || expired.Total() != 0 {
		t.Fatal(restored, expired, err)
	}
}

func TestCrossDayPartialRefundPreservesNewDailyAndOriginalExpiry(t *testing.T) {
	oldExpiry := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
	newExpiry := oldExpiry.Add(24 * time.Hour)
	original := Entry{Kind: "deduct", Amount: 40, RechargeAmount: 10, GiftAmount: 30, DailyAmount: 20, DailyExpiresAt: &oldExpiry}
	wallet := Wallet{Balance: 50, GiftBalance: 50, DailyBalance: 50, DailyExpiresAt: &newExpiry}
	returned := Allocation{}
	for i, amount := range []int64{10, 10, 20} {
		part, err := original.RefundAllocation(amount, returned)
		if err != nil {
			t.Fatal(err)
		}
		next, expired, err := wallet.Restore(part, oldExpiry)
		if err != nil || next.DailyBalance != 50 || !next.DailyExpiresAt.Equal(newExpiry) {
			t.Fatal(next, err)
		}
		if i < 2 && expired.Total() != 0 {
			t.Fatal("permanent funds expired", part)
		}
		if i == 2 && (expired.Daily != 20 || !expired.DailyExpiresAt.Equal(oldExpiry) || next.Balance != 70) {
			t.Fatal(next, expired)
		}
		returned.Recharge += part.Recharge
		returned.Gift += part.Gift
		returned.Daily += part.Daily
		wallet = next
	}
	if _, err := original.RefundAllocation(1, returned); err == nil {
		t.Fatal("over-refund accepted")
	}
	maxed := Wallet{Balance: MaxAmount, RechargeBalance: MaxAmount}
	got, expired, err := maxed.Restore(Allocation{Gift: 20, Daily: 20, DailyExpiresAt: &oldExpiry}, oldExpiry)
	if err != nil || got != maxed || expired.Daily != 20 {
		t.Fatal("expired return must not overflow usable balance", got, expired, err)
	}
}
