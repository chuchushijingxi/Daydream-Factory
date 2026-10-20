package project

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

// MediaCreditView 与使用记录共用同一结算口径：只有账本回执算预付/返还，调用方自报不计。
type MediaCreditView struct {
	Basis       c.Charge `json:"basis"`
	Prepaid     int64    `json:"prepaid"`
	Refunded    int64    `json:"refunded"`
	Charged     int64    `json:"charged"`
	Settlement  string   `json:"settlement"`
	DeductionID *string  `json:"deductionId"`
	RefundID    *string  `json:"refundId"`
}

func mediaChargeID(owner, project, command string) string {
	return "media_" + d.Digest([]string{owner, project, command})
}

func (s *Service) mediaQuote(ctx context.Context, p d.Project, model string, target MediaTarget, imageType, aspectRatio string, requested *md.MediaOptions, at time.Time) (ChargeQuote, string, error) {
	if e := s.mediaReady(); e != nil {
		return ChargeQuote{}, "", e
	}
	kind, _, inputDigest, e := mediaInput(&p, MediaCommand{Target: target, ImageType: imageType, AspectRatio: aspectRatio})
	if e != nil {
		return ChargeQuote{}, "", e
	}
	if _, e := characterImageComposition(&p, target, imageType); e != nil {
		return ChargeQuote{}, "", e
	}
	ratio, e := mediaImageAspectRatio(&p, target, imageType, aspectRatio)
	if e != nil {
		return ChargeQuote{}, "", e
	}
	if s.Prices == nil {
		return ChargeQuote{}, "", fault.New("MODEL_PRICE_UNAVAILABLE", "积分定价服务不可用")
	}
	prices, ok := s.Prices.(ports.MediaPrices)
	if !ok {
		return ChargeQuote{}, "", fault.New("MODEL_PRICE_UNAVAILABLE", "媒体定价服务不可用")
	}
	pricing, e := prices.MediaPrice(ctx, model)
	if e != nil {
		return ChargeQuote{}, "", e
	}
	rule, version, connection := pricing.Rule, pricing.Version, pricing.ConnectionID
	if !rule.Valid() {
		return ChargeQuote{}, "", fault.ErrInvalid
	}
	profile := pricing.Profile
	if profile == nil {
		return ChargeQuote{}, "", fault.New("MEDIA_PROFILE_UNAVAILABLE", "模型尚无已核实的媒体规格")
	}
	if kind == "image" && !slices.Contains(profile.AspectRatios, ratio) {
		return ChargeQuote{}, "", fault.New("IMAGE_ASPECT_RATIO_INVALID", "当前模型不支持该图片比例")
	}
	options := profile.Defaults()
	if requested != nil {
		options = *requested
	}
	if !profile.Supports(options) {
		return ChargeQuote{}, "", fault.New("MEDIA_OPTIONS_INVALID", "分辨率或时长不受当前模型支持")
	}
	rule, ok = rule.AtResolution(options.Resolution, pricing.PriceDefaultResolution)
	if !ok {
		return ChargeQuote{}, "", fault.New("MODEL_PRICE_UNAVAILABLE", "该分辨率尚未配置积分单价")
	}
	if connection != "mock" || !md.IsMockModel(model, kind) {
		return ChargeQuote{}, "", fault.New("TASK_PROTOCOL_UNSUPPORTED", "真实媒体厂商任务尚未接入")
	}
	unit, quantity := "image", int64(1)
	if kind == "video" {
		unit, quantity = "second", int64(options.DurationSeconds)
	}
	original, e := c.Price(quantity, rule.UnitPrice, 1)
	if e != nil {
		return ChargeQuote{}, "", e
	}
	q := ChargeQuote{MediaOptions: &options, SourceVersion: p.ScriptSource.Version, InputRevision: p.Revision, ModelID: model, ConnectionID: connection, Scope: "media-" + kind, Quantity: quantity, Unit: unit, PriceVersion: version, Rule: rule, OriginalAmount: original, Amount: original, Free: rule.Free(at)}
	if q.Free {
		q.Amount = 0
	}
	digest := d.Digest([]any{p.ID, target, inputDigest, options})
	if imageType != "" || aspectRatio != "" {
		digest = d.Digest([]any{digest, imageType, ratio})
	}
	q.Basis = d.Digest([]any{q, digest})
	return q, digest, nil
}

func (s *Service) freezeMediaCredit(ctx context.Context, p d.Project, in MediaCommand) (*c.Charge, *md.MediaOptions, error) {
	if p.OwnerID == "" {
		return nil, nil, nil
	} // 仅有无归属的内部保留夹具；正式用户存储会拒绝无归属项目。
	if s.Credits == nil {
		return nil, nil, fault.New("MODEL_PRICE_UNAVAILABLE", "积分账本不可用")
	}
	at := time.Now().UTC()
	q, digest, e := s.mediaQuote(ctx, p, in.ModelID, in.Target, in.ImageType, in.AspectRatio, in.Options, at)
	if e != nil {
		return nil, nil, e
	}
	if in.BillingBasis == "" || in.BillingBasis != q.Basis {
		return nil, nil, fault.New("BILLING_BASIS_CHANGED", "计费依据已变化，请回读当前报价")
	}
	return &c.Charge{ID: mediaChargeID(p.OwnerID, p.ID, in.CommandID), SourceVersion: q.SourceVersion, InputRevision: q.InputRevision, Unit: q.Unit, ModelID: q.ModelID, ConnectionID: q.ConnectionID, Scope: q.Scope, ScopeDigest: digest, Quantity: q.Quantity, PriceVersion: q.PriceVersion, Rule: q.Rule, AcceptedAt: at, OriginalAmount: q.OriginalAmount, Amount: q.Amount, Free: q.Free}, q.MediaOptions, nil
}

