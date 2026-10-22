package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	u "drama-agent/internal/application/modelusage"
	app "drama-agent/internal/application/spaces"
	"drama-agent/internal/domain/billing"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	p "drama-agent/internal/domain/project"
)

func TestTeamUsagePGFrozenActorSpaceAndOverview(t *testing.T) {
	f := newTeamPG(t, 4)
	ctx := context.Background()
	f.join(t, f.one)
	f.join(t, f.two)
	if _, e := f.service.Allocate(ctx, f.owner, f.team.ID, f.one.UserID, app.AllocationInput{CommandID: identity.New("allocate"), Amount: 100}); e != nil {
		t.Fatal(e)
	}
	textID, mediaID, taskID := identity.New("text_charge"), identity.New("media_charge"), identity.New("task")
	at := time.Now().UTC().Truncate(time.Microsecond)
	credit := &c.Charge{ID: textID, AccountID: f.team.ID, HolderID: f.one.UserID, ActorID: f.one.UserID, AcceptedAt: at, Amount: 50, OriginalAmount: 50, DiscountPercent: 100}
	project := p.Project{ID: identity.New("project"), SpaceID: f.team.ID, OwnerID: f.two.UserID, CreatorID: f.two.UserID, Revision: 1, Title: "PRIVATE_TITLE", Original: []byte("PRIVATE_ORIGINAL"), Runs: []p.Run{{ID: textID, Credit: credit, Model: "fixture-language", Contract: p.CatalogExtractionContract, Status: p.RunCompleted, CreatedAt: at, Calls: []p.Call{{ID: identity.New("call"), Request: json.RawMessage(`{"private":"PRIVATE_REQUEST"}`), Response: "PRIVATE_RESPONSE", CreatedAt: at, Usage: billing.Usage{Known: true, Input: 1, Output: 1}, CostMicroCNY: 7}}}}}
	if e := f.store.Create(ctx, project); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.Deduct(ctx, c.Change{AccountID: f.team.ID, HolderID: f.one.UserID, Amount: 50, OperationID: "generation:" + textID + ":deduct", Source: project.ID, Subject: f.one.UserID, Reason: "generation"}); e != nil {
		t.Fatal(e)
	}
	input, _ := json.Marshal(map[string]any{"callerContext": map[string]any{"credit": c.Charge{ID: mediaID, AccountID: f.team.ID, HolderID: f.one.UserID, ActorID: f.one.UserID, Amount: 20, AcceptedAt: at, DiscountPercent: 100}}, "input": map[string]string{"prompt": "PRIVATE_PROMPT"}})
	payload := json.RawMessage(`{"operation":"image.generate","modelId":"fixture-image","submissions":[{"id":"fixture-call","startedAt":"2026-10-20T00:00:00Z","disposition":"accepted","receipt":{"billing":{"status":"known","knownMicroCny":3},"body":"PRIVATE_PROVIDER"}}],"result":{"files":[{"id":"output","contentType":"image/png"}]}}`)
	if _, e := f.store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.tasks(id,scope_id,command_id,status,revision,input_snapshot,payload,space_id,owner_id,creator_id) VALUES($1,$2,$1,'succeeded',1,$3,$4,$5,$6,$7)`, taskID, "project:"+project.ID, input, payload, f.team.ID, f.two.UserID, f.one.UserID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.Deduct(ctx, c.Change{AccountID: f.team.ID, HolderID: f.one.UserID, Amount: 20, OperationID: "generation:" + mediaID + ":deduct", Source: project.ID, Subject: f.one.UserID, Reason: "generation"}); e != nil {
		t.Fatal(e)
	}
	project.OwnerID = f.owner.UserID
	// 模拟内容转交仍只改负责人；冻结发起人始终为one，原创建人始终为two。
	if _, e := f.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.projects SET owner_id=$2 WHERE id=$1`, project.ID, project.OwnerID); e != nil {
		t.Fatal(e)
	}
	for _, view := range []string{"tasks", "calls"} {
		result, e := f.store.ModelUsage(ctx, u.Filter{View: view, UserID: f.one.UserID, SpaceID: f.team.ID, Sort: "asc", Page: 1, PageSize: 20})
		if e != nil || result.Total != 2 || result.Stats.KnownCostMicroCNY != 10 {
			t.Fatal("frozen actor/space grouping", view, result, e)
		}
		for _, record := range result.Items {
			if record.UserID != f.one.UserID || record.SpaceID != f.team.ID || record.SpaceName != f.team.Name || record.SpaceKind != "team" {
				t.Fatal("usage attribution followed owner or creator", record)
			}
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "PRIVATE_") {
			t.Fatal("usage leaked body", string(raw))
		}
		hidden, e := f.store.ModelUsage(ctx, u.Filter{View: view, UserID: f.one.UserID, SpaceID: f.owner.UserID, Sort: "asc", Page: 1, PageSize: 20})
		if e != nil || hidden.Total != 0 {
			t.Fatal("space filter crossed spaces", hidden, e)
		}
	}
	f.at = time.Now().UTC().Add(time.Millisecond)
	overview, e := f.service.Overview(ctx, f.owner, f.team.ID, "7d")
	if e != nil || overview.Allocated != 30 || overview.Unallocated != 500 || overview.Team.Balance != 530 || overview.Team.Balance != overview.Allocated+overview.Unallocated {
		t.Fatal("overview allocation totals", overview, e)
	}
	for _, member := range overview.Members {
		switch member.UserID {
		case f.one.UserID:
			if member.Credits != 70 || member.HolderBalance != 30 || member.Images != 1 {
				t.Fatal("member consumption/holder projection", member)
			}
		case f.two.UserID:
			if member.Credits != 0 {
				t.Fatal("text spend incorrectly attributed to project creator", member)
			}
		}
	}
}
