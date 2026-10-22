package credits

import (
	"drama-agent/internal/domain/fault"
	"time"
)

type HistoryFilter struct {
	Page, PageSize      int
	Direction, Category string
	Start, End          *time.Time
}

func (f HistoryFilter) Validate() error {
	if f.Page < 1 || f.Page > 1000000 || f.PageSize < 1 || f.PageSize > 50 {
		return fault.ErrInvalid
	}
	if f.Direction != "all" && f.Direction != "acquired" && f.Direction != "consumed" && f.Direction != "expired" && f.Direction != "reclaimed" {
		return fault.ErrInvalid
	}
	if f.Category != "all" && f.Category != Recharge && f.Category != Gift && f.Category != Legacy {
		return fault.ErrInvalid
	}
	if f.Start != nil && f.End != nil && !f.Start.Before(*f.End) {
		return fault.ErrInvalid
	}
	return nil
}

// Acquired 只统计发放；返还绝不再次计为发放。Consumed 是历史扣减总额；
// SettledConsumed 须有持久化的完成业务结果或运营显式调整；Pending 不断言生成已成功。
type HistorySummary struct {
	Acquired        int64 `json:"acquired"`
	Consumed        int64 `json:"consumed"`
	Refunded        int64 `json:"refunded"`
	NetConsumed     int64 `json:"netConsumed"`
	SettledConsumed int64 `json:"settledConsumed"`
	Pending         int64 `json:"pending"`
}

type ConsumptionAnalysis struct {
	Source string `json:"source"`
	Amount int64  `json:"amount"`
}

type History struct {
	Items    []Entry               `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
	Summary  HistorySummary        `json:"summary"`
	Analysis []ConsumptionAnalysis `json:"analysis"`
}
