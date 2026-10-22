// Package payment coordinates payment orders without owning channel protocols or credit arithmetic.
package payment

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/accounts"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/payment"
)

type Service struct {
	Store    ports.PaymentStore
	Channels map[string]ports.PaymentChannel
	Targets  ports.RechargeTargets
	Users    interface {
		CurrentUser(context.Context, string) (accounts.User, error)
	}
	RateLimiter ports.RateLimiter
	Environment string
	Now         func() time.Time
}

type ChannelOption struct {
	ID   string `json:"id"`
	Demo bool   `json:"demo"`
}
type Agreement struct {
	Version string `json:"version"`
	Agreed  bool   `json:"agreed"`
}

// PackageOption 显式投影公开档位，运营内部名称不得进入用户响应或订单快照。
// PackageOption 的名称自2026-10-21起展示在用户收银卡片上（用户确认）；订单快照仍不保存名称。
type PackageOption struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Revision          uint64    `json:"revision"`
	OriginalAmountFen int64     `json:"originalAmountFen"`
	AmountFen         int64     `json:"amountFen"`
	RechargeCredits   int64     `json:"rechargeCredits"`
	GiftCredits       int64     `json:"giftCredits"`
	Enabled           bool      `json:"enabled"`
	Sort              int       `json:"sort"`
	TestOnly          bool      `json:"testOnly"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}
type Options struct {
	Channels  []ChannelOption `json:"channels"`
	Packages  []PackageOption `json:"packages"`
	Targets   []p.Target      `json:"targets"`
	Agreement Agreement       `json:"agreement"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) channel(id string) (ports.PaymentChannel, error) {
	ch := s.Channels[id]
	if ch == nil || ch.ID() != id {
		return nil, fault.New("PAYMENT_CHANNEL_UNAVAILABLE", "支付渠道暂不可用")
	}
	return ch, nil
}
func (s *Service) testPackages(actor ports.Actor) bool {
	return (s.Environment == "development" || s.Environment == "staging") && actor.Role == ports.RoleOperator
}
func (s *Service) Options(ctx context.Context, actor ports.Actor) (Options, error) {
	out := Options{Channels: []ChannelOption{}, Packages: []PackageOption{}, Targets: []p.Target{}, Agreement: Agreement{Version: p.AgreementVersion}}
	if err := actor.Require(); err != nil {
		return out, err
	}
	for id, ch := range s.Channels {
		if ch != nil && ch.ID() == id {
			out.Channels = append(out.Channels, ChannelOption{ID: id, Demo: id == "demo"})
		}
	}
	sort.Slice(out.Channels, func(i, j int) bool { return out.Channels[i].ID < out.Channels[j].ID })
	packages, err := s.Store.PaymentPackages(ctx)
	if err != nil {
		return out, err
	}
	for _, pkg := range packages {
		if pkg.Enabled && (!pkg.TestOnly || s.testPackages(actor)) {
			out.Packages = append(out.Packages, PackageOption{ID: pkg.ID, Name: pkg.Name, Revision: pkg.Revision, OriginalAmountFen: pkg.OriginalAmountFen, AmountFen: pkg.AmountFen, RechargeCredits: pkg.RechargeCredits, GiftCredits: pkg.GiftCredits, Enabled: pkg.Enabled, Sort: pkg.Sort, TestOnly: pkg.TestOnly, CreatedAt: pkg.CreatedAt, UpdatedAt: pkg.UpdatedAt})
		}
	}
	sort.SliceStable(out.Packages, func(i, j int) bool {
		if out.Packages[i].Sort == out.Packages[j].Sort {
			return out.Packages[i].ID < out.Packages[j].ID
		}
		return out.Packages[i].Sort < out.Packages[j].Sort
	})
	if s.Targets == nil {
		return out, fault.New("PAYMENT_TARGET_FORBIDDEN", "充值目标不可用")
	}
	out.Targets, err = s.Targets.List(ctx, actor)
	if err != nil {
		return out, err
	}
	if out.Targets == nil {
		out.Targets = []p.Target{}
	}
	out.Agreement.Agreed, err = s.Store.PaymentAgreement(ctx, actor.UserID, p.AgreementVersion)
	return out, err
}

// PersonalTargets 只含个人空间，供未组装团队空间的测试与独立部署使用；
// 服务入口使用 spaces.Service.RechargeTargets。
type PersonalTargets struct{}

