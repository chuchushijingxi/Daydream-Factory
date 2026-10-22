package ports

import (
	"context"
	d "drama-agent/internal/domain/credits"
	md "drama-agent/internal/domain/modelconfig"
)

// CreditLedger 独立于认证、业务资源、供应方与支付协议。
type CreditLedger interface {
	Add(context.Context, d.Change) (d.Entry, error)
	Deduct(context.Context, d.Change) (d.Entry, error)
	Refund(context.Context, d.Change) (d.Entry, error)
	Balance(context.Context, string) (int64, error)
	CreditOperation(context.Context, string) (d.Entry, error)
	// CreditOperations 批读已冻结编号的原账目；调用方仍核对 Entry.Same/冻结账户与持有人，不从任务状态猜金额。
	CreditOperations(context.Context, []string) (map[string]d.Entry, error)
	CreditEntries(context.Context, string, int, int) ([]d.Entry, int64, error)
}

type CreditPrices interface {
	CreditPrice(context.Context, string) (d.PriceRule, uint64, string, error)
}

type MediaPricing struct {
	Rule                   d.PriceRule
	Version                uint64
	ConnectionID           string
	Profile                *md.MediaProfile
	PriceDefaultResolution string
}
type MediaPrices interface {
	MediaPrice(context.Context, string) (MediaPricing, error)
}

// CreditQueries 只投影账本与既有完成事实；独立成口避免业务计费消费者依赖展示层。
type CreditQueries interface {
	CreditWallet(context.Context, string) (d.Wallet, error)
	CreditHistory(context.Context, string, d.HistoryFilter) (d.History, error)
	RecentCreditEntries(context.Context, string) ([]d.Entry, error)
}
type CreditRecoveryProjects interface {
	UnsettledCreditProjectIDs(context.Context) ([]string, error)
}
type MediaCreditRecovery struct {
	SpaceID, ProjectID, ChargeID string
	AccountID, HolderID, ActorID string
	Amount                       int64
}
type CreditRecoveryMedia interface {
	UnsettledMediaCredits(context.Context) ([]MediaCreditRecovery, error)
}

// CreditUpdates 只携带失效提示，不含余额或业务内容；通道关闭表示源故障，应重连并重读权威 GET。
type CreditUpdates interface {
	WatchCreditAccount(context.Context, string) (<-chan struct{}, func(), error)
}

// TeamUpdates 只携带团队成员、邀请与加入申请的失效提示；收到后重读权威 GET。
type TeamUpdates interface {
	WatchTeam(context.Context, string) (<-chan struct{}, func(), error)
}
