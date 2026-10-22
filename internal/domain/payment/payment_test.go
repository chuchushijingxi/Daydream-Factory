package payment

import (
	"strings"
	"testing"
	"time"
)

func TestOrderTransitionsOnlyMoveForward(t *testing.T) {
	for _, from := range []Status{Pending, Credited, Closed, Refunded, "unknown"} {
		for _, to := range []Status{Pending, Credited, Closed, Refunded, "unknown"} {
			want := from == Pending && (to == Credited || to == Closed) || from == Credited && to == Refunded
			got, err := from.Transition(to)
			if (err == nil) != want || want && got != to || !want && got != from {
				t.Fatal(from, to, got, err)
			}
		}
	}
}
func TestOrderNumbersFreezeBeijingTimeAndValidate(t *testing.T) {
	at := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	for range 100 {
		no, err := GenerateOrderNo(at)
		if err != nil || len(no) != 26 || no[:16] != "PO20261020000000" || !ValidOrderNo(no) || seen[no] {
			t.Fatal(no, err)
		}
		seen[no] = true
	}
	for _, no := range []string{"PO20260230000000ABCDEFGHIJ", "PO20261020000000abcdef2345", "PO20261020000000ABCD0189AA", "PO20261020000000ABCDEFGHIJx"} {
		if ValidOrderNo(no) {
			t.Fatal(no)
		}
	}
}
func TestMoneyStringsStrictAndRoundTripSafeIntegerBoundary(t *testing.T) {
	for _, amount := range []int64{0, 1, 99, 100, 101, MaxAmount} {
		str, err := FormatAmount(amount)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ParseAmount(str)
		if err != nil || got != amount {
			t.Fatal(amount, str, got, err)
		}
	}
	for _, amount := range []string{"", "1", "1.0", "1.000", ".01", "-1.00", "+1.00", "1e2.00", " 1.00", "1.00\n", "一.00", "90071992547409.92", "9223372036854775808.00"} {
		if _, err := ParseAmount(amount); err == nil {
			t.Fatal(amount)
		}
	}
	if _, err := FormatAmount(-1); err == nil {
		t.Fatal("negative")
	}
	if _, err := FormatAmount(MaxAmount + 1); err == nil {
		t.Fatal("overflow")
	}
}
func TestPackageSnapshotsAndRefundProration(t *testing.T) {
	p := Package{Name: "测试档位", ID: "package", OriginalAmountFen: 100, AmountFen: 100, RechargeCredits: 300, GiftCredits: 100, Enabled: true}
	snapshot := p.Snapshot()
	p.AmountFen = 200
	quote, err := QuoteRefund(snapshot, 250, 5, 200, 100)
	if err != nil || quote != (RefundQuote{MaxCredits: 200, RechargeCredits: 100, GiftCredits: 5, AmountFen: 33}) {
		t.Fatal(quote, err)
	}
	// 不能以其他分类余额抬高上限，也不能超出团队未分配余额。
	if _, err := QuoteRefund(snapshot, 250, 100, 200, 201); err == nil {
		t.Fatal("team cap")
	}
	if _, err := QuoteRefund(snapshot, 0, MaxAmount, MaxAmount, 1); err == nil {
		t.Fatal("no recharge funds")
	}
	// 保留向下取整后的精确结果，不能用浮点或四舍五入。
	quote, err = QuoteRefund(snapshot, 300, 100, MaxAmount, 1)
	if err != nil || quote.AmountFen != 0 || quote.GiftCredits != 0 {
		t.Fatal(quote, err)
	}
	large := PackageSnapshot{PackageID: "large", OriginalAmountFen: MaxAmount, AmountFen: MaxAmount, RechargeCredits: MaxAmount - 1, GiftCredits: 1}
	quote, err = QuoteRefund(large, MaxAmount, MaxAmount, MaxAmount, MaxAmount-2)
	if err != nil || quote.AmountFen != MaxAmount-2 || quote.GiftCredits != 0 {
		t.Fatal("int64 intermediate overflow", quote, err)
	}
	for _, bad := range []PackageSnapshot{{PackageID: "bad", OriginalAmountFen: 1, AmountFen: 1, RechargeCredits: MaxAmount, GiftCredits: 1}, {PackageID: "bad", AmountFen: 0, RechargeCredits: 1}, {PackageID: "bad", OriginalAmountFen: 1, AmountFen: 1, RechargeCredits: 0}} {
		if bad.Validate() == nil {
			t.Fatal(bad)
		}
	}
}
func TestDailyLimitsAndBeijingMidnight(t *testing.T) {
	for _, tt := range []struct {
		paid, order, limit int64
		ok                 bool
	}{{0, 1, 1, true}, {1, 1, 1, false}, {499999, 1, 500000, true}, {499999, 2, 500000, false}, {MaxAmount, MaxAmount, 0, true}, {0, 0, 0, false}, {-1, 1, 0, false}} {
		if err := CheckDailyLimit(tt.paid, tt.order, tt.limit); (err == nil) != tt.ok {
			t.Fatal(tt, err)
		}
	}
	midnight := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)
	before, end := BeijingDay(midnight.Add(-time.Nanosecond))
	after, next := BeijingDay(midnight)
	if !end.Equal(midnight) || !after.Equal(midnight) || !before.Equal(midnight.Add(-24*time.Hour)) || !next.Equal(midnight.Add(24*time.Hour)) {
		t.Fatal(before, end, after, next)
	}
}

func TestPackageDualPricesAndInternalName(t *testing.T) {
	pkg := Package{ID: "discount", Name: strings.Repeat("充", 48), OriginalAmountFen: 10000, AmountFen: 8000, RechargeCredits: 1000, GiftCredits: 100}
	if err := pkg.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot := pkg.Snapshot()
	pkg.AmountFen = 7000
	pkg.OriginalAmountFen = 12000
	pkg.Name = "后来改名"
	for _, credits := range []int64{500, 1000} {
		quote, err := QuoteRefund(snapshot, 1000, 100, MaxAmount, credits)
		if err != nil || quote.AmountFen != credits*8 || quote.GiftCredits != credits/10 {
			t.Fatal(quote, err)
		}
	}
	for _, name := range []string{"", " ", "有\n换行", "有\x00控制", strings.Repeat("充", 49), string([]byte{0xff})} {
		invalid := pkg
		invalid.Name = name
		if invalid.Validate() == nil {
			t.Fatalf("invalid name accepted %q", name)
		}
	}
	for _, prices := range [][2]int64{{0, 100}, {101, 100}, {1, MaxAmount + 1}, {1, 0}} {
		invalid := snapshot
		invalid.AmountFen = prices[0]
		invalid.OriginalAmountFen = prices[1]
		if invalid.Validate() == nil {
			t.Fatal("invalid prices", prices)
		}
	}
}