func mediaDebit(owner, project string, charge *c.Charge) c.Change {
	return c.Change{AccountID: owner, Amount: charge.Amount, OperationID: creditDeductionID(charge.ID), Source: project, Subject: owner, Reason: "generation"}
}

// MediaCreditPermit 是媒体外发的业务前置，独立注入 API 与独立任务 worker：
// 只用任务的持久归属，不读会话或项目聚合，重启后仍核对原始计费依据。
type MediaCreditPermit struct {
	Ledger ports.CreditLedger
	Tasks  ports.TaskStore
}

func (g *MediaCreditPermit) ConfirmTaskSubmission(ctx context.Context, t td.Task, snapshot td.Snapshot) (*ports.TaskRejection, error) {
	var meta MediaContext
	if g.Ledger == nil || g.Tasks == nil {
		return nil, fault.New("MODEL_PRICE_UNAVAILABLE", "积分账本不可用")
	}
	invalid := &ports.TaskRejection{Code: "BILLING_BASIS_INVALID", Message: "任务缺少有效的已保存计费依据"}
	if json.Unmarshal(snapshot.CallerContext, &meta) != nil || meta.Credit == nil || t.OwnerID == "" || !strings.HasPrefix(t.ScopeID, "project:") {
		return invalid, nil
	}
	charge := meta.Credit
	project := strings.TrimPrefix(t.ScopeID, "project:")
	if !frozenMediaCharge(charge, t, project) {
		return invalid, nil
	}
	if !mediaChargeQuantity(t, snapshot, charge) {
		return invalid, nil
	}
	current, e := g.Tasks.Task(ctx, t.ID)
	if e != nil {
		return nil, e
	}
	if current.Status.Terminal() {
		return nil, nil
	} // 取消后的拒绝外发由内核的归属/修订/租约 CAS 保证，这里只补记事实。
	if charge.Amount == 0 {
		return nil, nil
	}
	change := mediaDebit(t.OwnerID, project, charge)
	entry, e := g.Ledger.CreditOperation(ctx, change.OperationID)
	if errors.Is(e, fault.ErrNotFound) {
		entry, e = g.Ledger.Deduct(ctx, change)
	}
	if e != nil {
		var f *fault.Error
		if errors.As(e, &f) && f.Code == "CREDIT_INSUFFICIENT" {
			return &ports.TaskRejection{Code: f.Code, Message: f.Message}, nil
		}
		return nil, e
	}
	if !entry.Same("deduct", change) {
		return nil, fault.New("CREDIT_IDEMPOTENCY_CONFLICT", "原预扣回执与任务依据不符")
	}
	return nil, nil
}

// frozenMediaCharge 逐项核对已保存的计费依据未被拼装错误或篡改：规则有效、
// 冻结时刻的免费标记与规则一致、按冻结规则重算的原价一致，且任务绑定与命令编号互相吻合。
// 免费金额必须为 0；非免费金额必须等于重算原价。调用方保证 charge 非 nil。
func frozenMediaCharge(charge *c.Charge, t td.Task, project string) bool {
	if !charge.Rule.Valid() || charge.AcceptedAt.IsZero() || charge.ScopeDigest == "" {
		return false
	}
	original, e := c.Price(charge.Quantity, charge.Rule.UnitPrice, 1)
	if e != nil || original != charge.OriginalAmount {
		return false
	}
	if charge.Free != charge.Rule.Free(charge.AcceptedAt) {
		return false
	}
	if charge.Free && charge.Amount != 0 || !charge.Free && charge.Amount != original {
		return false
	}
	return charge.ID == mediaChargeID(t.OwnerID, project, t.CommandID) &&
		charge.ModelID == t.State.ModelID &&
		charge.ConnectionID == t.State.Binding.ConnectionID
}

// 计量只依受理时冻结的供应模式与输入；不得用今天的规格默认值重新解释此前受理的历史样片任务。
func mediaChargeQuantity(t td.Task, snapshot td.Snapshot, charge *c.Charge) bool {
	if t.State.Operation == "image.generate" {
		return charge.Unit == "image" && charge.Quantity == 1 && charge.Scope == "media-image"
	}
	if t.State.Operation != "video.generate" || charge.Unit != "second" || charge.Scope != "media-video" {
		return false
	}
	var execution md.MockExecution
	if len(t.State.Binding.ProviderConfig) > 0 && json.Unmarshal(t.State.Binding.ProviderConfig, &execution) != nil {
		return false
	}
	var options md.MediaOptions
	if json.Unmarshal(snapshot.Input, &options) != nil {
		return false
	}
	if !execution.VideoOptions {
		return charge.Quantity == md.MockVideoBillingSeconds && (options.Resolution == "" && options.DurationSeconds == 0 || options.Resolution == "1080p" && options.DurationSeconds == md.MockVideoDurationSeconds)
	}
	return md.OfficialMediaProfile("mock", md.MockVideoModelID).Supports(options) && charge.Quantity == int64(options.DurationSeconds)
}

