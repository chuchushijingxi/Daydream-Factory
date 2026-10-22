// Package credits owns integer wallets, source allocation and immutable ledger values.
package credits

import (
	"drama-agent/internal/domain/fault"
	"math"
	"time"
	"unicode"
)

// MaxAmount 与 ports.MaxSafeInteger 同值（2^53-1）；领域不能依赖应用端口，
// 故在本包独立声明同一上界，改动时两处必须同步。
const MaxAmount int64 = 9007199254740991

const (
	Recharge = "recharge"
	Gift     = "gift"
	Legacy   = "legacy"
	Mixed    = "mixed"
)

// Wallet 保存余额及其记录在案的来源构成；legacy 仅为记录来源之前已存在的余额保留。
type Wallet struct {
	Balance         int64 `json:"balance"`
	RechargeBalance int64 `json:"rechargeBalance"`
	GiftBalance     int64 `json:"giftBalance"`
	LegacyBalance   int64 `json:"legacyBalance"`
	// 每日余额是 GiftBalance 的子集，不是第三种赠送账户；期限不外露为可写参数。
	DailyBalance   int64      `json:"-"`
	DailyExpiresAt *time.Time `json:"-"`
}

func ValidAmount(v int64) error {
	if v <= 0 || v > MaxAmount {
		return fault.New("CREDIT_INVALID_AMOUNT", "积分须为安全范围内的正整数")
	}
	return nil
}

