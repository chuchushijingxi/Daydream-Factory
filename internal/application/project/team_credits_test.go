package project

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	uploadapp "drama-agent/internal/application/upload"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
	td "drama-agent/internal/domain/task"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/uploadfiles"
)

type teamCreditIdentity struct{ scope ports.SpaceScope }

func (i *teamCreditIdentity) Resolve(_ context.Context, a ports.Actor, space string) (ports.SpaceScope, error) {
	scope := i.scope
	scope.SpaceID = space
	scope.UserID = a.UserID
	return scope, nil
}
func (i *teamCreditIdentity) Access(ctx context.Context, a ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	scope, err := i.Resolve(ctx, a, r.SpaceID)
	return spaces.Edit, scope, err
}

type teamCreditDiscount struct {
	percent, calls int
	err            error
}

func (d *teamCreditDiscount) CreditDiscount(context.Context, string, string) (int, error) {
	d.calls++
	return d.percent, d.err
}

type teamTextPrice struct{ rule c.PriceRule }

func (p teamTextPrice) CreditPrice(context.Context, string) (c.PriceRule, uint64, string, error) {
	return p.rule, 1, "connection", nil
}

func TestTeamTextQuoteRoundsOnceAndFreezesInitiatingMember(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC()
	identity := &teamCreditIdentity{scope: ports.SpaceScope{Kind: spaces.TeamKind, Role: spaces.Member, Status: spaces.Active}}
	discount := &teamCreditDiscount{percent: 60}
	base := &Service{Prices: teamTextPrice{c.PriceRule{UnitPrice: 1}}, Discounts: discount, Resolver: identity, Access: identity, Credits: &capturedGenerationLedger{}}
	service := base.ForScope(ports.SpaceScope{SpaceID: "team", UserID: "initiating-member"})
	project := d.Project{ID: "project", SpaceID: "team", OwnerID: "current-owner", CreatorID: "original-creator", Revision: 1, ProjectSettings: &d.ProjectSettings{LLMModel: "model"}, ScriptSource: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "source", Text: strings.Repeat("中", 110)}}}}
	run := d.Run{ID: "run", Contract: d.OutlineContract, CreatedAt: at}
	quote, _, err := service.quote(ctx, project, &run, at)
	// ceil(110/100)=2；整次打六折应ceil(110*60/10000)=1，不能对已取整的2再打折成2。
	if err != nil || quote.OriginalAmount != 2 || quote.Amount != 1 || quote.DiscountPercent != 60 || quote.AccountID != "team" || quote.HolderID != "initiating-member" || quote.ActorID != "initiating-member" {
		t.Fatal(quote, err)
	}
	if err = service.WithBillingBasis(quote.Basis).freezeCredit(ctx, project, &run); err != nil {
		t.Fatal(err)
	}
	if run.Credit.AccountID != "team" || run.Credit.ActorID != "initiating-member" || run.Credit.HolderID != "initiating-member" || run.Credit.DiscountPercent != 60 {
		t.Fatal("owner replaced the initiating actor", run.Credit)
	}
	discount.percent = 70
	if err = service.WithBillingBasis(quote.Basis).freezeCredit(ctx, project, &d.Run{ID: "later", Contract: d.OutlineContract, CreatedAt: at}); err == nil {
		t.Fatal("stale discount basis accepted")
	}
	discount.percent = 60
	// 成员与管理员都扣名下分配额；只有接任所有者后改扣未分配积分，报价必须随之失效。
	identity.scope.Role = spaces.Owner
	changed, _, err := service.quote(ctx, project, &run, at)
	if err != nil || changed.HolderID != "" || changed.Basis == quote.Basis {
		t.Fatal("role/holder transition did not invalidate quote", changed, err)
	}
	other := base.ForScope(ports.SpaceScope{SpaceID: "team", UserID: "other-admin"})
	changed, _, err = other.quote(ctx, project, &run, at)
	if err != nil || changed.ActorID != "other-admin" || changed.Basis == quote.Basis {
		t.Fatal("another actor reused quote identity", changed, err)
	}
}
func TestFreeAndPersonalQuotesSkipTeamDiscountLookup(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC()
	start, end := at.Add(-time.Hour), at.Add(time.Hour)
	identity := &teamCreditIdentity{scope: ports.SpaceScope{Kind: spaces.TeamKind, Role: spaces.Member, Status: spaces.Active}}
	discounts := &teamCreditDiscount{err: errors.New("must not load team discount")}
	service := (&Service{Prices: teamTextPrice{c.PriceRule{UnitPrice: 5, FreeEnabled: true, FreeStart: &start, FreeEnd: &end}}, Discounts: discounts, Access: identity, Resolver: identity}).ForScope(ports.SpaceScope{SpaceID: "team", UserID: "member"})
	project := d.Project{ID: "project", SpaceID: "team", OwnerID: "owner", Revision: 1, ScriptSource: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "source", Text: "中"}}}}
	quote, _, err := service.quote(ctx, project, &d.Run{Contract: d.OutlineContract}, at)
	if err != nil || !quote.Free || quote.Amount != 0 || quote.DiscountPercent != 0 || discounts.calls != 0 {
		t.Fatal("free offer loaded or recorded discount", quote, discounts.calls, err)
	}
	identity.scope.Kind, identity.scope.Role = spaces.Personal, spaces.Owner
	service.Prices = teamTextPrice{c.PriceRule{UnitPrice: 5}}
	service.scope = &ports.SpaceScope{SpaceID: "member", UserID: "member"}
	project.SpaceID, project.OwnerID = "member", "member"
	quote, _, err = service.quote(ctx, project, &d.Run{Contract: d.OutlineContract}, at)
	if err != nil || quote.DiscountPercent != 100 || quote.AccountID != "member" || quote.HolderID != "" || discounts.calls != 0 {
		t.Fatal("personal quote used team discount", quote, discounts.calls, err)
	}
}