// 一页只取一次原账本；完整冻结依据仍在各自 task input，缺失和冲突沿原规则处理。
func (s *Service) mediaLedgerEntries(ctx context.Context, snapshots map[string]td.Snapshot) (map[string]c.Entry, error) {
	ids := []string{}
	seen := map[string]bool{}
	for _, snapshot := range snapshots {
		var meta MediaContext
		if json.Unmarshal(snapshot.CallerContext, &meta) != nil {
			return nil, fault.ErrInvalid
		}
		if meta.Credit == nil {
			continue
		}
		if s.Credits == nil {
			return nil, fault.New("MODEL_PRICE_UNAVAILABLE", "积分账本不可用")
		}
		if meta.Credit.Amount <= 0 {
			continue
		}
		for _, id := range []string{creditDeductionID(meta.Credit.ID), creditRefundID(meta.Credit.ID)} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return map[string]c.Entry{}, nil
	}
	return s.Credits.CreditOperations(ctx, ids)
}

func mediaCredit(owner, project string, charge *c.Charge, status td.Status, entries map[string]c.Entry) (*MediaCreditView, error) {
	if charge == nil {
		return nil, nil
	}
	out := &MediaCreditView{Basis: *charge, Settlement: "pending"}
	if charge.Amount > 0 {
		change := mediaDebit(owner, project, charge)
		debit, exists := entries[change.OperationID]
		if exists {
			if !debit.Same("deduct", change) {
				return nil, fault.ErrConflict
			}
			out.Prepaid = debit.Amount
			out.DeductionID = &debit.ID
			refund, exists := entries[creditRefundID(charge.ID)]
			if exists {
				if !refund.Same("refund", mediaRefund(owner, charge.ID, debit)) {
					return nil, fault.ErrConflict
				}
				out.Refunded = refund.Amount
				out.RefundID = &refund.ID
			}
		}
	}
	if status.Terminal() {
		out.Settlement = "settled"
		if status == td.Succeeded {
			if charge.Amount > 0 && out.DeductionID == nil {
				return nil, fault.New("CREDIT_QUERY_UNAVAILABLE", "成功任务缺少可核对的原预扣回执")
			}
			out.Charged = out.Prepaid - out.Refunded
		}
		if (status == td.Failed || status == td.Cancelled) && out.Prepaid > out.Refunded {
			out.Settlement = "refund-pending"
		}
		if out.RefundID != nil {
			out.Settlement = "refunded"
		}
	}
	return out, nil
}

func mediaRefund(owner, chargeID string, debit c.Entry) c.Change {
	return c.Change{OriginalDeductionID: debit.ID, Amount: debit.Amount, OperationID: creditRefundID(chargeID), Subject: owner, Reason: "generation-incomplete"}
}

// RecoverMediaCredits 只结算终态且预扣未返还的任务；适配器只返回原任务与账本的窄事实。
func (s *Service) RecoverMediaCredits(ctx context.Context) error {
	if s.Credits == nil {
		return nil
	}
	queries, ok := s.Store.(ports.CreditRecoveryMedia)
	if !ok {
		return fault.New("CREDIT_RECOVERY_UNAVAILABLE", "积分恢复查询能力不可用")
	}
	items, e := queries.UnsettledMediaCredits(ctx)
	if e != nil {
		return e
	}
	for _, item := range items {
		change := mediaDebit(item.OwnerID, item.ProjectID, &c.Charge{ID: item.ChargeID, Amount: item.Amount})
		debit, e := s.Credits.CreditOperation(ctx, change.OperationID)
		if e != nil {
			return e
		}
		if !debit.Same("deduct", change) {
			return fault.ErrConflict
		}
		if _, e = s.Credits.Refund(ctx, mediaRefund(item.OwnerID, item.ChargeID, debit)); e != nil {
			return e
		}
	}
	return nil
}

// 当前执行只接受本地已实现的供应方；受理前用同一规格档案核对所选参数。
func mediaOptions(model string, requested *md.MediaOptions) (md.MediaOptions, error) {
	profile := md.OfficialMediaProfile("mock", model)
	in := md.MediaOptions{}
	if requested != nil {
		in = *requested
	}
	if profile == nil {
		return in, fault.New("TASK_PROTOCOL_UNSUPPORTED", "真实媒体厂商任务尚未接入")
	}
	if requested == nil {
		in = profile.Defaults()
	}
	if !profile.Supports(in) {
		return in, fault.New("MEDIA_OPTIONS_INVALID", "分辨率或时长不受当前模型支持")
	}
	return in, nil
}
