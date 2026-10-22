package credits

import (
	"drama-agent/internal/domain/fault"
	"math/big"
)

// PriceDiscount 对完整有理价格乘折扣后只取整一次，语言计价不能先取整原价再打折。
func PriceDiscount(quantity, unitPrice, divisor int64, percent int) (int64, error) {
	if quantity <= 0 || unitPrice < 0 || unitPrice > MaxAmount || divisor <= 0 || percent < 1 || percent > 100 {
		return 0, fault.ErrInvalid
	}
	n := new(big.Int).Mul(big.NewInt(quantity), big.NewInt(unitPrice))
	n.Mul(n, big.NewInt(int64(percent)))
	denominator := new(big.Int).Mul(big.NewInt(divisor), big.NewInt(100))
	q, remainder := new(big.Int), new(big.Int)
	q.QuoRem(n, denominator, remainder)
	if remainder.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() || q.Int64() > MaxAmount {
		return 0, fault.New("CREDIT_INVALID_AMOUNT", "计费金额溢出")
	}
	return q.Int64(), nil
}
