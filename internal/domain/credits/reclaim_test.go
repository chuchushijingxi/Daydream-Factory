package credits

import (
	"testing"
	"time"
)

func TestReclaimOnlyRequestedPermanentCategory(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	initial := Wallet{Balance: 150, RechargeBalance: 80, GiftBalance: 60, LegacyBalance: 10, DailyBalance: 50, DailyExpiresAt: &expiry}
	w, a, err := initial.Reclaim(Recharge, 70)
	if err != nil || w.Balance != 80 || w.RechargeBalance != 10 || w.GiftBalance != 60 || w.DailyBalance != 50 || a != (Allocation{Recharge: 70}) {
		t.Fatal(w, a, err)
	}
	w, a, err = initial.Reclaim(Gift, 10)
	if err != nil || w.Balance != 140 || w.GiftBalance != 50 || w.DailyBalance != 50 || w.DailyExpiresAt != initial.DailyExpiresAt || a != (Allocation{Gift: 10}) {
		t.Fatal(w, a, err)
	}
	for _, tt := range []struct {
		category string
		amount   int64
	}{{Gift, 11}, {Recharge, 81}, {Legacy, 1}, {Mixed, 1}, {Gift, 0}, {Gift, -1}, {Recharge, MaxAmount + 1}} {
		if _, _, err = initial.Reclaim(tt.category, tt.amount); err == nil {
			t.Fatal(tt)
		}
	}
	if _, _, err = (Wallet{Balance: MaxAmount, RechargeBalance: MaxAmount, GiftBalance: MaxAmount}).Reclaim(Recharge, 1); err == nil {
		t.Fatal("invalid wallet arithmetic")
	}
}
func TestReclaimChangeDerivesFactsAndChecksOriginalInIdempotency(t *testing.T) {
	c := Change{Amount: 1, OriginalAdditionID: "addition", OperationID: "reclaim", Subject: "operator", Reason: "退款"}
	if err := c.Validate("reclaim"); err != nil {
		t.Fatal(err)
	}
	entry := Entry{Kind: "reclaim", Amount: 1, OriginalAdditionID: c.OriginalAdditionID, OperationID: c.OperationID, Subject: c.Subject, Reason: c.Reason}
	if !entry.Same("reclaim", c) {
		t.Fatal("same reclaim")
	}
	changed := c
	changed.OriginalAdditionID = "another"
	if entry.Same("reclaim", changed) {
		t.Fatal("original addition mismatch ignored")
	}
	for _, field := range []string{"account", "source", "category", "deduction"} {
		changed = c
		switch field {
		case "account":
			changed.AccountID = "account"
		case "source":
			changed.Source = "source"
		case "category":
			changed.Category = Gift
		case "deduction":
			changed.OriginalDeductionID = "deduction"
		}
		if changed.Validate("reclaim") == nil {
			t.Fatal(field)
		}
	}
	for _, kind := range []string{"add", "deduct", "refund"} {
		if c.Validate(kind) == nil {
			t.Fatal("other action accepted addition", kind)
		}
	}
	f := HistoryFilter{Page: 1, PageSize: 10, Direction: "reclaimed", Category: "all"}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
}
