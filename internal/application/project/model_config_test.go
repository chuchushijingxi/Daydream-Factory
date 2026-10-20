package project

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
)

type configMemory struct {
	mu      sync.Mutex
	gate    chan struct{}
	cfg     md.ModelConfig
	failure bool
}

func newConfigMemory() *configMemory {
	return &configMemory{gate: make(chan struct{}, 1), cfg: md.DefaultModelConfig()}
}

func (m *configMemory) ModelConfigSnapshot(context.Context) (md.ModelConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, _ := json.Marshal(m.cfg)
	var v md.ModelConfig
	_ = json.Unmarshal(raw, &v)
	return v, nil
}

func (m *configMemory) ModelConfigCommit(ctx context.Context, v md.ModelConfig, rev uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if m.failure {
		return errors.New("injected commit failure")
	}
	if m.cfg.Revision != rev {
		return fault.ErrConflict
	}
	m.cfg = v
	return nil
}

func (m *configMemory) AcquireModelConfig(ctx context.Context) (ports.Lease, error) {
	select {
	case m.gate <- struct{}{}:
		return ports.Lease{Context: ctx, Release: func() { <-m.gate }}, nil
	case <-ctx.Done():
		return ports.Lease{}, ctx.Err()
	}
}

// Cipher protocol fixture only; AES-GCM itself is tested in the storage adapter.
type fixtureCipher struct{}

func (fixtureCipher) Seal(s string) ([]byte, error) { return []byte("cipher:" + s), nil }

func (fixtureCipher) Open(b []byte) (string, error) {
	return strings.TrimPrefix(string(b), "cipher:"), nil
}

func enabledInput(rev uint64, key *string) modelconfig.ModelConfigInput {
	conn := md.DefaultModelConfig().Connections[0]
	in := modelconfig.ModelConfigInput{Revision: rev, Connection: modelconfig.ConnectionInput{ID: conn.ID, Name: conn.Name, Kind: conn.Kind, Protocol: conn.Protocol, BaseURL: conn.BaseURL}, APIKey: key}
	for i, m := range conn.Models {
		on := i == 0
		in.Connection.Models = append(in.Connection.Models, modelconfig.ModelEnableInput{ID: m.ID, ModelID: m.ModelID, Category: m.Category, Enabled: &on, ContextWindow: m.ContextWindow, MaxOutputTokens: m.MaxOutputTokens, Pricing: m.Pricing})
	}
	return in
}

func configFixture() (*Service, *configMemory, *fakeModel) {
	store := newMemory()
	model := &fakeModel{}
	cfg := newConfigMemory()
	c := &modelconfig.Service{Store: cfg, Projects: store, Cipher: fixtureCipher{}, Factory: func(context.Context, string, string, string, md.ConfiguredModel) (ports.Model, error) {
		return model, nil
	}}
	return &Service{Store: store, Models: c, Agent: unitAgent{}, Authorize: freeAuthorization}, cfg, model
}

func TestModelConfigurationCandidatesCredentialsAndNoCalls(t *testing.T) {
	s, store, model := configFixture()
	ctx := context.Background()
	key := "dummy-new-key"
	c := configService(s)
	view, err := c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, enabledInput(1, &key))
	if err != nil || !view.Connections[0].Models[0].Available || !view.Connections[0].CredentialConfigured {
		t.Fatal(view, err)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), key) || strings.Contains(string(raw), "apiKey") {
		t.Fatal("credential leaked in view")
	}
	options, err := s.AvailableModels(ctx)
	if err != nil || len(options.Analysis) != 1 || len(options.Image) != 0 || len(options.Video) != 0 {
		t.Fatal(options, err)
	}
	off := false
	input := enabledInput(view.Revision, nil)
	input.Connection.Models[0].Enabled = &off
	view, err = c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, input)
	if err != nil || view.Connections[0].Models[0].Available || !view.Connections[0].CredentialConfigured {
		t.Fatal(view, err)
	}
	config, _ := store.ModelConfigSnapshot(ctx)
	if string(config.Connections[0].Secret) != "cipher:"+key {
		t.Fatal("toggle lost stored credential")
	}
	options, err = s.AvailableModels(ctx)
	if err != nil || len(options.Analysis) != 0 {
		t.Fatal(options, err)
	}
	input = enabledInput(view.Revision, nil)
	input.Connection.BaseURL = "https://another.invalid"
	_, err = c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, input)
	expectCode(t, err, "OFFICIAL_CONNECTION_FIXED")
	if model.count != 0 {
		t.Fatal("configuration operation called model")
	}
}

