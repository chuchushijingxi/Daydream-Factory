package project

import (
	"context"
	"testing"

	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	uploadapp "drama-agent/internal/application/upload"
	c "drama-agent/internal/domain/credits"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/uploadfiles"
)

func TestScopedCreateFromFilePersistsInitialRunAtomically(t *testing.T) {
	ctx := context.Background()
	selected := ports.SpaceScope{SpaceID: "user", UserID: "user"}
	identity := &teamCreditIdentity{scope: ports.SpaceScope{Kind: spaces.Personal, Role: spaces.Owner, Status: spaces.Active}}
	files := uploadfiles.New()
	files.Resolver, files.Access = identity, identity
	file, err := files.ForScope(selected).Upload(ctx, uploadapp.Input{Filename: "source.txt", Data: []byte("完整虚构故事。")})
	if err != nil {
		t.Fatal(err)
	}
	registry := &modelregistry.Registry{Models: map[string]ports.Model{"model": &fakeModel{}}, DefaultModel: "model", OptionsValue: ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "model"}}}}
	store := scopeProjects{memory: newMemory()}
	base := &Service{Store: store, Resolver: identity, Access: identity, Files: files, Importer: scriptimport.Importer{}, Models: registry, Agent: unitAgent{}, Authorize: UserInitiatedExecution, Prices: teamTextPrice{c.PriceRule{}}, Credits: &capturedGenerationLedger{}}
	// 初始聚合与生成意图必须一次Create。
	service := base.ForScope(selected)
	settings := d.ProjectSettings{LLMModel: "model"}
	quote, err := service.QuoteFile(ctx, file.ID, settings)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.WithBillingBasis(quote.Basis).CreateFromFile(ctx, "project", file.ID, 0, CreationChoices{Settings: &settings})
	if err != nil || created.ID == "" || created.LatestRun() == nil || created.LatestRun().Status != d.RunRunning || created.LatestRun().Credit.ActorID != "user" {
		t.Fatal(created, err)
	}
	saved, err := store.Snapshot(ctx, created.ID)
	if err != nil || saved.SourceFileID != file.ID || saved.LatestRun().Credit == nil {
		t.Fatal("initial intent not persisted atomically", saved, err)
	}
	failedBase := *base
	failedBase.Agent = nil
	failed, err := failedBase.ForScope(selected).CreateFromFile(ctx, "failed intent", file.ID, 0, CreationChoices{})
	if err != nil || failed.LatestRun() == nil || failed.LatestRun().Status != d.RunFailed || failed.LatestRun().ErrorCode != "MODEL_UNAVAILABLE" {
		t.Fatal("initial failure was not saved", failed, err)
	}
	saved, err = store.Snapshot(ctx, failed.ID)
	if err != nil || saved.LatestRun().ErrorCode != "MODEL_UNAVAILABLE" {
		t.Fatal(saved, err)
	}
}
