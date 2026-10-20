package project

import (
	"context"
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	"errors"
	"strings"
	"time"
)

type ChargeQuote struct {
	MediaOptions   *md.MediaOptions `json:"mediaOptions,omitempty"`
	SourceVersion  uint64           `json:"sourceVersion"`
	InputRevision  uint64           `json:"inputRevision"`
	Basis          string           `json:"basis"`
	ModelID        string           `json:"modelId"`
	ConnectionID   string           `json:"connectionId"`
	Scope          string           `json:"scope"`
	Quantity       int64            `json:"quantity"`
	Unit           string           `json:"unit"`
	PriceVersion   uint64           `json:"priceVersion"`
	Rule           c.PriceRule      `json:"rule"`
	OriginalAmount int64            `json:"originalAmount"`
	Amount         int64            `json:"amount"`
	Free           bool             `json:"free"`
}

func (s *Service) WithBillingBasis(basis string) *Service {
	copy := *s
	copy.billingBasis = basis
	return &copy
}
func chargeText(p d.Project, r *d.Run) (string, string, error) {
	if r.Contract == d.CatalogExtractionContract && r.Stage == "visualPrompt" {
		copy := *r
		copy.Contract = d.VisualContract
		return chargeText(p, &copy)
	}
	switch r.Contract {
	case d.StoryboardContract:
		if r.StoryboardScope == nil || p.Storyboard == nil {
			return "", "", fault.ErrInvalid
		}
		ep := p.Storyboard.Episode(r.StoryboardScope.EpisodeID)
		if ep == nil {
			return "", "", fault.ErrNotFound
		}
		if r.StoryboardScope.Operation == "breakdown" {
			fragments, e := p.ScriptSource.Fragments(ep.SourceRanges)
			if e != nil {
				return "", "", e
			}
			var b strings.Builder
			for _, f := range fragments {
				b.WriteString(f.Text)
			}
			return b.String(), "episode", nil
		}
		var b strings.Builder
		for _, id := range r.StoryboardScope.ShotIDs {
			sh := p.Storyboard.Shot(id)
			if sh == nil {
				return "", "", fault.ErrNotFound
			}
			b.WriteString(sh.Action)
			for _, v := range sh.Dialogues {
				b.WriteString(v.Text)
			}
			b.WriteString(sh.Camera.Size)
			b.WriteString(sh.Camera.Angle)
			b.WriteString(sh.Camera.Movement)
			b.WriteString(sh.DesignNotes)
		}
		return b.String(), "shot-material", nil
	case d.VisualContract:
		var b strings.Builder
		seen := map[string]bool{}
		for _, t := range r.Targets {
			a := p.Asset(t.AssetID)
			if a == nil {
				return "", "", fault.ErrNotFound
			}
			if !seen[a.ID] {
				b.WriteString(a.Name)
				for _, v := range a.Aliases {
					b.WriteString(v)
				}
				b.WriteString(a.Description)
				if v := a.Character; v != nil {
					b.WriteString(v.RoleType)
					b.WriteString(v.Age)
					b.WriteString(v.Gender)
					b.WriteString(v.Biography)
					b.WriteString(v.Appearance)
					b.WriteString(v.Personality)
				}
				seen[a.ID] = true
			}
			for _, l := range a.Looks {
				if l.ID == t.LookID {
					b.WriteString(l.Name)
					b.WriteString(l.Description)
				}
			}
		}
		return b.String(), "asset-material", nil
	default:
		var b strings.Builder
		for _, span := range p.ScriptSource.Spans {
			b.WriteString(span.Text)
		}
		return b.String(), "script", nil
	}
}
func (s *Service) quote(ctx context.Context, p d.Project, r *d.Run, at time.Time) (ChargeQuote, string, error) {
	if s.Prices == nil {
		return ChargeQuote{}, "", fault.New("MODEL_PRICE_UNAVAILABLE", "积分定价服务不可用")
	}
	text, scope, e := chargeText(p, r)
	if e != nil {
		return ChargeQuote{}, "", e
	}
	rule, version, connection, e := s.Prices.CreditPrice(ctx, s.modelID(p))
	if e != nil {
		return ChargeQuote{}, "", e
	}
	n := c.Count(text)
	if n == 0 {
		return ChargeQuote{}, "", fault.ErrInvalid
	}
	amount, e := c.Price(n, rule.UnitPrice, 100)
	if e != nil {
		return ChargeQuote{}, "", e
	}
	var scopeIdentity any
	if r.Contract == d.StoryboardContract {
		scopeIdentity = r.StoryboardScope
	} else if scope == "asset-material" {
		scopeIdentity = r.Targets
	}
	digest := d.Digest([]any{p.ScriptSource.Version, text, scope, scopeIdentity})
	q := ChargeQuote{SourceVersion: p.ScriptSource.Version, InputRevision: p.Revision, ModelID: s.modelID(p), ConnectionID: connection, Scope: scope, Quantity: n, Unit: "characters", PriceVersion: version, Rule: rule, OriginalAmount: amount, Amount: amount, Free: rule.Free(at)}
	if q.Free {
		q.Amount = 0
	}
	q.Basis = d.Digest([]any{q, digest})
	return q, digest, nil
}
func (s *Service) freezeCredit(ctx context.Context, p d.Project, r *d.Run) error {
	if p.OwnerID == "" {
		return nil
	} // 保留的内部历史工作无归属用户，不收费。
	if r.Credit != nil {
		return nil
	}
	if s.Credits == nil {
		return fault.New("MODEL_PRICE_UNAVAILABLE", "积分账本不可用")
	}
	q, digest, e := s.quote(ctx, p, r, r.CreatedAt)
	if e != nil {
		return e
	}
	if s.billingBasis == "" || q.Basis != s.billingBasis {
		return fault.New("BILLING_BASIS_CHANGED", "计费依据已变化，请回读当前报价")
	}
	r.Credit = &c.Charge{SourceVersion: q.SourceVersion, InputRevision: q.InputRevision, Unit: q.Unit, ID: r.ID, ModelID: q.ModelID, ConnectionID: q.ConnectionID, Scope: q.Scope, ScopeDigest: digest, Quantity: q.Quantity, PriceVersion: q.PriceVersion, Rule: q.Rule, AcceptedAt: r.CreatedAt, OriginalAmount: q.OriginalAmount, Amount: q.Amount, Free: q.Free}
	return nil
}
func creditDeductionID(id string) string { return "generation:" + id + ":deduct" }
func creditRefundID(id string) string    { return "generation:" + id + ":refund" }
func (s *Service) ensureCredit(ctx context.Context, p d.Project, r *d.Run) error {
	if p.OwnerID == "" {
		return nil
	}
	if r.Credit == nil || s.Credits == nil {
		return fault.New("MODEL_PRICE_UNAVAILABLE", "缺少已持久化计费依据")
	}
	if r.Credit.Amount == 0 {
		return nil
	}
	_, e := s.Credits.Deduct(ctx, c.Change{AccountID: p.OwnerID, Amount: r.Credit.Amount, OperationID: creditDeductionID(r.Credit.ID), Source: p.ID, Subject: p.OwnerID, Reason: "generation"})
	if e != nil {
		return e
	}
	// 扣减在途时取消可能已提交；放行任何供应商请求前先重读业务事实，
	// 迟到取消在这次重读中即时结算，返还与预扣共用同一幂等编号。
	current, e := s.Store.Snapshot(ctx, p.ID)
	if e != nil {
		return e
	}
	if e = s.settleCredits(ctx, current); e != nil {
		return e
	}
	return nil
}
func (s *Service) settleCredits(ctx context.Context, p d.Project) error {
	if s.Credits == nil || p.OwnerID == "" {
		return nil
	}
	groups := map[string][]d.Run{}
	for _, r := range p.Runs {
		if r.Credit != nil {
			groups[r.Credit.ID] = append(groups[r.Credit.ID], r)
		}
	}
	for _, runs := range groups {
		charge := runs[0].Credit
		if charge.Amount == 0 {
			continue
		}
		failed, active := false, false
		for _, r := range runs {
			failed = failed || r.Status == d.RunFailed || r.Status == d.RunCancelled
			active = active || r.Status == d.RunRunning
		}
		if !failed || active {
			continue
		}
		debit, e := s.Credits.CreditOperation(ctx, creditDeductionID(charge.ID))
		if errors.Is(e, fault.ErrNotFound) {
			continue
		}
		if e != nil {
			return e
		}

		existing, check := s.Credits.CreditOperation(ctx, creditRefundID(charge.ID))
		if check == nil {
			if existing.Kind != "refund" || existing.OriginalDeductionID != debit.ID || existing.AccountID != debit.AccountID || existing.Amount != debit.Amount {
				return fault.New("CREDIT_IDEMPOTENCY_CONFLICT", "返还编号与原扣款不一致")
			}
			continue
		}
		if !errors.Is(check, fault.ErrNotFound) {
			return check
		}
		if _, e = s.Credits.Refund(ctx, c.Change{OriginalDeductionID: debit.ID, Amount: debit.Amount, OperationID: creditRefundID(charge.ID), Subject: p.OwnerID, Reason: "generation-incomplete"}); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) RecoverCredits(ctx context.Context) error {
	if s.Credits == nil {
		return nil
	}
	queries, ok := s.Store.(ports.CreditRecoveryProjects)
	if !ok {
		return fault.New("CREDIT_RECOVERY_UNAVAILABLE", "积分恢复查询能力不可用")
	}
	ids, e := queries.UnsettledCreditProjectIDs(ctx)
	if e != nil {
		return e
	}
	for _, id := range ids {
		p, e := s.Store.Snapshot(ctx, id)
		if e != nil {
			return e
		}
		if e = s.settleCredits(ctx, p); e != nil {
			return e
		}
	}
	return s.RecoverMediaCredits(ctx)
}

type QuoteInput struct {
	AspectRatio   string           `json:"aspectRatio,omitempty"`
	ImageType     string           `json:"imageType,omitempty"`
	BreakdownMode string           `json:"breakdownMode,omitempty"`
	Options       *md.MediaOptions `json:"options,omitempty"`
	ModelID       string           `json:"modelId,omitempty"`
	Target        *MediaTarget     `json:"target,omitempty"`
	Revision      uint64           `json:"revision"`
	FileID        string           `json:"fileId,omitempty"`
	Operation     string           `json:"operation"`
	AssetID       string           `json:"assetId,omitempty"`
	LookID        string           `json:"lookId,omitempty"`
	EpisodeID     string           `json:"episodeId,omitempty"`
	ShotIDs       []string         `json:"shotIds,omitempty"`
}

func (s *Service) Quote(ctx context.Context, id string, in QuoteInput) (ChargeQuote, error) {
	if (in.ImageType != "" || in.AspectRatio != "") && in.Operation != "media" {
		return ChargeQuote{}, fault.ErrInvalid
	}
	if in.BreakdownMode != "" && in.Operation != "breakdown" {
		return ChargeQuote{}, fault.ErrInvalid
	}
	// 媒体核价只依赖当前内容，沿媒体读视图避免传输/解析完整模型原答。
	// 文本报价仍保留完整快照；正式生成继续使用原 Snapshot/Commit。
	var p d.Project
	var e error
	if in.Operation == "media" {
		p, e = s.mediaProject(ctx, id)
	} else {
		p, e = s.Project(ctx, id)
	}
	if e != nil {
		return ChargeQuote{}, e
	}
	if p.Revision != in.Revision {
		return ChargeQuote{}, fault.ErrConflict
	}
	r := d.Run{}
	switch in.Operation {
	case "media":
		if in.Target == nil {
			return ChargeQuote{}, fault.ErrInvalid
		}
		q, _, e := s.mediaQuote(ctx, p, in.ModelID, *in.Target, in.ImageType, in.AspectRatio, in.Options, time.Now().UTC())
		return q, e
	case "replace-source":
		if s.Files == nil {
			return ChargeQuote{}, fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
		}
		file, raw, err := s.Files.ReadOriginal(ctx, in.FileID)
		if err != nil {
			return ChargeQuote{}, err
		}
		source, err := s.Importer.Parse(file.Filename, raw, p.ScriptSource.Version+1)
		if err != nil {
			return ChargeQuote{}, err
		}
		p.ScriptSource = source
		r.Contract = d.EpisodeIndexContract
	case "asset-preparation":
		r.Contract = d.CatalogExtractionContract
		if p.CatalogReady() {
			r.Stage = "visualPrompt"
			r.Targets = p.PendingVisualTargets()
		}
	case "outline":
		r.Contract = d.OutlineContract
	case "episode-index":
		r.Contract = d.EpisodeIndexContract
	case "visual":
		r.Contract = d.VisualContract
		r.Targets = p.PendingVisualTargets()
	case "single-visual":
		r.Contract = d.VisualContract
		r.Targets, e = singleVisualTargets(&p, d.VisualTarget{AssetID: in.AssetID, LookID: in.LookID})
		if e != nil {
			return ChargeQuote{}, e
		}
	case "breakdown", "fillPrompts", "regeneratePrompts":
		if p.Storyboard == nil {
			return ChargeQuote{}, fault.ErrNotFound
		}
		r.Contract = d.StoryboardContract
		r.StoryboardScope, _, e = storyboardPromptScope(&p, in.EpisodeID, in.Operation, in.ShotIDs)
		if e != nil {
			return ChargeQuote{}, e
		}
		if e = s.freezeBreakdownOptions(ctx, p, r.StoryboardScope, in.BreakdownMode); e != nil {
			return ChargeQuote{}, e
		}
	default:
		return ChargeQuote{}, fault.ErrInvalid
	}
	q, _, e := s.quote(ctx, p, &r, time.Now().UTC())
	return q, e
}
func (s *Service) QuoteFile(ctx context.Context, fileID string, settings d.ProjectSettings) (ChargeQuote, error) {
	if s.Files == nil {
		return ChargeQuote{}, fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
	}
	file, raw, e := s.Files.ReadOriginal(ctx, fileID)
	if e != nil {
		return ChargeQuote{}, e
	}
	source, e := s.Importer.Parse(file.Filename, raw, 1)
	if e != nil {
		return ChargeQuote{}, e
	}
	if e = s.validateSettings(ctx, settings, nil); e != nil {
		return ChargeQuote{}, e
	}
	p := d.Project{Revision: 1, ScriptSource: source, ProjectSettings: &settings}
	r := d.Run{Contract: d.EpisodeIndexContract}
	q, _, e := s.quote(ctx, p, &r, time.Now().UTC())
	return q, e
}
