package spaces

import (
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	d "drama-agent/internal/domain/spaces"
	"time"
)

type AllocationInput struct {
	CommandID string `json:"commandId"`
	Amount    int64  `json:"amount,omitempty"`
	All       bool   `json:"all,omitempty"`
}
type CreditWalletView struct {
	SpaceID      string `json:"spaceId"`
	Role         d.Role `json:"role"`
	Balance      int64  `json:"balance"`
	TotalBalance *int64 `json:"totalBalance,omitempty"`
	Allocated    *int64 `json:"allocated,omitempty"`
	Unallocated  *int64 `json:"unallocated,omitempty"`
}

// CreditEntryView 明确选择可外发的账务字段；持有人不可看到整账户的历史余额。
type CreditEntryView struct {
	ID                  string    `json:"id"`
	OperationID         string    `json:"operationId"`
	AccountID           string    `json:"accountId"`
	HolderID            string    `json:"holderId,omitempty"`
	Kind                string    `json:"kind"`
	Amount              int64     `json:"amount"`
	Balance             *int64    `json:"balance,omitempty"`
	Category            string    `json:"category"`
	RechargeAmount      int64     `json:"rechargeAmount"`
	GiftAmount          int64     `json:"giftAmount"`
	LegacyAmount        int64     `json:"legacyAmount"`
	Source              string    `json:"source"`
	Subject             string    `json:"subject"`
	Reason              string    `json:"reason"`
	OriginalDeductionID string    `json:"originalDeductionId,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
}
type CreditHistoryView struct {
	Items       []CreditEntryView              `json:"items"`
	Total       int64                          `json:"total"`
	Page        int                            `json:"page"`
	PageSize    int                            `json:"pageSize"`
	Summary     c.HistorySummary               `json:"summary"`
	Analysis    []c.ConsumptionAnalysis        `json:"analysis"`
	Allocations *ports.Page[c.AllocationEvent] `json:"allocations,omitempty"`
}
type CreditRecentView struct {
	Items       []CreditEntryView   `json:"items"`
	Allocations []c.AllocationEvent `json:"allocations,omitempty"`
}

func creditEntryViews(entries []c.Entry, includeBalance bool) []CreditEntryView {
	out := make([]CreditEntryView, 0, len(entries))
	for _, entry := range entries {
		view := CreditEntryView{ID: entry.ID, OperationID: entry.OperationID, AccountID: entry.AccountID, HolderID: entry.HolderID, Kind: entry.Kind, Amount: entry.Amount, Category: entry.Category, RechargeAmount: entry.RechargeAmount, GiftAmount: entry.GiftAmount, LegacyAmount: entry.LegacyAmount, Source: entry.Source, Subject: entry.Subject, Reason: entry.Reason, OriginalDeductionID: entry.OriginalDeductionID, CreatedAt: entry.CreatedAt}
		if includeBalance {
			balance := entry.Balance
			view.Balance = &balance
		}
		out = append(out, view)
	}
	return out
}
func creditHistoryView(history c.History, includeBalance bool) CreditHistoryView {
	return CreditHistoryView{Items: creditEntryViews(history.Items, includeBalance), Total: history.Total, Page: history.Page, PageSize: history.PageSize, Summary: history.Summary, Analysis: history.Analysis}
}
