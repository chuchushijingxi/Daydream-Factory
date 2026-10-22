package ports

import (
	"context"
	c "drama-agent/internal/domain/credits"
	p "drama-agent/internal/domain/payment"
	"time"
)

// PaymentChannel 仅运输已规范化事实；签名材料、密钥及原报文不进入支付应用。
type PaymentChannel interface {
	ID() string
	Create(context.Context, ChannelOrder) (string, error)
	Query(context.Context, string) (ChannelTrade, error)
	Close(context.Context, string) error
	Refund(context.Context, ChannelRefund) (ChannelRefundResult, error)
	// QueryRefund 固定先传退款号，再传原订单号，所有渠道与恢复调用共用此顺序。
	QueryRefund(ctx context.Context, refundNo, orderNo string) (ChannelRefundResult, error)
	ParseNotify(context.Context, NotifyInput) (PaymentNotify, NotifyAck, error)
	Bill(context.Context, time.Time) ([]BillRow, error)
}
type ChannelOrder struct {
	OrderNo, Description string
	AmountFen            int64
	ExpiresAt            time.Time
}

// ChannelTrade.State: paid | notpay | closed | failed；未知状态必须返回协议错误。
type ChannelTrade struct {
	OrderNo, TradeNo, State string
	AmountFen               int64
	PaidAt                  *time.Time
}
type ChannelRefund struct {
	RefundNo, OrderNo, TradeNo, Reason string
	AmountFen, TotalFen                int64
}

// ChannelRefundResult.State: pending | succeeded | failed | not_found。
type ChannelRefundResult struct {
	BillAt                                             *time.Time // 渠道已确认的账单归属时刻，不是本地完成时间。
	RefundNo, OrderNo, TradeNo, ChannelRefundID, State string
	AmountFen, TotalFen                                int64
}
type NotifyInput struct {
	Header map[string]string
	Body   []byte
}
type PaymentNotify struct {
	Kind   string
	Trade  ChannelTrade
	Refund ChannelRefundResult
}
type NotifyAck struct {
	Status            int
	ContentType, Body string
}
type BillRow struct {
	ForeignApplication                      bool // 同商户其他应用交易，只记差异，不补积分。
	OrderNo, TradeNo, Kind, RefundNo, State string
	AmountFen                               int64
}

// RechargeTargets 不暴露团队内部结构；由团队空间提供个人与本人所有团队的目标。
type RechargeTargets interface {
	List(context.Context, Actor) ([]p.Target, error)
	Resolve(context.Context, Actor, p.Target) (ResolvedRechargeTarget, error)
}
type ResolvedRechargeTarget struct {
	Target    p.Target
	AccountID string
}

type PaymentOrderFilter struct {
	Page, PageSize                                                       int
	BuyerUserID, OrderNo, TradeNo, TargetKind, TargetID, Channel, Status string
	Start, End                                                           *time.Time
}
type PaymentAnomalyFilter struct {
	Page, PageSize        int
	Status, Kind, Channel string
}
type PaymentBillClaim struct {
	Date     time.Time
	Attempts int
}
type PaymentConfirmation struct {
	OrderNo, Channel, Source string
	Trade                    ChannelTrade
}
type PaymentOrderDetails struct {
	Order   p.Order    `json:"order"`
	Events  []p.Event  `json:"events"`
	Refunds []p.Refund `json:"refunds"`
	Credits []c.Entry  `json:"credits"`
}

// PaymentStore 的原子写动作不等待渠道HTTP；账务与支付状态共用同一PG事务。
type PaymentStore interface {
	PaymentPackages(context.Context) ([]p.Package, error)
	SavePaymentPackage(context.Context, p.Package, string) (p.Package, error)
	PaymentSettings(context.Context) (p.Settings, error)
	SavePaymentSettings(context.Context, p.Settings, string) (p.Settings, error)
	PaymentAgreement(context.Context, string, string) (bool, error)
	PaymentOrderByRequest(context.Context, string, string) (p.Order, error)
	PendingPaymentOrder(context.Context, string) (p.Order, error)
	CreatePaymentOrder(context.Context, p.Order) (p.Order, bool, error)
	PaymentOrder(context.Context, string) (p.Order, error)
	PaymentOrders(context.Context, PaymentOrderFilter) (Page[p.Order], error)
	PaymentOrderDetails(context.Context, string) (PaymentOrderDetails, error)
	SetPaymentOrderCode(context.Context, string, string, string) (p.Order, error)
	RecordPaymentEvent(context.Context, p.Event) error
	CreditPaymentOrder(context.Context, PaymentConfirmation) (p.Order, error)
	ConfirmedPaymentCredit(context.Context, string) (p.Order, bool, error)
	ClosePaymentOrder(context.Context, string, string, string) (p.Order, error)
	ClaimPaymentOrders(context.Context, time.Time, int, time.Duration) ([]p.Order, error)
	SchedulePaymentOrder(context.Context, string, int, time.Time) error
	PaymentRefundQuote(context.Context, string, int64) (p.RefundQuote, error)
	CreatePaymentRefund(context.Context, p.RefundRequest) (p.Refund, error)
	// PaymentRefund按渠道退款号读取；请求编号使用单独入口，避免OR双身份歧义。
	PaymentRefund(context.Context, string) (p.Refund, error)
	PaymentRefundByRequest(context.Context, string) (p.Refund, error)
	FinishPaymentRefund(context.Context, string, ChannelRefundResult, string) (p.Refund, error)
	ClaimPaymentRefunds(context.Context, time.Time, int, time.Duration) ([]p.Refund, error)
	RecordPaymentAnomaly(context.Context, p.Anomaly) error
	PaymentAnomalies(context.Context, PaymentAnomalyFilter) (Page[p.Anomaly], error)
	ResolvePaymentAnomaly(context.Context, string, string, string) (p.Anomaly, error)
	ClaimPaymentBill(context.Context, string, *time.Time, time.Time) (PaymentBillClaim, bool, error)
	FinishPaymentBill(context.Context, string, PaymentBillClaim, bool, map[string]int64) error
	PaymentOrdersForBill(context.Context, string, time.Time, time.Time) ([]p.Order, error)
}

// DemoPaymentTrade 仅供演示适配器保存其模拟渠道事实；不包含订单或钱包内部状态。
type DemoPaymentTrade struct {
	OrderNo, State         string
	AmountFen, RefundedFen int64
	PaidAt                 *time.Time
	RefundNo               string
	RefundBillAt           *time.Time
	UpdatedAt              time.Time
}
type DemoPaymentStore interface {
	CreateDemoPayment(context.Context, string, int64) (DemoPaymentTrade, error)
	DemoPayment(context.Context, string) (DemoPaymentTrade, error)
	CloseDemoPayment(context.Context, string) (DemoPaymentTrade, error)
	SimulateDemoPayment(context.Context, string, string) (DemoPaymentTrade, error)
	RefundDemoPayment(context.Context, ChannelRefund) (DemoPaymentTrade, error)
}