func TestModelConfigurationCASRollbackAndCancellation(t *testing.T) {
	s, store, _ := configFixture()
	ctx := context.Background()
	key := "dummy"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, enabledInput(1, &key))
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, fault.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal(ok, conflicts)
	}
	before, _ := store.ModelConfigSnapshot(ctx)
	store.failure = true
	input := enabledInput(before.Revision, &key)
	input.Connection.Models[0].Alias = "提交故障测试别名"
	if _, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, input); e == nil {
		t.Fatal("fault ignored")
	}
	after, _ := store.ModelConfigSnapshot(ctx)
	if after.Revision != before.Revision || after.Connections[0].BaseURL != before.Connections[0].BaseURL || string(after.Connections[0].Secret) != string(before.Connections[0].Secret) {
		t.Fatal("partial config commit")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(cancelled, input); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestModelConfigurationTaskStartupInterlockAndFixedSelection(t *testing.T) {
	s, store, model := configFixture()
	ctx := context.Background()
	key := "dummy"
	if _, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, enabledInput(1, &key)); e != nil {
		t.Fatal(e)
	}
	p := d.Project{ID: "config-race", Revision: 1, ScriptSource: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "s", Text: "虚构材料"}}}, Assets: []d.Asset{}, Runs: []d.Run{}}
	if e := s.Store.Create(ctx, p); e != nil {
		t.Fatal(e)
	}
	p, e := s.SaveSettings(ctx, p.ID, 1, d.ProjectSettings{LLMModel: md.DeepSeekModelID})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	off := false
	wg.Add(2)
	go func() { defer wg.Done(); _, e := s.Start(ctx, p.ID, p.Revision); results <- e }()
	go func() {
		defer wg.Done()
		_, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, func() modelconfig.ModelConfigInput {
			v := enabledInput(2, nil)
			v.Connection.Models[0].Enabled = &off
			return v
		}())
		results <- e
	}()
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		} else {
			var command *fault.Error
			if !errors.Is(err, fault.ErrModelUnavailable) && (!errors.As(err, &command) || command.Code != "RUN_ACTIVE") {
				t.Fatal(err)
			}
		}
	}
	if ok != 1 {
		t.Fatal("startup and disable both admitted", ok)
	}
	stored, _ := s.Store.Snapshot(ctx, p.ID)
	config, _ := store.ModelConfigSnapshot(ctx)
	if stored.Active() && !config.Connections[0].Models[0].Enabled {
		t.Fatal("active task with changed connection")
	}
	if stored.ProjectSettings.LLMModel != md.DeepSeekModelID || model.count != 0 {
		t.Fatal("selection changed or model called")
	}
	if stored.Active() {
		_, err := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, enabledInput(config.Revision, nil))
		expectCode(t, err, "RUN_ACTIVE")
	}
}

func TestModelConfigurationEmptyVisualStillFailsWithoutConfigRead(t *testing.T) {
	s, store, _ := configFixture()
	ctx := context.Background()
	store.cfg.Connections[0].Models[0].Enabled = true
	store.cfg.Connections[0].Secret = []byte("broken")
	configService(s).Cipher = nil
	p := d.Project{ID: "empty-target-config", Revision: 1, ScriptSource: d.ScriptSource{Version: 1}, Assets: []d.Asset{}, Runs: []d.Run{{ID: "p3", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "completed"}}}
	if e := s.Store.Create(ctx, p); e != nil {
		t.Fatal(e)
	}
	result, e := s.StartVisual(ctx, p.ID, 1)
	if e != nil || result.LatestRun().Status != "failed" || result.LatestRun().Error != d.EmptyVisualError || len(result.LatestRun().Calls) != 0 {
		t.Fatal(result, e)
	}
}