func (PersonalTargets) List(_ context.Context, actor ports.Actor) ([]p.Target, error) {
	if err := actor.Require(); err != nil {
		return nil, err
	}
	return []p.Target{{Kind: "personal", ID: actor.UserID, Name: "个人空间"}}, nil
}
func (PersonalTargets) Resolve(_ context.Context, actor ports.Actor, target p.Target) (ports.ResolvedRechargeTarget, error) {
	if err := actor.Require(); err != nil {
		return ports.ResolvedRechargeTarget{}, err
	}
	if target.Kind != "personal" || target.ID != actor.UserID {
		return ports.ResolvedRechargeTarget{}, fault.New("PAYMENT_TARGET_FORBIDDEN", "不能向此空间充值")
	}
	return ports.ResolvedRechargeTarget{Target: p.Target{Kind: "personal", ID: actor.UserID, Name: "个人空间"}, AccountID: actor.UserID}, nil
}
func (s *Service) requireBuyer(ctx context.Context, actor ports.Actor) error {
	if err := actor.Require(); err != nil {
		return err
	}
	if s.Users == nil {
		return fault.New("AUTH_UNAVAILABLE", "账号查询暂不可用")
	}
	user, err := s.Users.CurrentUser(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if user.ID != actor.UserID {
		return fault.New("AUTH_REQUIRED", "账号身份不一致")
	}
	if user.Status != "enabled" {
		return fault.New("ACCOUNT_DISABLED", "账号已禁用")
	}
	return nil
}
func validID(id string) bool {
	return id != "" && len(id) <= 128 && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "\x00\r\n")
}
func validReason(reason string) bool {
	return strings.TrimSpace(reason) != "" && len(reason) <= 2000 && !strings.ContainsRune(reason, '\x00')
}
func codeIs(err error, code string) bool {
	var f *fault.Error
	return errors.As(err, &f) && f.Code == code
}
func notFound(err error) bool {
	return errors.Is(err, fault.ErrNotFound) || codeIs(err, "PAYMENT_ORDER_NOT_FOUND")
}
func orderError(err error) error {
	if notFound(err) {
		return fault.New("PAYMENT_ORDER_NOT_FOUND", "充值订单不存在")
	}
	return err
}
func channelError(err error) error {
	return fault.Wrap("PAYMENT_CHANNEL_FAILED", "支付渠道暂不可用，请稍后重试", err)
}
func validSource(source string) bool {
	switch source {
	case "notify", "query", "bill", "simulate":
		return true
	}
	return false
}
func validTradeState(state string) bool {
	switch state {
	case "paid", "notpay", "closed", "failed", "not_found":
		return true
	}
	return false
}
func (s *Service) event(ctx context.Context, orderNo, source, outcome, operator, reason string, facts map[string]any) error {
	return s.Store.RecordPaymentEvent(ctx, p.Event{ID: identity.New("pe"), OrderNo: orderNo, Source: source, Outcome: outcome, OperatorID: operator, Reason: reason, Facts: facts, CreatedAt: s.now()})
}
func (s *Service) alert(ctx context.Context, kind, channel, no string, detail map[string]any) error {
	// 日志只带已知类别和订单身份；渠道报文、签名、密钥及原始错误不进入告警。
	slog.ErrorContext(ctx, "payment_alert", "kind", kind, "channel", channel, "order_no", no)
	return s.Store.RecordPaymentAnomaly(ctx, p.Anomaly{ID: identity.New("pa"), OrderNo: no, Channel: channel, Kind: kind, Detail: detail, Status: "open", CreatedAt: s.now()})
}

// RejectNotification 留下验签或商户身份失败的安全事实；原始通知绝不进入应用存储。
func (s *Service) RejectNotification(ctx context.Context, channel, kind string) error {
	if _, err := s.channel(channel); err != nil {
		return err
	}
	if kind != "signature_invalid" && kind != "identity_mismatch" && kind != "amount_mismatch" && kind != "notification_invalid" {
		return fault.ErrInvalid
	}
	return s.alert(ctx, kind, channel, "", nil)
}

// 渠道请求可能已耗尽调用Context，错误审计仍给5秒独立预算，不保留原始报文/错误内容。
func (s *Service) channelFailureEvent(ctx context.Context, no, source string) error {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.event(auditCtx, no, source, "channel_failed", "", "", map[string]any{"errorCode": "PAYMENT_CHANNEL_FAILED"})
}