type Entry struct {
	ID                  string     `json:"id"`
	OperationID         string     `json:"operationId"`
	AccountID           string     `json:"accountId"`
	HolderID            string     `json:"holderId,omitempty"`
	Kind                string     `json:"kind"`
	Amount              int64      `json:"amount"`
	Balance             int64      `json:"balance"`
	Category            string     `json:"category"`
	RechargeAmount      int64      `json:"rechargeAmount"`
	GiftAmount          int64      `json:"giftAmount"`
	LegacyAmount        int64      `json:"legacyAmount"`
	Source              string     `json:"source"`
	Subject             string     `json:"subject"`
	Reason              string     `json:"reason"`
	OriginalDeductionID string     `json:"originalDeductionId,omitempty"`
	OriginalAdditionID  string     `json:"originalAdditionId,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	DailyAmount         int64      `json:"-"`
	DailyExpiresAt      *time.Time `json:"-"`
}
type Change struct {
	AccountID, HolderID                  string
	Amount                               int64
	OperationID, Source, Subject, Reason string
	OriginalDeductionID                  string
	OriginalAdditionID                   string
	Category                             string
	DailyExpiresAt                       *time.Time
}

func (c Change) Validate(kind string) error {
	if kind != "add" && kind != "deduct" && kind != "refund" && kind != "reclaim" {
		return fault.ErrInvalid
	}
	if c.DailyExpiresAt != nil && (kind != "add" || c.Category != Gift || c.DailyExpiresAt.IsZero()) {
		return fault.ErrInvalid
	}
	if len(c.HolderID) > 128 || (kind == "add" && c.HolderID != "") {
		return fault.ErrInvalid
	}
	if kind == "add" {
		if c.Category != Recharge && c.Category != Gift {
			return fault.New("CREDIT_INVALID_CATEGORY", "发放积分须指定充值或赠送分类")
		}
	} else if c.Category != "" {
		return fault.New("CREDIT_INVALID_CATEGORY", "扣减、返还及扣回分类由原账务事实确定")
	}
	if e := ValidAmount(c.Amount); e != nil {
		return e
	}
	for _, v := range []string{c.OperationID, c.Subject, c.Reason} {
		if v == "" || len(v) > 1024 {
			return fault.ErrInvalid
		}
	}
	if kind == "reclaim" {
		if c.OriginalAdditionID == "" || c.OriginalDeductionID != "" || c.AccountID != "" || c.Source != "" {
			return fault.ErrInvalid
		}
	} else if c.OriginalAdditionID != "" {
		return fault.ErrInvalid
	} else if kind == "refund" {
		if c.OriginalDeductionID == "" || c.AccountID != "" || c.Source != "" {
			return fault.ErrInvalid
		}
	} else if c.AccountID == "" || c.Source == "" || c.OriginalDeductionID != "" {
		return fault.ErrInvalid
	}
	return nil
}

// Same 判定既有账目条目与本次变更完全一致，用于幂等写入的回执核对；
// 返还及扣回不比对归属与分类（由原账务事实确定），发放必须带分类。
func (e Entry) Same(kind string, c Change) bool {
	if e.Kind != kind || e.Amount != c.Amount || e.OperationID != c.OperationID || e.Subject != c.Subject || e.Reason != c.Reason || e.OriginalDeductionID != c.OriginalDeductionID || e.OriginalAdditionID != c.OriginalAdditionID {
		return false
	}
	if kind == "refund" || kind == "reclaim" {
		return e.HolderID == c.HolderID
	}
	if kind == "add" && e.Category != c.Category {
		return false
	}
	if kind == "add" && !sameExpiry(e.DailyExpiresAt, c.DailyExpiresAt) {
		return false
	}
	return e.AccountID == c.AccountID && e.HolderID == c.HolderID && e.Source == c.Source
}
func Count(text string) int64 {
	var n int64
	for _, r := range text {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// Price 对整个目标一次性向上取整，中间乘积不溢出。
func Price(quantity, unitPrice, divisor int64) (int64, error) {
	if quantity <= 0 || unitPrice < 0 || unitPrice > MaxAmount || divisor <= 0 {
		return 0, fault.ErrInvalid
	}
	if unitPrice == 0 {
		return 0, nil
	}
	if quantity > math.MaxInt64/unitPrice {
		return 0, fault.New("CREDIT_INVALID_AMOUNT", "计费运算溢出")
	}
	n := quantity * unitPrice
	q := n / divisor
	if n%divisor != 0 {
		q++
	}
	if q > MaxAmount {
		return 0, fault.New("CREDIT_INVALID_AMOUNT", "计费金额溢出")
	}
	return q, nil
}

type PriceRule struct {
	ResolutionPrices map[string]int64 `json:"resolutionPrices,omitempty"`
	UnitPrice        int64            `json:"unitPrice"`
	FreeEnabled      bool             `json:"freeEnabled"`
	FreeStart        *time.Time       `json:"freeStart"`
	FreeEnd          *time.Time       `json:"freeEnd"`
}

func (r PriceRule) Valid() bool {
	if len(r.ResolutionPrices) > 32 {
		return false
	}
	for key, price := range r.ResolutionPrices {
		if key == "" || len(key) > 64 || price < 0 || price > MaxAmount {
			return false
		}
	}
	return r.UnitPrice >= 0 && r.UnitPrice <= MaxAmount && (!r.FreeEnabled || r.FreeStart != nil && r.FreeEnd != nil && r.FreeStart.Before(*r.FreeEnd))
}

// AtResolution 冻结所选档位的单价，不把整张价目表复制进计费依据。
// UnitPrice 是默认档位价；其他档位必须有显式条目，缺价永远不会静默变成免费。
func (r PriceRule) AtResolution(resolution, defaultResolution string) (PriceRule, bool) {
	price, ok := r.ResolutionPrices[resolution]
	if !ok {
		if resolution != defaultResolution {
			return PriceRule{}, false
		}
		price = r.UnitPrice
	}
	r.UnitPrice = price
	r.ResolutionPrices = nil
	return r, r.Valid()
}

func (r PriceRule) Free(at time.Time) bool {
	return r.FreeEnabled && r.FreeStart != nil && r.FreeEnd != nil && !at.Before(*r.FreeStart) && at.Before(*r.FreeEnd)
}

// Charge 是一次业务启动的不可变计费依据，由其内部多个 Run 共用。
type Charge struct {
	AccountID       string    `json:"accountId"`
	HolderID        string    `json:"holderId,omitempty"`
	ActorID         string    `json:"actorId"`
	DiscountPercent int       `json:"discountPercent"`
	SourceVersion   uint64    `json:"sourceVersion"`
	InputRevision   uint64    `json:"inputRevision"`
	Unit            string    `json:"unit"`
	ID              string    `json:"id"`
	ModelID         string    `json:"modelId"`
	ConnectionID    string    `json:"connectionId"`
	Scope           string    `json:"scope"`
	ScopeDigest     string    `json:"scopeDigest"`
	Quantity        int64     `json:"quantity"`
	PriceVersion    uint64    `json:"priceVersion"`
	Rule            PriceRule `json:"rule"`
	AcceptedAt      time.Time `json:"acceptedAt"`
	OriginalAmount  int64     `json:"originalAmount"`
	Amount          int64     `json:"amount"`
	Free            bool      `json:"free"`
}
