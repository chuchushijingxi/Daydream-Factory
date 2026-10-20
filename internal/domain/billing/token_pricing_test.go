package billing

import (
	"testing"
)

func TestDecimalConnectionPricingRoundsUpAndGuardsOverflow(t *testing.T) {
	price := TokenPricing{Input: .25, Output: .5, CacheHitHundredths: 1}
	if !price.Valid() || price.Reserve(3, 3) != 3 || price.AffordableOutput(2, 100) != 4 {
		t.Fatal("decimal reserve/output limit incorrect")
	}
	cost, ok := price.CheckedCost(Usage{Input: 10, Output: 3, CacheHit: 2, Known: true})
	if !ok || cost != 4 {
		t.Fatal(cost, ok)
	}
	if _, ok = price.CheckedCost(Usage{Input: 1, CacheHit: 2}); ok {
		t.Fatal("invalid usage accepted")
	}
	expensive := TokenPricing{Input: 100000, Output: 100000}
	if _, ok = expensive.CheckedCost(Usage{Input: int(^uint(0) >> 1), Output: 1}); ok {
		t.Fatal("overflow became known or negative cost")
	}
}
