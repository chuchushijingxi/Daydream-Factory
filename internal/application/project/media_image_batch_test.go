package project

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

func imageBatchCommand(t *testing.T, s *Service, p d.Project, target MediaTarget) MediaCommand {
	t.Helper()
	options := &md.MediaOptions{Resolution: "1K"}
	quote, _, err := s.mediaQuote(context.Background(), p, md.MockImageModelID, target, "", "", options, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return MediaCommand{CommandID: identity.New("batch-image"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: target, Options: options, BillingBasis: quote.Basis}
}

type imageBatchPrices struct{}

func (imageBatchPrices) CreditPrice(context.Context, string) (c.PriceRule, uint64, string, error) {
	return c.PriceRule{UnitPrice: 5}, 1, "mock", nil
}
func (prices imageBatchPrices) MediaPrice(ctx context.Context, model string) (ports.MediaPricing, error) {
	rule, version, connection, err := prices.CreditPrice(ctx, model)
	profile := md.OfficialMediaProfile(connection, model)
	return ports.MediaPricing{Rule: rule, Version: version, ConnectionID: connection, Profile: profile, PriceDefaultResolution: profile.DefaultResolution}, err
}

type imageBatchResolver struct {
	calls int
	mediaResolver
}

func (r *imageBatchResolver) ResolveTask(ctx context.Context, model, operation string) (td.Binding, error) {
	binding, err := r.mediaResolver.ResolveTask(ctx, model, operation)
	r.calls++
	binding.ProviderConfig, _ = json.Marshal(md.MockExecution{Fail: r.calls == 2})
	return binding, err
}

func TestImageBatchChargesAndRefundsEachOriginalTaskExactlyOnce(t *testing.T) {
	s, runtime, p, _ := mediaFixture(t)
	ctx := context.Background()
	s.Prices = imageBatchPrices{}
	runtime.Service.Resolver = &imageBatchResolver{}
	s.MediaTasks.Resolver = runtime.Service.Resolver
	if _, err := s.Credits.Add(ctx, c.Change{AccountID: p.OwnerID, Subject: p.OwnerID, Amount: 20, Category: c.Gift, OperationID: identity.New("batch-test-fund"), Source: "isolated-fixture", Reason: "batch-image-test"}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Credits.Balance(ctx, p.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	commands := []MediaCommand{imageBatchCommand(t, s, p, MediaTarget{AssetID: "c"}), imageBatchCommand(t, s, p, MediaTarget{AssetID: "prop"})}
	items, err := s.StartImageBatch(ctx, p.ID, commands)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Task == nil || item.Task.Credit == nil || item.Task.Credit.Basis.Amount != 5 {
			t.Fatal("frozen item quote", item)
		}
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			if err := runtime.Service.Drive(ctx, item.Task.ID); err != nil {
				t.Fatal(err)
			}
			v, err := s.Media(ctx, p.ID, item.Task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if v.Status.Terminal() {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	for i := 0; i < 2; i++ {
		if err := mediaFixtureWorker(s).RecoverMediaCredits(ctx); err != nil {
			t.Fatal(err)
		}
		if err := mediaFixtureWorker(s).RecoverImageFills(ctx); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := s.Media(ctx, p.ID, items[0].Task.ID)
	second, _ := s.Media(ctx, p.ID, items[1].Task.ID)
	if first.Status != td.Succeeded || first.Credit.Charged != 5 || second.Status != td.Failed || second.Credit.Refunded != 5 {
		t.Fatal("individual settlement", first.Status, first.Credit, second.Status, second.Credit)
	}
	after, err := s.Credits.Balance(ctx, p.OwnerID)
	if err != nil || after != before-5 {
		t.Fatal("batch balance", before, after, err)
	}
	if _, err := s.StartImageBatch(ctx, p.ID, commands); err != nil {
		t.Fatal(err)
	}
	stable, _ := s.Credits.Balance(ctx, p.OwnerID)
	if stable != after {
		t.Fatal("batch replay charged twice")
	}
	project, _ := s.Project(ctx, p.ID)
	if imageFileID(project, commands[0].Target) == "" || imageFileID(project, commands[1].Target) != "" {
		t.Fatal("failed image was filled")
	}
}

func TestImageBatchPersistsPerTargetAndRecoversEmptyImagesWithoutBrowser(t *testing.T) {
	s, _, p, _ := mediaFixture(t)
	ctx := context.Background()
	commands := []MediaCommand{
		imageBatchCommand(t, s, p, MediaTarget{AssetID: "c"}),
		imageBatchCommand(t, s, p, MediaTarget{AssetID: "c", LookID: "l"}),
	}
	if _, err := s.StartImageBatch(ctx, p.ID, append(commands, commands[0])); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("duplicate target must reject before acceptance", err)
	}
	items, err := s.StartImageBatch(ctx, p.ID, commands)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	for _, item := range items {
		if item.Err != nil || item.Task == nil || !item.Task.FillMissingImage {
			t.Fatal("real per-target receipt", item)
		}
		mediaFinish(t, s, *item.Task)
	}
	// 采用授权与原command一起冻结，不能经另一路由把同一编号改成单项候选。
	if _, err := s.StartMedia(ctx, p.ID, commands[0]); err == nil {
		t.Fatal("command changed adoption intent")
	}
	// 两个独立协调者模拟API/worker并发；CAS失败须重新读取，不覆盖另一目标。
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- mediaFixtureWorker(s).RecoverImageFills(ctx) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := mediaFixtureWorker(s).RecoverImageFills(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.Project(ctx, p.ID)
	if err != nil || imageFileID(after, commands[0].Target) == "" || imageFileID(after, commands[1].Target) == "" || after.Revision != p.Revision+2 {
		t.Fatal("both independent targets must be durably filled", after.Revision, err)
	}
	// 刷新/重启后的原清单重放回同一task；项目revision和图片变化不产生二次生成。
	itemsAgain, err := s.StartImageBatch(ctx, p.ID, commands)
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range itemsAgain {
		if item.Err != nil || item.Task.ID != items[i].Task.ID {
			t.Fatal("replay", item)
		}
	}
	if err := mediaFixtureWorker(s).RecoverImageFills(ctx); err != nil {
		t.Fatal(err)
	}
	stable, _ := s.Project(ctx, p.ID)
	if stable.Revision != after.Revision {
		t.Fatal("recovery performed a duplicate project write")
	}
	queries := s.Store.(ports.MediaImageFills)
	pending, err := queries.PendingImageFills(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range pending {
		if item.ProjectID == p.ID {
			t.Fatal("filled task remains pending", item)
		}
	}
}

func TestImageBatchPreservesConcurrentUploadAndStalePromptAndReportsPartialAcceptance(t *testing.T) {
	s, _, p, _ := mediaFixture(t)
	ctx := context.Background()
	commands := []MediaCommand{
		imageBatchCommand(t, s, p, MediaTarget{AssetID: "c"}),
		imageBatchCommand(t, s, p, MediaTarget{AssetID: "prop"}),
		{CommandID: identity.New("invalid-target"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: MediaTarget{AssetID: "missing"}},
	}
	items, err := s.StartImageBatch(ctx, p.ID, commands)
	if err != nil || len(items) != 3 || items[0].Task == nil || items[1].Task == nil || !errors.Is(items[2].Err, fault.ErrNotFound) {
		t.Fatal("partial receipts", items, err)
	}
	first, second := mediaFinish(t, s, *items[0].Task), mediaFinish(t, s, *items[1].Task)
	// 用户在后台补图前手动选用另一个真实已发布文件，自动补图须保留用户选择。
	p, err = s.SaveImage(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "c"}, second.Result.Files[0].FileID)
	if err != nil {
		t.Fatal(err)
	}
	revision := p.Revision
	p.Prompt(d.VisualTarget{AssetID: "prop"}).Text = "用户已更改的完整提示词"
	p.Revision++
	if err := s.Store.Commit(ctx, p, revision); err != nil {
		t.Fatal(err)
	}
	if err := mediaFixtureWorker(s).RecoverImageFills(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Project(ctx, p.ID)
	if imageFileID(after, commands[0].Target) != second.Result.Files[0].FileID || imageFileID(after, commands[1].Target) != "" || after.Revision != p.Revision {
		t.Fatal("concurrent user content overwritten", after.Revision)
	}
	for _, task := range []MediaView{first, second} {
		if saved, err := s.Media(ctx, p.ID, task.ID); err != nil || saved.Result == nil {
			t.Fatal("historical output lost", err)
		}
	}
	newCommand := imageBatchCommand(t, s, after, commands[0].Target)
	result, err := s.StartImageBatch(ctx, p.ID, []MediaCommand{newCommand})
	var conflict *fault.Error
	if err != nil || !errors.As(result[0].Err, &conflict) || conflict.Code != "ASSET_IMAGE_EXISTS" {
		t.Fatal("must skip filled image", result, err)
	}
}
