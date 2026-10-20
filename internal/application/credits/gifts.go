package credits

import (
	"context"
	"time"

	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
)

const GiftTimezone = "Asia/Shanghai"

// GiftPolicy 是运营配置；它不改变任何已受理的赠送。
type GiftPolicy struct {
	Revision         uint64 `json:"revision"`
	RegistrationGift int64  `json:"registrationGift"`
	DailyGift        int64  `json:"dailyGift"`
	Timezone         string `json:"timezone"`
}

type GiftPolicyInput struct {
	Revision         uint64 `json:"revision"`
	RegistrationGift int64  `json:"registrationGift"`
	DailyGift        int64  `json:"dailyGift"`
}

type DailyGift struct {
	Date           string `json:"date"`
	Amount         int64  `json:"amount"`
	Claimed        bool   `json:"claimed"`
	AlreadyClaimed bool   `json:"alreadyClaimed"`
	// PG资格时刻至下一业务午夜的预算；缓存加载耗时须从中扣除，不进API回执。
	CacheTTL time.Duration `json:"-"`
}

// GiftStore 在同一 PG 事务内协调领取资格与账本；该端口归应用层，不属于独立金额核心。
type GiftStore interface {
	CreditGiftPolicy(context.Context) (GiftPolicy, error)
	SaveCreditGiftPolicy(context.Context, GiftPolicyInput) (GiftPolicy, error)
	ClaimDailyCreditGift(context.Context, string) (DailyGift, error)
}

// DailyGiftCache 只保存已提交的正数资格，未命中才执行原领取事务。
type DailyGiftCache interface {
	ReadDailyGift(context.Context, string, func(context.Context) (DailyGift, error)) (DailyGift, error)
}

type Gifts struct {
	Store GiftStore
	Cache DailyGiftCache
}

type GiftGrant struct {
	UserID         string
	Kind           string
	Period         string
	Amount         int64
	PolicyRevision uint64
}

// RegistrationGiftStore 由注册事务提供，避免建用户成功而赠送未提交。
type RegistrationGiftStore interface {
	CreditGiftPolicy(context.Context) (GiftPolicy, error)
	RecordCreditGift(context.Context, GiftGrant) error
}

func GrantRegistrationGift(ctx context.Context, store RegistrationGiftStore, userID string, policy GiftPolicy) error {
	return store.RecordCreditGift(ctx, GiftGrant{UserID: userID, Kind: "registration", Period: "once", Amount: policy.RegistrationGift, PolicyRevision: policy.Revision})
}

func (s *Gifts) Policy(ctx context.Context, actor ports.Actor) (GiftPolicy, error) {
	if err := actor.RequireOperator(); err != nil {
		return GiftPolicy{}, err
	}
	return s.Store.CreditGiftPolicy(ctx)
}

func (s *Gifts) Save(ctx context.Context, actor ports.Actor, in GiftPolicyInput) (GiftPolicy, error) {
	if err := actor.RequireOperator(); err != nil {
		return GiftPolicy{}, err
	}
	if in.Revision >= uint64(d.MaxAmount) || in.RegistrationGift < 0 || in.RegistrationGift > d.MaxAmount || in.DailyGift < 0 || in.DailyGift > d.MaxAmount {
		return GiftPolicy{}, fault.New("CREDIT_POLICY_INVALID", "赠送额度须为安全范围内的非负整数")
	}
	return s.Store.SaveCreditGiftPolicy(ctx, in)
}

func (s *Gifts) Daily(ctx context.Context, actor ports.Actor) (DailyGift, error) {
	if err := actor.Require(); err != nil {
		return DailyGift{}, err
	}
	if s.Cache == nil {
		return DailyGift{}, fault.New("RUNTIME_UNAVAILABLE", "每日赠送缓存尚未配置")
	}
	return s.Cache.ReadDailyGift(ctx, actor.UserID, func(ctx context.Context) (DailyGift, error) {
		return s.Store.ClaimDailyCreditGift(ctx, actor.UserID)
	})
}

// GiftDate 与 PG 共用固定的服务端日历；不依赖宿主机本地时区。
func GiftDate(at time.Time) string {
	return at.In(time.FixedZone(GiftTimezone, 8*60*60)).Format("2006-01-02")
}
