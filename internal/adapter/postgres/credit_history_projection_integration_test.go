package postgres_test

import (
	"context"
	"testing"
	"time"

	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/project"
)

func TestCreditHistoryWaitsForAllChargeRunsAndKeepsDeletedFacts(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	account, charge := identity.New("history_chain_account"), identity.New("history_chain_charge")
	if _, err := s.Add(ctx, c.Change{AccountID: account, Category: c.Gift, Amount: 100, OperationID: identity.New("grant"), Source: "fixture", Subject: "operator", Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	project := p.Project{ID: identity.New("history_chain_project"), SpaceID: account, CreatorID: account, OwnerID: account, Revision: 1, Runs: []p.Run{
		{ID: identity.New("run"), Credit: &c.Charge{ID: charge, AccountID: account, ActorID: account, DiscountPercent: 100}, Status: p.RunCompleted},
		{ID: identity.New("run"), Credit: &c.Charge{ID: charge, AccountID: account, ActorID: account, DiscountPercent: 100}, Status: p.RunRunning},
	}}
	if _, err := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.spaces(id,kind) VALUES($1,'personal') ON CONFLICT DO NOTHING`, account); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	debit, err := s.Deduct(ctx, c.Change{AccountID: account, Amount: 60, OperationID: "generation:" + charge + ":deduct", Source: project.ID, Subject: account, Reason: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	filter := c.HistoryFilter{Page: 1, PageSize: 10, Direction: "consumed", Category: "all"}
	history, err := s.CreditHistory(ctx, account, filter)
	if err != nil || history.Summary.Pending != 60 || history.Summary.SettledConsumed != 0 || len(history.Analysis) != 0 {
		t.Fatal("partially completed shared charge treated as settled", history, err)
	}
	project.Runs[1].Status = p.RunCompleted
	project.Revision++
	if err = s.Commit(ctx, project, project.Revision-1); err != nil {
		t.Fatal(err)
	}
	history, err = s.CreditHistory(ctx, account, filter)
	if err != nil || history.Summary.Pending != 0 || history.Summary.SettledConsumed != 60 || len(history.Analysis) != 1 || history.Analysis[0].Amount != 60 {
		t.Fatal("completed chain was stale or counted twice", history, err)
	}
	if _, err = s.Refund(ctx, c.Change{OriginalDeductionID: debit.ID, Amount: 10, OperationID: identity.New("refund"), Subject: account, Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	deleted := time.Now().UTC()
	project.DeletedAt = &deleted
	project.Revision++
	if err = s.Commit(ctx, project, project.Revision-1); err != nil {
		t.Fatal(err)
	}
	history, err = s.CreditHistory(ctx, account, filter)
	if err != nil || history.Summary.SettledConsumed != 50 || history.Summary.Refunded != 10 || history.Summary.Pending != 0 || len(history.Analysis) != 1 || history.Analysis[0].Amount != 50 {
		t.Fatal("deleted project or refund changed historical completion facts", history, err)
	}
}
