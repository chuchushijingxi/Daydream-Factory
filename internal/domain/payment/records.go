package payment

import "time"

type Settings struct {
	Revision      uint64 `json:"revision"`
	DailyLimitFen int64  `json:"dailyLimitFen"`
}
type Event struct {
	ID         string         `json:"id"`
	OrderNo    string         `json:"orderNo,omitempty"`
	RefundID   string         `json:"refundId,omitempty"`
	Source     string         `json:"source"`
	Outcome    string         `json:"outcome"`
	OperatorID string         `json:"operatorId,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Facts      map[string]any `json:"facts"`
	CreatedAt  time.Time      `json:"createdAt"`
}
type Refund struct {
	BillAt          *time.Time `json:"billAt"`
	ID              string     `json:"id"`
	RefundNo        string     `json:"refundNo"`
	OrderNo         string     `json:"orderNo"`
	Kind            string     `json:"kind"`
	AmountFen       int64      `json:"amountFen"`
	RechargeCredits int64      `json:"rechargeCredits"`
	GiftCredits     int64      `json:"giftCredits"`
	Status          string     `json:"status"`
	OperatorID      string     `json:"operatorId,omitempty"`
	Reason          string     `json:"reason"`
	ChannelRefundID string     `json:"channelRefundId"`
	Attempts        int        `json:"attempts"`
	NextQueryAt     time.Time  `json:"-"`
	CreatedAt       time.Time  `json:"createdAt"`
	FinishedAt      *time.Time `json:"finishedAt"`
}
type RefundRequest struct {
	RefundID, OrderNo, OperatorID, Reason string
	Credits                               int64
}
type Anomaly struct {
	ID         string         `json:"id"`
	OrderNo    string         `json:"orderNo,omitempty"`
	Channel    string         `json:"channel"`
	Kind       string         `json:"kind"`
	Detail     map[string]any `json:"detail"`
	Status     string         `json:"status"`
	ResolvedBy string         `json:"resolvedBy,omitempty"`
	Note       string         `json:"note,omitempty"`
	ResolvedAt *time.Time     `json:"resolvedAt"`
	CreatedAt  time.Time      `json:"createdAt"`
}
