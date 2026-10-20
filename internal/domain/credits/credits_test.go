package credits

import (
	"math"
	"testing"
	"time"
)

func TestWholeIntegerPriceUnicodeAndPromotionBoundaries(t *testing.T) {
	if Count(" 中 文 A1！🙂 \t\n\u3000") != 6 {
		t.Fatal(Count(" 中 文 A1！🙂 \t\n\u3000"))
	}
	for _, v := range []struct{ n, p, d, w int64 }{{12345, 2, 100, 247}, {18450, 2, 100, 369}, {1, 2, 100, 1}, {100, 2, 100, 2}, {1, 0, 100, 0}, {12, 5, 1, 60}} {
		got, e := Price(v.n, v.p, v.d)
		if e != nil || got != v.w {
			t.Fatal(v, got, e)
		}
	}
	if _, e := Price(math.MaxInt64, 2, 100); e == nil {
		t.Fatal("overflow")
	}
	for _, n := range []int64{-1, 0, MaxAmount + 1} {
		if ValidAmount(n) == nil {
			t.Fatal("invalid amount", n)
		}
	}
	start := time.Now()
	end := start.Add(time.Hour)
	r := PriceRule{UnitPrice: 2, FreeEnabled: true, FreeStart: &start, FreeEnd: &end}
	if !r.Free(start) || r.Free(end) || r.Free(start.Add(-time.Nanosecond)) {
		t.Fatal("promotion boundaries")
	}
}

func TestCategoryIsExplicitOnlyForFundingAndPartOfIdempotency(t *testing.T) {
	c := Change{AccountID: "account", Amount: 2, OperationID: "operation", Source: "source", Subject: "operator", Reason: "reason"}
	if c.Validate("add") == nil {
		t.Fatal("uncategorized grant accepted")
	}
	for _, category := range []string{Recharge, Gift} {
		c.Category = category
		if err := c.Validate("add"); err != nil {
			t.Fatal(category, err)
		}
		if c.Validate("deduct") == nil {
			t.Fatal("caller selected spending allocation")
		}
	}
	c.Category = Legacy
	if c.Validate("add") == nil {
		t.Fatal("new legacy grant accepted")
	}
	c.Category = Gift
	e := Entry{Kind: "add", AccountID: c.AccountID, Amount: c.Amount, OperationID: c.OperationID, Source: c.Source, Subject: c.Subject, Reason: c.Reason, Category: Gift}
	if !e.Same("add", c) {
		t.Fatal("same grant not recognized")
	}
	c.Category = Recharge
	if e.Same("add", c) {
		t.Fatal("category rewrite ignored")
	}
	c.Category = ""
	if err := c.Validate("deduct"); err != nil {
		t.Fatal(err)
	}
	c.OriginalDeductionID = "arbitrary"
	if c.Validate("deduct") == nil {
		t.Fatal("deduction unexpectedly had an original")
	}
	for _, invalid := range []HistoryFilter{{Page: 0}, {Page: 1, PageSize: 51, Direction: "all", Category: "all"}, {Page: 1, PageSize: 10, Direction: "invalid", Category: "all"}, {Page: 1, PageSize: 10, Direction: "all", Category: "invalid"}} {
		if invalid.Validate() == nil {
			t.Fatal("invalid filter", invalid)
		}
	}
}