func TestSharedDeepSeekConnectionAndIndependentModelSwitches(t *testing.T) {
	s, store, _ := configFixture()
	ctx := context.Background()
	key := "dummy-shared-key"
	input := enabledInput(1, &key)
	on := true
	input.Connection.Models[1].Enabled = &on
	view, err := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, input)
	if err != nil || len(view.Connections[0].Models) != 2 || !view.Connections[0].Models[0].Available || !view.Connections[0].Models[1].Available {
		t.Fatal(view, err)
	}
	created := map[string]bool{}
	flash, pro := &fakeModel{}, &fakeModel{}
	configService(s).Factory = func(_ context.Context, k, url, protocol string, profile md.ConfiguredModel) (ports.Model, error) {
		id := profile.ModelID
		if k != key || url != md.DeepSeekBaseURL {
			t.Fatal("models received different credentials")
		}
		created[id] = true
		if id == md.DeepSeekProModelID {
			return pro, nil
		}
		return flash, nil
	}
	bound, err := availableBindings(s, ctx)
	if err != nil || bound[md.DeepSeekProModelID].Model != pro || bound[md.DeepSeekModelID].Model != flash || len(created) != 2 {
		t.Fatal("incorrect model binding", err)
	}
	off := false
	input = enabledInput(view.Revision, nil)
	input.Connection.Models[0].Enabled = &off
	input.Connection.Models[1].Enabled = &on
	view, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, input)
	opts, err2 := s.AvailableModels(ctx)
	if err != nil || err2 != nil || view.Connections[0].Models[0].Available || !view.Connections[0].Models[1].Available || len(opts.Analysis) != 1 || opts.Analysis[0].ID != md.DeepSeekProModelID {
		t.Fatal(view, opts, err, err2)
	}
	config, _ := store.ModelConfigSnapshot(ctx)
	if string(config.Connections[0].Secret) != "cipher:"+key {
		t.Fatal("model toggle changed the shared key")
	}
	bound, err = availableBindings(s, ctx)
	if err != nil || bound[md.DeepSeekModelID].Model != nil || bound[md.DeepSeekProModelID].Model != pro {
		t.Fatal("disabled model still bound", err)
	}
	bad := enabledInput(view.Revision, nil)
	bad.Connection.Models[1].ModelID = md.DeepSeekModelID
	if _, err = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, bad); err == nil {
		t.Fatal("duplicate model accepted", err)
	}
	if flash.count != 0 || pro.count != 0 {
		t.Fatal("saving or binding called provider")
	}
}

func TestCustomConnectionAliasMixedCategoriesAndStableReferences(t *testing.T) {
	s, store, _ := configFixture()
	ctx := context.Background()
	on := true
	key := "dummy-gateway"
	price := billing.TokenPricing{Input: .5, Output: 1.5, CacheHitHundredths: 5}
	in := modelconfig.ModelConfigInput{Revision: 1, APIKey: &key, Connection: modelconfig.ConnectionInput{Name: "测试中转", Kind: "gateway", Protocol: "openai-chat", BaseURL: "https://gateway.example.test/v1", Models: []modelconfig.ModelEnableInput{
		{ModelID: "upstream-text", Alias: "编剧模型", Category: "analysis", Enabled: &on, ContextWindow: 8192, MaxOutputTokens: 1024, Pricing: &price},
		{ModelID: "upstream-image", Alias: "画面", Category: "image", Enabled: &on},
		{ModelID: "upstream-video", Alias: "镜头", Category: "video", Enabled: &on},
	}}}
	v, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if e != nil || len(v.Connections) != len(md.DefaultModelConfig().Connections)+1 {
		t.Fatal(v, e)
	}
	conn := v.Connections[len(v.Connections)-1]
	id := conn.Models[0].ID
	if id == "upstream-text" || conn.Models[0].Name != "编剧模型" || !conn.Models[0].Available || conn.Models[1].Available || conn.Models[2].Available {
		t.Fatal("identity/alias/category availability incorrect", conn)
	}
	options, e := s.AvailableModels(ctx)
	if e != nil || len(options.Analysis) != 1 || options.Analysis[0].ID != id || options.Analysis[0].Name != "编剧模型" || len(options.Image) != 1 || options.Image[0].ID != conn.Models[1].ID || len(options.Video) != 1 || options.Video[0].ID != conn.Models[2].ID || len(options.Catalog) != configuredDefaultModelCount()+3 {
		t.Fatal(options, e)
	}
	called := false
	configService(s).Factory = func(_ context.Context, k, url, protocol string, m md.ConfiguredModel) (ports.Model, error) {
		if k != key || url != in.Connection.BaseURL || protocol != "openai-chat" || m.ModelID != "upstream-text" {
			t.Fatal("connection mapping incorrect")
		}
		called = true
		return &fakeModel{window: 8192, output: 1024}, nil
	}
	bound, e := availableBindings(s, ctx)
	if e != nil || !called || bound[id].Pricing.Input != .5 || bound[id].Model == nil {
		t.Fatal("custom model failed binding", e)
	}
	p := d.Project{ID: "custom-fixed", Revision: 1, ScriptSource: d.ScriptSource{Version: 1}, Assets: []d.Asset{}, Runs: []d.Run{}}
	if e = s.Store.Create(ctx, p); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveSettings(ctx, p.ID, 1, d.ProjectSettings{LLMModel: id}); e != nil {
		t.Fatal(e)
	}
	in.Revision = v.Revision
	in.APIKey = nil
	in.Connection.ID = conn.ID
	for i := range in.Connection.Models {
		in.Connection.Models[i].ID = conn.Models[i].ID
	}
	in.Connection.Models[0].Alias = "新别名"
	off := false
	in.Connection.Models[0].Enabled = &off
	v, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	options, e = s.AvailableModels(ctx)
	if e != nil || len(options.Analysis) != 0 || options.Catalog[len(options.Catalog)-3].Name != "新别名" {
		t.Fatal("disabled alias lost", options, e)
	}
	stored, _ := s.Store.Snapshot(ctx, p.ID)
	if stored.ProjectSettings.LLMModel != id {
		t.Fatal("renamed alias changed fixed selection")
	}
	if _, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Delete(ctx, conn.ID, v.Revision); e == nil {
		t.Fatal("referenced connection deleted")
	}
	in.Revision = v.Revision
	in.Connection.Models[0].ModelID = "other-model"
	if _, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, in); e == nil {
		t.Fatal("referenced upstream ID changed")
	}
	cfg, _ := store.ModelConfigSnapshot(ctx)
	if string(cfg.Connections[len(cfg.Connections)-1].Secret) != "cipher:"+key {
		t.Fatal("shared credential lost")
	}
}

