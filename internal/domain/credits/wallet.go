package credits

import (
	"drama-agent/internal/domain/fault"
	"time"
)

// Allocation 是一次变更的来源份额；历史未分类只沿已有余额和原扣款保留。
type Allocation struct {
	Recharge       int64
	Gift           int64
	Legacy         int64
	Daily          int64 // Gift 内的每日份额，不能再次计入 Total。
	DailyExpiresAt *time.Time
}

func (a Allocation) Total() int64 { return a.Recharge + a.Gift + a.Legacy }

func (a Allocation) Category() string {
	switch a.Total() {
	case a.Recharge:
		return Recharge
	case a.Gift:
		return Gift
	case a.Legacy:
		return Legacy
	default:
		return Mixed
	}
}

func Funding(amount int64, category string) (Allocation, error) {
	if err := ValidAmount(amount); err != nil {
		return Allocation{}, err
	}
	switch category {
	case Recharge:
		return Allocation{Recharge: amount}, nil
	case Gift:
		return Allocation{Gift: amount}, nil
	default:
		return Allocation{}, fault.New("CREDIT_INVALID_CATEGORY", "发放积分须指定充值或赠送分类")
	}
}

func (w Wallet) Deduct(amount int64) (Wallet, Allocation, error) {
	if err := ValidAmount(amount); err != nil {
		return Wallet{}, Allocation{}, err
	}
	if amount > w.Balance {
		return Wallet{}, Allocation{}, fault.New("CREDIT_INSUFFICIENT", "积分余额不足")
	}
	left := amount
	a := Allocation{Gift: min(left, w.GiftBalance)}
	a.Daily = min(a.Gift, w.DailyBalance)
	if a.Daily > 0 {
		a.DailyExpiresAt = w.DailyExpiresAt
	}
	left -= a.Gift
	a.Recharge = min(left, w.RechargeBalance)
	left -= a.Recharge
	a.Legacy = min(left, w.LegacyBalance)
	if left-a.Legacy != 0 {
		return Wallet{}, Allocation{}, fault.ErrInvalid
	}
	w.Balance -= amount
	w.RechargeBalance -= a.Recharge
	w.GiftBalance -= a.Gift
	w.DailyBalance -= a.Daily
	if w.DailyBalance == 0 {
		w.DailyExpiresAt = nil
	}
	w.LegacyBalance -= a.Legacy
	return w, a, nil
}

func (w Wallet) Credit(a Allocation) (Wallet, error) {
	if a.Daily < 0 || a.Daily > a.Gift || (a.Daily == 0) != (a.DailyExpiresAt == nil) {
		return Wallet{}, fault.ErrInvalid
	}
	if a.Daily > 0 && w.DailyBalance > 0 && !sameExpiry(w.DailyExpiresAt, a.DailyExpiresAt) {
		return Wallet{}, fault.ErrInvalid
	}
	for _, part := range []int64{a.Recharge, a.Gift, a.Legacy} {
		if part < 0 || part > MaxAmount {
			return Wallet{}, fault.ErrInvalid
		}
	}
	if err := ValidAmount(a.Total()); err != nil {
		return Wallet{}, err
	}
	if a.Total() > MaxAmount-w.Balance {
		return Wallet{}, fault.New("CREDIT_INVALID_AMOUNT", "积分余额溢出")
	}
	w.Balance += a.Total()
	w.RechargeBalance += a.Recharge
	w.GiftBalance += a.Gift
	w.DailyBalance += a.Daily
	if a.Daily > 0 {
		w.DailyExpiresAt = a.DailyExpiresAt
	}
	w.LegacyBalance += a.Legacy
	return w, nil
}

func (original Entry) RefundAllocation(amount int64, returned Allocation) (Allocation, error) {
	if original.Kind != "deduct" {
		return Allocation{}, fault.ErrInvalid
	}
	if err := ValidAmount(amount); err != nil {
		return Allocation{}, err
	}
	if amount > original.Amount-returned.Total() {
		return Allocation{}, fault.New("CREDIT_REFUND_EXCEEDED", "累计返还超过原扣款")
	}
	// 部分返还先恢复充值，再赠送，后历史未分类；完整返还恢复原构成。
	left := amount
	a := Allocation{Recharge: min(left, original.RechargeAmount-returned.Recharge)}
	left -= a.Recharge
	a.Gift = min(left, original.GiftAmount-returned.Gift)
	// 先返永久赠送，再返原每日份额，保留原期限，不根据当前钱包重分。
	permanent := original.GiftAmount - original.DailyAmount - (returned.Gift - returned.Daily)
	a.Daily = max(int64(0), a.Gift-permanent)
	if a.Daily > 0 {
		a.DailyExpiresAt = original.DailyExpiresAt
	}
	left -= a.Gift
	a.Legacy = min(left, original.LegacyAmount-returned.Legacy)
	if left-a.Legacy != 0 {
		return Allocation{}, fault.ErrInvalid
	}
	return a, nil
}

func sameExpiry(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// Expire 只移除尚未预扣的每日份额；进行中的任务不受影响。
func (w Wallet) Expire(at time.Time) (Wallet, Allocation) {
	if w.DailyBalance == 0 || w.DailyExpiresAt == nil || at.Before(*w.DailyExpiresAt) {
		return w, Allocation{}
	}
	a := Allocation{Gift: w.DailyBalance, Daily: w.DailyBalance, DailyExpiresAt: w.DailyExpiresAt}
	w.Balance -= a.Daily
	w.GiftBalance -= a.Daily
	w.DailyBalance, w.DailyExpiresAt = 0, nil
	return w, a
}

// Restore 将返还和原每日份额的到期视为同一原子动作。已过期的返还不进入
// 可用钱包，避免与新一天的积分混合、延期，或让中间金额溢出安全整数。
func (w Wallet) Restore(a Allocation, at time.Time) (Wallet, Allocation, error) {
	expired := Allocation{}
	if a.Daily > 0 && a.DailyExpiresAt != nil && !at.Before(*a.DailyExpiresAt) {
		expired = Allocation{Gift: a.Daily, Daily: a.Daily, DailyExpiresAt: a.DailyExpiresAt}
		a.Gift -= a.Daily
		a.Daily, a.DailyExpiresAt = 0, nil
	}
	if a.Total() == 0 {
		return w, expired, nil
	}
	next, err := w.Credit(a)
	return next, expired, err
}
