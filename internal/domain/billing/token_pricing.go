package billing

import (
	"math"
	"math/big"
)

// Peak CNY / million tokens, verified against official pricing on 2026-10-13.
// Calls retain their rate snapshot; historical costs are never recomputed.
type TokenPricing struct {
	Input              float64 `json:"inputCnyPerMillion"`
	Output             float64 `json:"outputCnyPerMillion"`
	CacheHitHundredths float64 `json:"cacheHitHundredthsCnyPerMillion"`
}

func (p TokenPricing) Valid() bool {
	for _, v := range []float64{p.Input, p.Output, p.CacheHitHundredths / 100} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100000 {
			return false
		}
	}
	return p.Input >= 0.000001 && p.Output >= 0.000001 && p.CacheHitHundredths <= p.Input*100
}

func ceilMicros(n int64) int64 { return (n + 999999) / 1000000 }

func (p TokenPricing) Reserve(input, output int) int64 {
	return ceilMicros(int64(input)*int64(math.Round(p.Input*1000000))) + ceilMicros(int64(output)*int64(math.Round(p.Output*1000000)))
}

func (p TokenPricing) AffordableOutput(remaining int64, maximum int) int {
	lo, hi := 0, maximum
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if p.Reserve(0, mid) <= remaining {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

func (p TokenPricing) CheckedCost(u Usage) (int64, bool) {
	if u.Input < 0 || u.Output < 0 || u.CacheHit < 0 || u.CacheHit > u.Input {
		return 0, false
	}
	total := new(big.Int)
	for _, v := range []struct {
		tokens int
		rate   float64
	}{{u.Input - u.CacheHit, p.Input}, {u.Output, p.Output}, {u.CacheHit, p.CacheHitHundredths / 100}} {
		n := new(big.Int).Mul(big.NewInt(int64(v.tokens)), big.NewInt(int64(math.Round(v.rate*1000000))))
		total.Add(total, n)
	}
	total.Add(total, big.NewInt(999999))
	total.Quo(total, big.NewInt(1000000))
	if !total.IsInt64() {
		return 0, false
	}
	return total.Int64(), true
}

func (p TokenPricing) Cost(u Usage) int64 { v, _ := p.CheckedCost(u); return v }