type capturedGenerationLedger struct {
	ports.CreditLedger
	entries          map[string]c.Entry
	deducts, refunds []c.Change
	failure          error
}

func (l *capturedGenerationLedger) CreditOperation(_ context.Context, id string) (c.Entry, error) {
	if entry, ok := l.entries[id]; ok {
		return entry, nil
	}
	return c.Entry{}, fault.ErrNotFound
}
func (l *capturedGenerationLedger) Deduct(_ context.Context, in c.Change) (c.Entry, error) {
	l.deducts = append(l.deducts, in)
	if l.failure != nil {
		return c.Entry{}, l.failure
	}
	entry := c.Entry{ID: "debit", OperationID: in.OperationID, AccountID: in.AccountID, HolderID: in.HolderID, Kind: "deduct", Amount: in.Amount, Source: in.Source, Subject: in.Subject, Reason: in.Reason}
	if l.entries == nil {
		l.entries = map[string]c.Entry{}
	}
	l.entries[in.OperationID] = entry
	return entry, nil
}
func (l *capturedGenerationLedger) Refund(_ context.Context, in c.Change) (c.Entry, error) {
	l.refunds = append(l.refunds, in)
	return c.Entry{}, nil
}

type permitTaskFacts struct {
	ports.TaskStore
	task td.Task
}

func (s permitTaskFacts) Task(context.Context, string) (td.Task, error) { return s.task, nil }
func validMemberMediaCharge() (td.Task, c.Charge) {
	task := td.Task{ID: "task", SpaceID: "team", OwnerID: "transferred-owner", CreatorID: "initiator", ScopeID: "project:project", CommandID: "command", Status: td.Queued, State: td.State{ModelID: md.MockImageModelID, Operation: "image.generate", Binding: td.Binding{ConnectionID: "mock"}}}
	charge := c.Charge{ID: mediaChargeID("initiator", "project", "command"), AccountID: "team", HolderID: "initiator", ActorID: "initiator", DiscountPercent: 60, Quantity: 1, Unit: "image", Scope: "media-image", ModelID: md.MockImageModelID, ConnectionID: "mock", ScopeDigest: "input", Rule: c.PriceRule{UnitPrice: 5}, OriginalAmount: 5, Amount: 3, AcceptedAt: time.Now().UTC()}
	return task, charge
}
func TestMediaPermitUsesFrozenIdentityAndReturnsTerminalInsufficiency(t *testing.T) {
	for _, scenario := range []struct{ code, holder, actor, account, want string }{
		{"CREDIT_HOLDER_INSUFFICIENT", "initiator", "initiator", "team", "MEMBER_CREDITS_INSUFFICIENT"},
		{"CREDIT_UNALLOCATED_INSUFFICIENT", "", "initiator", "team", "TEAM_UNALLOCATED_INSUFFICIENT"},
		{"CREDIT_INSUFFICIENT", "", "initiator", "initiator", "CREDIT_INSUFFICIENT"},
	} {
		t.Run(scenario.want, func(t *testing.T) {
			task, charge := validMemberMediaCharge()
			task.SpaceID, charge.AccountID = scenario.account, scenario.account
			charge.HolderID = scenario.holder
			ledger := &capturedGenerationLedger{failure: fault.New(scenario.code, "insufficient")}
			permit := &MediaCreditPermit{Ledger: ledger, Tasks: permitTaskFacts{task: task}}
			raw, _ := json.Marshal(MediaContext{Credit: &charge})
			rejected, err := permit.ConfirmTaskSubmission(context.Background(), task, td.Snapshot{CallerContext: raw})
			if err != nil || rejected == nil || rejected.Code != scenario.want || len(ledger.deducts) != 1 {
				t.Fatal("insufficient credit became a runtime retry", rejected, err, ledger.deducts)
			}
			debit := ledger.deducts[0]
			if debit.Subject != "initiator" || debit.AccountID != scenario.account || debit.HolderID != scenario.holder {
				t.Fatal("persisted task owner changed charge identity", debit)
			}
		})
	}
}
func TestFrozenMediaChargeAndLedgerRejectChangedHolderOrDiscount(t *testing.T) {
	task, charge := validMemberMediaCharge()
	if !frozenMediaCharge(&charge, task, "project") {
		t.Fatal("valid frozen discount rejected")
	}
	modified := charge
	modified.DiscountPercent = 100
	if frozenMediaCharge(&modified, task, "project") {
		t.Fatal("discount changed without changing amount")
	}
	modified = charge
	modified.ActorID = "transferred-owner"
	if frozenMediaCharge(&modified, task, "project") {
		t.Fatal("transfer replaced the original payer")
	}
	modified = charge
	modified.HolderID = "somebody-else"
	if frozenMediaCharge(&modified, task, "project") {
		t.Fatal("charge spent another holder balance")
	}
	ledger := &capturedGenerationLedger{}
	debit := generationDebit("project", &charge)
	entry, err := ledger.Deduct(context.Background(), debit)
	if err != nil {
		t.Fatal(err)
	}
	entry.HolderID = "wrong-holder"
	ledger.entries[entry.OperationID] = entry
	permit := &MediaCreditPermit{Ledger: ledger, Tasks: permitTaskFacts{task: task}}
	raw, _ := json.Marshal(MediaContext{Credit: &charge})
	if _, err = permit.ConfirmTaskSubmission(context.Background(), task, td.Snapshot{CallerContext: raw}); err == nil {
		t.Fatal("wrong holder receipt accepted")
	}
	refund := generationRefund(charge.ID, c.Entry{ID: "original", HolderID: "original-holder", Subject: "original-actor", Amount: 3})
	if refund.HolderID != "original-holder" || refund.Subject != "original-actor" {
		t.Fatal("refund changed original identity", refund)
	}
}

