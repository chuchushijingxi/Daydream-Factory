package credits

import (
	"errors"
	"math"
	"testing"

	"drama-agent/internal/domain/fault"
)

func TestWalletSpendingAndPartialRefundRestoreOriginalSources(t *testing.T) {
	before := Wallet{Balance: 100, GiftBalance: 30, RechargeBalance: 50, LegacyBalance: 20}
	after, spent, err := before.Deduct(90)
	if err != nil || after != (Wallet{Balance: 10, LegacyBalance: 10}) || spent != (Allocation{Gift: 30, Recharge: 50, Legacy: 10}) || spent.Category() != Mixed {
		t.Fatal(after, spent, err)
	}
	original := Entry{Kind: "deduct", Amount: 90, GiftAmount: 30, RechargeAmount: 50, LegacyAmount: 10}
	returned := Allocation{}
	for _, step := range []struct {
		amount int64
		want   Allocation
	}{{20, Allocation{Recharge: 20}}, {40, Allocation{Recharge: 30, Gift: 10}}, {30, Allocation{Gift: 20, Legacy: 10}}} {
		part, err := original.RefundAllocation(step.amount, returned)
		if err != nil || part != step.want {
			t.Fatal(part, step.want, err)
		}
		after, err = after.Credit(part)
		if err != nil {
			t.Fatal(err)
		}
		returned.Recharge += part.Recharge
		returned.Gift += part.Gift
		returned.Legacy += part.Legacy
	}
	if after != before {
		t.Fatal("full refund changed the original sources", after, before)
	}
	if _, err := original.RefundAllocation(1, returned); err == nil {
		t.Fatal("over-refund accepted")
	}
}

func TestWalletInsufficientOverflowAndInvalidRefundPreserveValues(t *testing.T) {
	wallet := Wallet{Balance: 2, GiftBalance: 2}
	_, _, err := wallet.Deduct(3)
	var command *fault.Error
	if !errors.As(err, &command) || command.Code != "CREDIT_INSUFFICIENT" || wallet.Balance != 2 {
		t.Fatal(wallet, err)
	}
	if _, err := (Wallet{Balance: MaxAmount, GiftBalance: MaxAmount}).Credit(Allocation{Gift: 1}); err == nil {
		t.Fatal("balance overflow accepted")
	}
	if _, err := wallet.Credit(Allocation{Gift: math.MaxInt64, Recharge: math.MaxInt64, Legacy: 3}); err == nil {
		t.Fatal("allocation overflow wrapped into a valid amount")
	}
	if _, err := (Entry{Kind: "add", Amount: 2, GiftAmount: 2}).RefundAllocation(1, Allocation{}); err == nil {
		t.Fatal("funding was treated as a deduction")
	}
	for _, category := range []string{Gift, Recharge} {
		allocation, err := Funding(2, category)
		if err != nil || allocation.Total() != 2 || allocation.Category() != category {
			t.Fatal(allocation, err)
		}
	}
	if _, err := Funding(1, Legacy); err == nil {
		t.Fatal("new legacy funding accepted")
	}
}
