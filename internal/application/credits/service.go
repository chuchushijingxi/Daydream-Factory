package credits

import (
	"context"
	"drama-agent/internal/application/ports"
	a "drama-agent/internal/domain/accounts"
	d "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
)

type Service struct {
	Ledger  ports.CreditLedger
	Queries ports.CreditQueries
	Updates ports.CreditUpdates
	Users   interface {
		CurrentUser(context.Context, string) (a.User, error)
	}
}

func (s *Service) Watch(ctx context.Context, actor ports.Actor, target string) (<-chan struct{}, func(), error) {
	id, err := s.account(ctx, actor, target)
	if err != nil {
		return nil, nil, err
	}
	if s.Updates == nil {
		return nil, nil, fault.New("CREDIT_UPDATES_UNAVAILABLE", "积分实时同步暂不可用")
	}
	return s.Updates.WatchCreditAccount(ctx, id)
}

func (s *Service) Wallet(ctx context.Context, actor ports.Actor, target string) (d.Wallet, error) {
	id, e := s.account(ctx, actor, target)
	if e != nil {
		return d.Wallet{}, e
	}
	if s.Queries == nil {
		return d.Wallet{}, fault.New("CREDIT_QUERY_UNAVAILABLE", "积分查询尚未配置")
	}
	return s.Queries.CreditWallet(ctx, id)
}

func (s *Service) History(ctx context.Context, actor ports.Actor, target string, filter d.HistoryFilter) (d.History, error) {
	id, e := s.account(ctx, actor, target)
	if e != nil {
		return d.History{}, e
	}
	if e := filter.Validate(); e != nil {
		return d.History{}, e
	}
	if s.Queries == nil {
		return d.History{}, fault.New("CREDIT_QUERY_UNAVAILABLE", "积分查询尚未配置")
	}
	return s.Queries.CreditHistory(ctx, id, filter)
}

func (s *Service) RecentEntries(ctx context.Context, actor ports.Actor) ([]d.Entry, error) {
	id, err := s.account(ctx, actor, "")
	if err != nil {
		return nil, err
	}
	if s.Queries == nil {
		return nil, fault.New("CREDIT_QUERY_UNAVAILABLE", "积分查询尚未配置")
	}
	return s.Queries.RecentCreditEntries(ctx, id)
}

func (s *Service) account(ctx context.Context, actor ports.Actor, target string) (string, error) {
	if e := actor.Require(); e != nil {
		return "", e
	}
	if target == "" {
		return actor.UserID, nil
	}
	if e := actor.RequireOperator(); e != nil {
		return "", e
	}
	if _, e := s.Users.CurrentUser(ctx, target); e != nil {
		return "", e
	}
	return target, nil
}
func (s *Service) Balance(ctx context.Context, actor ports.Actor, target string) (int64, error) {
	id, e := s.account(ctx, actor, target)
	if e != nil {
		return 0, e
	}
	return s.Ledger.Balance(ctx, id)
}
func (s *Service) Entries(ctx context.Context, actor ports.Actor, target string, page, size int) (ports.Page[d.Entry], error) {
	out := ports.Page[d.Entry]{Page: page, PageSize: size}
	if e := ports.CheckPage(page, size); e != nil {
		return out, e
	}
	id, e := s.account(ctx, actor, target)
	if e != nil {
		return out, e
	}
	out.Items, out.Total, e = s.Ledger.CreditEntries(ctx, id, page, size)
	return out, e
}
func (s *Service) Adjust(ctx context.Context, actor ports.Actor, target, kind, operation, reason string, amount int64, category string) (d.Entry, error) {
	if e := actor.RequireOperator(); e != nil {
		return d.Entry{}, e
	}
	if _, e := s.account(ctx, actor, target); e != nil {
		return d.Entry{}, e
	}
	c := d.Change{AccountID: target, Amount: amount, OperationID: operation, Source: "operations-adjustment", Subject: actor.UserID, Reason: reason, Category: category}
	switch kind {
	case "add":
		return s.Ledger.Add(ctx, c)
	case "deduct":
		return s.Ledger.Deduct(ctx, c)
	}
	return d.Entry{}, fault.ErrInvalid
}
func (s *Service) Operation(ctx context.Context, actor ports.Actor, id string) (d.Entry, error) {
	if e := actor.RequireOperator(); e != nil {
		return d.Entry{}, e
	}
	return s.Ledger.CreditOperation(ctx, id)
}