func TestQuoteFileCarriesSelectedSpaceAndMatchesCreationBasis(t *testing.T) {
	ctx := context.Background()
	identity := &teamCreditIdentity{scope: ports.SpaceScope{Kind: spaces.TeamKind, Role: spaces.Member, Status: spaces.Active}}
	discount := &teamCreditDiscount{percent: 60}
	files := uploadfiles.New()
	files.Resolver, files.Access = identity, identity
	selected := ports.SpaceScope{SpaceID: "team-one", UserID: "actor"}
	source := []byte(strings.Repeat("中", 110))
	file, err := files.ForScope(selected).Upload(ctx, uploadapp.Input{Filename: "source.txt", Data: source})
	if err != nil {
		t.Fatal(err)
	}
	registry := &modelregistry.Registry{Models: map[string]ports.Model{"model": &fakeModel{}}, OptionsValue: ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "model"}}}}
	base := &Service{Files: files, Importer: scriptimport.Importer{}, Models: registry, Prices: teamTextPrice{c.PriceRule{UnitPrice: 1}}, Discounts: discount, Resolver: identity, Access: identity}
	service := base.ForScope(selected)
	settings := d.ProjectSettings{LLMModel: "model"}
	quotation, err := service.QuoteFile(ctx, file.ID, settings)
	if err != nil || quotation.AccountID != "team-one" || quotation.ActorID != "actor" || quotation.HolderID != "actor" || quotation.Amount != 1 {
		t.Fatal(quotation, err)
	}
	parsed, err := base.Importer.Parse(file.Filename, source, 1)
	if err != nil {
		t.Fatal(err)
	}
	project := d.Project{ID: "new-project", SpaceID: "team-one", OwnerID: "actor", CreatorID: "actor", Revision: 1, ScriptSource: parsed, ProjectSettings: &settings}
	actual, _, err := service.quote(ctx, project, &d.Run{Contract: d.EpisodeIndexContract}, time.Now())
	if err != nil || actual.Basis != quotation.Basis {
		t.Fatal("create-time quote changed its selected space", actual, quotation, err)
	}
	selected.SpaceID = "team-two"
	otherFile, err := files.ForScope(selected).Upload(ctx, uploadapp.Input{Filename: "source.txt", Data: source})
	if err != nil {
		t.Fatal(err)
	}
	other, err := base.ForScope(selected).QuoteFile(ctx, otherFile.ID, settings)
	if err != nil || other.AccountID != "team-two" || other.Basis == quotation.Basis {
		t.Fatal("cross-space file reused billing basis", other, err)
	}
}
