// Package modelusage projects existing execution and ledger facts, without bodies.
package modelusage

import (
	"context"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"encoding/json"
	"time"
)

type Filter struct {
	View                                                                string
	Search                                                              string
	UserID, TaskID, RequestID, ModelID, ConnectionID, Operation, Status string
	From, Until                                                         *time.Time
	Page, PageSize                                                      int
	Sort                                                                string
}
type Record struct {
	ID                string          `json:"id"`
	TaskID            string          `json:"taskId"`
	UserID            string          `json:"userId"`
	Username          *string         `json:"username"`
	Email             *string         `json:"email"`
	Operation         string          `json:"operation"`
	Status            string          `json:"status"`
	ModelID           string          `json:"modelId"`
	ConnectionID      *string         `json:"connectionId"`
	RequestID         *string         `json:"requestId"`
	CreatedAt         time.Time       `json:"createdAt"`
	DurationMS        *int64          `json:"durationMs"`
	InputTokens       *int64          `json:"inputTokens"`
	OutputTokens      *int64          `json:"outputTokens"`
	CacheHitTokens    *int64          `json:"cacheHitTokens"`
	MediaUsage        json.RawMessage `json:"mediaUsage"`
	KnownCostMicroCNY int64           `json:"knownCostMicroCny"`
	UnknownCost       bool            `json:"unknownCost"`
	CallCount         int64           `json:"callCount"`
	Basis             json.RawMessage `json:"basis"`
	Prepaid           int64           `json:"prepaid"`
	Refunded          int64           `json:"refunded"`
	Charged           int64           `json:"charged"`
	Settlement        string          `json:"settlement"`
	DeductionID       *string         `json:"deductionId"`
	RefundID          *string         `json:"refundId"`
	ErrorCode         *string         `json:"errorCode"`
}
type Stats struct {
	Count             int64 `json:"count"`
	CallCount         int64 `json:"callCount"`
	Charged           int64 `json:"charged"`
	Refunded          int64 `json:"refunded"`
	KnownCostMicroCNY int64 `json:"knownCostMicroCny"`
	UnknownCost       bool  `json:"unknownCost"`
}
type Result struct {
	Items    []Record  `json:"items"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
	Stats    Stats     `json:"stats"`
	ReadAt   time.Time `json:"readAt"`
}
type Queries interface {
	ModelUsage(context.Context, Filter) (Result, error)
}
type Service struct{ Queries Queries }

func (s *Service) Query(ctx context.Context, a ports.Actor, f Filter) (Result, error) {
	if e := a.RequireOperator(); e != nil {
		return Result{}, e
	}
	if e := ports.CheckPage(f.Page, f.PageSize); e != nil {
		return Result{}, e
	}
	if f.View != "tasks" && f.View != "calls" {
		return Result{}, fault.ErrInvalid
	}
	if f.Sort != "asc" && f.Sort != "desc" {
		return Result{}, fault.ErrInvalid
	}
	if f.From != nil && f.Until != nil && !f.From.Before(*f.Until) {
		return Result{}, fault.ErrInvalid
	}
	return s.Queries.ModelUsage(ctx, f)
}