func TestUnreferencedConnectionDeleteAndMissingProfileAreExplicit(t *testing.T) {
	s, _, _ := configFixture()
	ctx := context.Background()
	on := true
	key := "dummy"
	v, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, modelconfig.ModelConfigInput{Revision: 1, APIKey: &key, Connection: modelconfig.ConnectionInput{Name: "待配置", Kind: "gateway", Protocol: "openai-chat", BaseURL: "https://example.test/v1", Models: []modelconfig.ModelEnableInput{{ModelID: "custom", Category: "analysis", Enabled: &on}}}})
	if e != nil || v.Connections[len(v.Connections)-1].Models[0].Available || v.Connections[len(v.Connections)-1].Models[0].Reason == "" {
		t.Fatal(v, e)
	}
	if _, e = configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Delete(ctx, v.Connections[len(v.Connections)-1].ID, v.Revision-1); !errors.Is(e, fault.ErrConflict) {
		t.Fatal(e)
	}
	next, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Delete(ctx, v.Connections[len(v.Connections)-1].ID, v.Revision)
	if e != nil || len(next.Connections) != len(md.DefaultModelConfig().Connections) {
		t.Fatal(next, e)
	}
}

func configuredDefaultModelCount() int {
	count := 0
	for _, conn := range md.DefaultModelConfig().Connections {
		count += len(conn.Models)
	}
	return count
}

func configService(s *Service) *modelconfig.Service { return s.Models.(*modelconfig.Service) }

// 单独解析候选模型验证端口；没有项目 Service 复制或生产全目录绑定旁路。
func availableBindings(s *Service, ctx context.Context) (map[string]ports.ResolvedModel, error) {
	opts, err := s.AvailableModels(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]ports.ResolvedModel{}
	for _, option := range opts.Analysis {
		bound, err := s.Models.Resolve(ctx, option.ID)
		if err != nil {
			return nil, err
		}
		out[option.ID] = bound
	}
	return out, nil
}
func modelForTest(s *Service, r *d.Run) ports.Model {
	bound, err := s.resolve(context.Background(), r.Model)
	if err != nil {
		return nil
	}
	return bound.Model
}

func TestModelProviderResolvesOnlySelectedModelWithoutCalls(t *testing.T) {
	s, store, model := configFixture()
	store.cfg.Connections[0].Secret = []byte("cipher:fixture-only")
	for i := range store.cfg.Connections[0].Models {
		store.cfg.Connections[0].Models[i].Enabled = true
	}
	constructions := 0
	configService(s).Factory = func(_ context.Context, _, _, _ string, profile md.ConfiguredModel) (ports.Model, error) {
		constructions++
		if profile.ID != md.DeepSeekModelID {
			t.Fatal("selected task constructed another model", profile.ID)
		}
		return model, nil
	}
	resolved, err := s.Models.Resolve(context.Background(), md.DeepSeekModelID)
	if err != nil || resolved.ID != md.DeepSeekModelID || resolved.Model != model || constructions != 1 || model.count != 0 {
		t.Fatal("model provider selection or no-call boundary", resolved, err, constructions)
	}
}
