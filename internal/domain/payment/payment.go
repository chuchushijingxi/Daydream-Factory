// Package payment owns payment order rules and integer-only money arithmetic.
package payment

import (
	"crypto/rand"
	"drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"encoding/base32"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxAmount = credits.MaxAmount
const OrderLifetime = 15 * time.Minute
const AgreementVersion = "2026-10-20"
const DefaultDailyLimitFen int64 = 500000

var beijing = time.FixedZone("Asia/Shanghai", 8*60*60)

type Status string

const (
	Pending  Status = "pending"
	Credited Status = "credited"
	Closed   Status = "closed"
	Refunded Status = "refunded"
)

func (s Status) Valid() bool { return s == Pending || s == Credited || s == Closed || s == Refunded }

// Transition 只允许已确认的前进路径；重复事实由应用层在事务中幂等回读。
func (s Status) Transition(next Status) (Status, error) {
	if s == Pending && (next == Credited || next == Closed) || s == Credited && next == Refunded {
		return next, nil
	}
	return s, fault.New("PAYMENT_CONFLICT", "订单状态不允许此操作")
}

type PackageSnapshot struct {
	PackageID         string `json:"packageId"`
	OriginalAmountFen int64  `json:"originalAmountFen"`
	AmountFen         int64  `json:"amountFen"`
	RechargeCredits   int64  `json:"rechargeCredits"`
	GiftCredits       int64  `json:"giftCredits"`
}

func (p PackageSnapshot) Validate() error {
	if p.PackageID == "" || len(p.PackageID) > 128 || p.AmountFen <= 0 || p.AmountFen > p.OriginalAmountFen || p.OriginalAmountFen > MaxAmount || p.RechargeCredits <= 0 || p.RechargeCredits > MaxAmount || p.GiftCredits < 0 || p.GiftCredits > MaxAmount-p.RechargeCredits {
		return fault.New("PAYMENT_PACKAGE_INVALID", "充值档位无效")
	}
	return nil
}

type Package struct {
	Name              string    `json:"name"`
	OriginalAmountFen int64     `json:"originalAmountFen"`
	ID                string    `json:"id"`
	Revision          uint64    `json:"revision"`
	AmountFen         int64     `json:"amountFen"`
	RechargeCredits   int64     `json:"rechargeCredits"`
	GiftCredits       int64     `json:"giftCredits"`
	Enabled           bool      `json:"enabled"`
	Sort              int       `json:"sort"`
	TestOnly          bool      `json:"testOnly"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func (p Package) Snapshot() PackageSnapshot {
	return PackageSnapshot{PackageID: p.ID, OriginalAmountFen: p.OriginalAmountFen, AmountFen: p.AmountFen, RechargeCredits: p.RechargeCredits, GiftCredits: p.GiftCredits}
}
func (p Package) Validate() error {
	if !utf8.ValidString(p.Name) || strings.TrimSpace(p.Name) != p.Name || utf8.RuneCountInString(p.Name) < 1 || utf8.RuneCountInString(p.Name) > 48 || strings.ContainsFunc(p.Name, unicode.IsControl) {
		return fault.New("PAYMENT_PACKAGE_INVALID", "档位名称须为1至48个字符且不含控制字符")
	}
	if p.Revision > uint64(MaxAmount) || int64(p.Sort) < -2147483648 || int64(p.Sort) > 2147483647 {
		return fault.New("PAYMENT_PACKAGE_INVALID", "充值档位版本或排序无效")
	}
	return p.Snapshot().Validate()
}

type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (t Target) Valid() bool {
	return (t.Kind == "personal" || t.Kind == "team") && t.ID != "" && len(t.ID) <= 128
}

type Order struct {
	OrderNo         string `json:"orderNo"`
	BuyerUserID     string `json:"buyerUserId"`
	ClientRequestID string `json:"clientRequestId"`
	Target          Target `json:"target"`
	CreditAccountID string `json:"-"`
	PackageSnapshot
	Channel          string     `json:"channel"`
	CodeURL          string     `json:"codeUrl"`
	ChannelTradeNo   string     `json:"channelTradeNo"`
	Status           Status     `json:"status"`
	CloseReason      string     `json:"closeReason,omitempty"`
	AgreementVersion string     `json:"agreementVersion"`
	ExpiresAt        time.Time  `json:"expiresAt"`
	NextQueryAt      time.Time  `json:"-"`
	QueryAttempts    int        `json:"-"`
	CreatedAt        time.Time  `json:"createdAt"`
	CreditedAt       *time.Time `json:"creditedAt"`
	PaidAt           *time.Time `json:"paidAt"`
	ClosedAt         *time.Time `json:"closedAt"`
	RefundedAt       *time.Time `json:"refundedAt"`
}

func GenerateOrderNo(at time.Time) (string, error) {
	if at.In(beijing).Year() < 1 || at.In(beijing).Year() > 9999 {
		return "", fault.ErrInvalid
	}
	var entropy [7]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate payment order number: %w", err)
	}
	suffix := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(entropy[:])[:10]
	return "PO" + at.In(beijing).Format("20060102150405") + suffix, nil
}
func ValidOrderNo(no string) bool {
	if len(no) != 26 || !strings.HasPrefix(no, "PO") {
		return false
	}
	at, err := time.ParseInLocation("20060102150405", no[2:16], beijing)
	if err != nil || at.Year() < 1 || at.Format("20060102150405") != no[2:16] {
		return false
	}
	for _, c := range no[16:] {
		if !(c >= 'A' && c <= 'Z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

// FormatAmount 与 ParseAmount 不经过浮点，渠道金额统一为人民币两位小数。
func FormatAmount(fen int64) (string, error) {
	if fen < 0 || fen > MaxAmount {
		return "", fault.New("PAYMENT_INVALID_AMOUNT", "支付金额无效")
	}
	return fmt.Sprintf("%d.%02d", fen/100, fen%100), nil
}
func ParseAmount(yuan string) (int64, error) {
	invalid := func() (int64, error) { return 0, fault.New("PAYMENT_INVALID_AMOUNT", "支付金额须为两位小数") }
	parts := strings.Split(yuan, ".")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) != 2 {
		return invalid()
	}
	for _, c := range parts[0] + parts[1] {
		if c < '0' || c > '9' {
			return invalid()
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > MaxAmount/100 {
		return invalid()
	}
	fraction, _ := strconv.ParseInt(parts[1], 10, 64)
	if whole == MaxAmount/100 && fraction > MaxAmount%100 {
		return invalid()
	}
	return whole*100 + fraction, nil
}

type RefundQuote struct {
	MaxCredits      int64 `json:"maxCredits"`
	RechargeCredits int64 `json:"rechargeCredits"`
	GiftCredits     int64 `json:"giftCredits"`
	AmountFen       int64 `json:"amountFen"`
}

// QuoteRefund 由调用方提供同一快照内的钱包及团队未分配上限；个人上限传 MaxAmount。
// 乘积使用整数大数，合法安全整数的乘积也可能超过 int64。
func QuoteRefund(p PackageSnapshot, rechargeBalance, permanentGiftBalance, refundableLimit, requestedCredits int64) (RefundQuote, error) {
	if err := p.Validate(); err != nil {
		return RefundQuote{}, err
	}
	for _, v := range []int64{rechargeBalance, permanentGiftBalance, refundableLimit} {
		if v < 0 || v > MaxAmount {
			return RefundQuote{}, fault.ErrInvalid
		}
	}
	maximum := min(p.RechargeCredits, rechargeBalance, refundableLimit)
	quote := RefundQuote{MaxCredits: maximum}
	if requestedCredits <= 0 || requestedCredits > maximum {
		return quote, fault.New("PAYMENT_REFUND_EXCEEDED", "退款积分超过可退上限")
	}
	quote.RechargeCredits = requestedCredits
	quote.AmountFen = proportion(p.AmountFen, requestedCredits, p.RechargeCredits)
	quote.GiftCredits = min(proportion(p.GiftCredits, requestedCredits, p.RechargeCredits), permanentGiftBalance)
	return quote, nil
}
func proportion(amount, numerator, denominator int64) int64 {
	n := new(big.Int).Mul(big.NewInt(amount), big.NewInt(numerator))
	return n.Quo(n, big.NewInt(denominator)).Int64()
}

func CheckDailyLimit(paidFen, orderFen, limitFen int64) error {
	if paidFen < 0 || paidFen > MaxAmount || orderFen <= 0 || orderFen > MaxAmount || limitFen < 0 || limitFen > MaxAmount {
		return fault.New("PAYMENT_INVALID_AMOUNT", "支付金额无效")
	}
	if limitFen > 0 && (paidFen > limitFen || orderFen > limitFen-paidFen) {
		return fault.New("PAYMENT_DAILY_LIMIT", "超过单日充值上限，请联系客服")
	}
	return nil
}

// BeijingDay 冻结服务端接受时刻所属日的半开区间，SQL 求和不依赖连接时区。
func BeijingDay(at time.Time) (time.Time, time.Time) {
	local := at.In(beijing)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, beijing)
	return start.UTC(), start.Add(24 * time.Hour).UTC()
}
