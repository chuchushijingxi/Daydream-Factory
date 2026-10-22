package payment

import (
	"context"
	"strings"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
)

func (s *Service) Packages(ctx context.Context, actor ports.Actor) ([]p.Package, error) {
	if err := actor.RequireOperator(); err != nil {
		return nil, err
	}
	return s.Store.PaymentPackages(ctx)
}
func (s *Service) SavePackage(ctx context.Context, actor ports.Actor, pkg p.Package) (p.Package, error) {
	if err := actor.RequireOperator(); err != nil {
		return p.Package{}, err
	}
	if !validID(pkg.ID) || pkg.Revision >= uint64(p.MaxAmount) || int64(pkg.Sort) > p.MaxAmount || int64(pkg.Sort) < -p.MaxAmount {
		return p.Package{}, fault.ErrInvalid
	}
	pkg.Name = strings.TrimSpace(pkg.Name)
	if err := pkg.Validate(); err != nil {
		return p.Package{}, err
	}
	return s.Store.SavePaymentPackage(ctx, pkg, actor.UserID)
}
func (s *Service) Settings(ctx context.Context, actor ports.Actor) (p.Settings, error) {
	if err := actor.RequireOperator(); err != nil {
		return p.Settings{}, err
	}
	return s.Store.PaymentSettings(ctx)
}
func (s *Service) SaveSettings(ctx context.Context, actor ports.Actor, in p.Settings) (p.Settings, error) {
	if err := actor.RequireOperator(); err != nil {
		return p.Settings{}, err
	}
	if in.Revision >= uint64(p.MaxAmount) || in.DailyLimitFen < 0 || in.DailyLimitFen > p.MaxAmount {
		return p.Settings{}, fault.ErrInvalid
	}
	return s.Store.SavePaymentSettings(ctx, in, actor.UserID)
}
func validateOrderFilter(filter ports.PaymentOrderFilter) error {
	if err := ports.CheckPage(filter.Page, filter.PageSize); err != nil {
		return err
	}
	if filter.Start != nil && filter.End != nil && !filter.Start.Before(*filter.End) {
		return fault.ErrInvalid
	}
	if filter.Status != "" && !p.Status(filter.Status).Valid() {
		return fault.ErrInvalid
	}
	if filter.Channel != "" && filter.Channel != "demo" && filter.Channel != "wechatpay" && filter.Channel != "alipay" {
		return fault.ErrInvalid
	}
	if filter.TargetKind != "" && filter.TargetKind != "personal" && filter.TargetKind != "team" {
		return fault.ErrInvalid
	}
	for _, id := range []string{filter.BuyerUserID, filter.OrderNo, filter.TradeNo, filter.TargetID} {
		if id != "" && !validID(id) {
			return fault.ErrInvalid
		}
	}
	return nil
}
func (s *Service) Orders(ctx context.Context, actor ports.Actor, filter ports.PaymentOrderFilter) (ports.Page[p.Order], error) {
	if err := actor.RequireOperator(); err != nil {
		return ports.Page[p.Order]{}, err
	}
	if err := validateOrderFilter(filter); err != nil {
		return ports.Page[p.Order]{}, err
	}
	return s.Store.PaymentOrders(ctx, filter)
}
func (s *Service) OrderDetails(ctx context.Context, actor ports.Actor, no string) (ports.PaymentOrderDetails, error) {
	if err := actor.RequireOperator(); err != nil {
		return ports.PaymentOrderDetails{}, err
	}
	out, err := s.Store.PaymentOrderDetails(ctx, no)
	return out, orderError(err)
}
func (s *Service) QueryOrder(ctx context.Context, actor ports.Actor, no, reason string) (p.Order, error) {
	if err := actor.RequireOperator(); err != nil {
		return p.Order{}, err
	}
	if !validReason(reason) {
		return p.Order{}, fault.ErrInvalid
	}
	order, err := s.Store.PaymentOrder(ctx, no)
	if err != nil {
		return p.Order{}, orderError(err)
	}
	if err = s.event(ctx, no, "query", "requested", actor.UserID, reason, nil); err != nil {
		return order, err
	}
	order, _, err = s.queryOrder(ctx, order, "query", actor.UserID)
	return order, err
}
func (s *Service) Anomalies(ctx context.Context, actor ports.Actor, filter ports.PaymentAnomalyFilter) (ports.Page[p.Anomaly], error) {
	if err := actor.RequireOperator(); err != nil {
		return ports.Page[p.Anomaly]{}, err
	}
	if err := ports.CheckPage(filter.Page, filter.PageSize); err != nil {
		return ports.Page[p.Anomaly]{}, err
	}
	if filter.Status != "" && filter.Status != "open" && filter.Status != "resolved" {
		return ports.Page[p.Anomaly]{}, fault.ErrInvalid
	}
	if filter.Channel != "" && filter.Channel != "demo" && filter.Channel != "wechatpay" && filter.Channel != "alipay" {
		return ports.Page[p.Anomaly]{}, fault.ErrInvalid
	}
	if filter.Kind != "" && !strings.Contains("|signature_invalid|identity_mismatch|amount_mismatch|notification_invalid|paid_after_close|bill_missing_local|bill_missing_channel|bill_amount_mismatch|refund_mismatch|refund_failed|", "|"+filter.Kind+"|") {
		return ports.Page[p.Anomaly]{}, fault.ErrInvalid
	}
	return s.Store.PaymentAnomalies(ctx, filter)
}
func (s *Service) ResolveAnomaly(ctx context.Context, actor ports.Actor, id, note string) (p.Anomaly, error) {
	if err := actor.RequireOperator(); err != nil {
		return p.Anomaly{}, err
	}
	if !validID(id) || !validReason(note) {
		return p.Anomaly{}, fault.ErrInvalid
	}
	return s.Store.ResolvePaymentAnomaly(ctx, id, actor.UserID, note)
}
