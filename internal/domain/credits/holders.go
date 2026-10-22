package credits

import (
	"drama-agent/internal/domain/fault"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type HolderAllocation struct {
	AccountID string `json:"accountId"`
	HolderID  string `json:"holderId"`
	Balance   int64  `json:"balance"`
	Open      bool   `json:"open"`
}
type AllocationChange struct {
	AccountID, HolderID, OperationID, ActorID string
	Amount                                    int64
	All                                       bool
}
type AllocationEvent struct {
	ID              string    `json:"id"`
	OperationID     string    `json:"operationId"`
	AccountID       string    `json:"accountId"`
	HolderID        string    `json:"holderId"`
	Kind            string    `json:"kind"`
	Amount          int64     `json:"amount"`
	Balance         int64     `json:"balance"`
	ActorID         string    `json:"actorId"`
	CreatedAt       time.Time `json:"createdAt"`
	RequestedAmount int64     `json:"-"`
	Replayed        bool      `json:"-"`
	All             bool      `json:"-"`
}
type Balances struct {
	Wallet    Wallet `json:"wallet"`
	Allocated int64  `json:"allocated"`
}

func Unallocated(balance, allocated int64) (int64, error) {
	if balance < 0 || balance > MaxAmount || allocated < 0 || allocated > balance {
		return 0, fault.New("CREDIT_ALLOCATION_INVARIANT", "积分分配与账户余额不一致")
	}
	return balance - allocated, nil
}
func (b Balances) Unallocated() (int64, error) { return Unallocated(b.Wallet.Balance, b.Allocated) }
func (c AllocationChange) Validate(kind string) error {
	for _, v := range []string{c.AccountID, c.HolderID, c.OperationID, c.ActorID} {
		if v == "" || len(v) > 256 || !utf8.ValidString(v) || strings.TrimSpace(v) != v || strings.ContainsFunc(v, unicode.IsControl) {
			return fault.ErrInvalid
		}
	}
	switch kind {
	case "allocate":
		if c.All {
			return fault.ErrInvalid
		}
		return ValidAmount(c.Amount)
	case "reclaim":
		if c.All {
			if c.Amount != 0 {
				return fault.ErrInvalid
			}
			return nil
		}
		return ValidAmount(c.Amount)
	case "release":
		if c.Amount != 0 || c.All {
			return fault.ErrInvalid
		}
		return nil
	default:
		return fault.ErrInvalid
	}
}
func (e AllocationEvent) Same(kind string, c AllocationChange) bool {
	return e.Kind == kind && e.AccountID == c.AccountID && e.HolderID == c.HolderID && e.OperationID == c.OperationID && e.ActorID == c.ActorID && e.RequestedAmount == c.Amount && e.All == c.All
}
func (h HolderAllocation) valid() bool {
	return h.Balance >= 0 && h.Balance <= MaxAmount && (h.Open || h.Balance == 0)
}
func (h HolderAllocation) Allocate(unallocated, amount int64) (HolderAllocation, error) {
	if !h.valid() || unallocated < 0 || unallocated > MaxAmount {
		return HolderAllocation{}, fault.ErrInvalid
	}
	if err := ValidAmount(amount); err != nil {
		return HolderAllocation{}, err
	}
	if amount > unallocated {
		return HolderAllocation{}, fault.New("CREDIT_UNALLOCATED_INSUFFICIENT", "账户未分配积分不足")
	}
	if amount > MaxAmount-h.Balance {
		return HolderAllocation{}, fault.ErrInvalid
	}
	h.Open = true
	h.Balance += amount
	return h, nil
}
func (h HolderAllocation) Reclaim(amount int64, all bool) (HolderAllocation, int64, error) {
	if !h.valid() {
		return HolderAllocation{}, 0, fault.ErrInvalid
	}
	if all {
		if amount != 0 {
			return HolderAllocation{}, 0, fault.ErrInvalid
		}
		amount = h.Balance
	} else if err := ValidAmount(amount); err != nil {
		return HolderAllocation{}, 0, err
	}
	if amount > h.Balance {
		return HolderAllocation{}, 0, fault.New("CREDIT_HOLDER_INSUFFICIENT", "持有人可用积分不足")
	}
	h.Balance -= amount
	return h, amount, nil
}
func (h HolderAllocation) Release() (HolderAllocation, int64, error) {
	if !h.valid() {
		return HolderAllocation{}, 0, fault.ErrInvalid
	}
	amount := h.Balance
	h.Balance = 0
	h.Open = false
	return h, amount, nil
}
func (h HolderAllocation) Deduct(amount int64) (HolderAllocation, error) {
	if !h.valid() {
		return HolderAllocation{}, fault.ErrInvalid
	}
	if err := ValidAmount(amount); err != nil {
		return HolderAllocation{}, err
	}
	if !h.Open || h.Balance < amount {
		return HolderAllocation{}, fault.New("CREDIT_HOLDER_INSUFFICIENT", "持有人可用积分不足")
	}
	h.Balance -= amount
	return h, nil
}
func (h HolderAllocation) Refund(amount int64) (HolderAllocation, error) {
	if !h.valid() {
		return HolderAllocation{}, fault.ErrInvalid
	}
	if err := ValidAmount(amount); err != nil {
		return HolderAllocation{}, err
	}
	if !h.Open {
		return h, nil
	}
	if amount > MaxAmount-h.Balance {
		return HolderAllocation{}, fault.ErrInvalid
	}
	h.Balance += amount
	return h, nil
}
