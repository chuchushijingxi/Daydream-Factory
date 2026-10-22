package credits

import "testing"

func TestPriceDiscountRoundsWholeRationalPriceExactlyOnce(t *testing.T) {
	for _, test := range []struct {
		quantity, unit, divisor int64
		percent                 int
		want                    int64
	}{{1, 101, 100, 85, 1}, {12345, 2, 100, 85, 210}, {18450, 2, 100, 100, 369}, {1, 3, 1, 85, 3}, {10, 0, 1, 85, 0}, {MaxAmount, MaxAmount, MaxAmount, 100, MaxAmount}} {
		got, err := PriceDiscount(test.quantity, test.unit, test.divisor, test.percent)
		if err != nil || got != test.want {
			t.Fatal(test, got, err)
		}
	}
	rounded, _ := Price(1, 101, 100)
	doubleRounded, _ := PriceDiscount(rounded, 1, 1, 85)
	exact, _ := PriceDiscount(1, 101, 100, 85)
	if doubleRounded != 2 || exact != 1 {
		t.Fatal("fixture does not expose double-rounding risk", doubleRounded, exact)
	}
	for _, test := range []struct {
		q, u, div int64
		p         int
	}{{0, 1, 1, 85}, {1, 1, 0, 85}, {1, -1, 1, 85}, {1, 1, 1, 0}, {1, 1, 1, 101}, {MaxAmount, MaxAmount, 1, 100}} {
		if _, err := PriceDiscount(test.q, test.u, test.div, test.p); err == nil {
			t.Fatal("invalid or overflowing price accepted", test)
		}
	}
}
