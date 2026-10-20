package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
)

// Memory is deliberately test-only and runs the same snapshot/commit contracts as PG.
type memory struct {
	mu     sync.Mutex
	data   map[string]d.Project
	locked map[string]bool
}

func newMemory() *memory { return &memory{data: map[string]d.Project{}, locked: map[string]bool{}} }

func (m *memory) Create(ctx context.Context, p d.Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[p.ID]; ok {
		return fault.ErrConflict
	}
	m.data[p.ID] = d.Clone(p)
	return nil
}

func (m *memory) Snapshot(ctx context.Context, id string) (d.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.data[id]
	if !ok {
		return p, fault.ErrNotFound
	}
	return d.Clone(p), nil
}

func (m *memory) Commit(ctx context.Context, p d.Project, rev uint64) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data[p.ID].Revision != rev {
		return fault.ErrConflict
	}
	m.data[p.ID] = d.Clone(p)
	return nil
}

func (m *memory) List(context.Context) ([]d.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := []d.Project{}
	for _, v := range m.data {
		p = append(p, d.Clone(v))
	}
	return p, nil
}

func (m *memory) Acquire(ctx context.Context, id string) (ports.Lease, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked[id] {
		return ports.Lease{}, false, nil
	}
	m.locked[id] = true
	return ports.Lease{Context: ctx, Release: func() { m.mu.Lock(); delete(m.locked, id); m.mu.Unlock() }}, true, nil
}

type fakeModel struct {
	generate      func(ports.Request) (ports.Response, error)
	catalogVisual bool
	count         int
	window        int
	output        int
}

// Unit-test driver only. Production iteration is exercised with the real Eino
// 适配器由 HTTP/模型集成与子进程测试覆盖。
type unitAgent struct{}

func (unitAgent) Run(ctx context.Context, session ports.AgentSession) error {
	messages := append([]ports.Message{}, session.Dialogue...)
	for {
		response, err := session.Generate(ctx, messages)
		if err != nil {
			return err
		}
		assistant := ports.Message{Role: "assistant", Content: response.Text, Reasoning: response.Reasoning}
		for _, a := range response.Actions {
			assistant.Actions = append(assistant.Actions, d.Action{ID: a.ID, Name: a.Name, Arguments: a.Arguments})
		}
		messages = append(messages, assistant)
		for _, action := range response.Actions {
			result, err := session.Execute(ctx, action.ID)
			if err != nil {
				return err
			}
			messages = append(messages, ports.Message{Role: "tool", ToolCallID: action.ID, Content: result.Text})
			var feedback struct {
				OK bool `json:"ok"`
			}
			if json.Unmarshal([]byte(result.Text), &feedback) != nil || !feedback.OK {
				return fault.ErrInvalid
			}
			if result.Finished {
				return nil
			}
		}
	}
}

func (m *fakeModel) Capacity() (int, int) {
	if m.window > 0 {
		return m.window, m.output
	}
	return 1000000, 393216
}

func (m *fakeModel) Generate(ctx context.Context, r ports.Request, observe func(json.RawMessage) error) (ports.Response, error) {
	b, _ := json.Marshal(r)
	if err := observe(b); err != nil {
		return ports.Response{}, err
	}
	if m.catalogVisual {
		if reply, ok := modelregistry.FreeVisualReply(r); ok {
			return reply, nil
		}
	}
	m.count++
	return m.generate(r)
}

func fixture(t *testing.T, model *fakeModel) (*Service, d.Project) {
	t.Helper()
	model.catalogVisual = true
	store := newMemory()
	p := d.Project{ID: "p", Revision: 1, ScriptSource: d.ScriptSource{Version: 1, Spans: []d.SourceSpan{{ID: "s1", Text: "祖父坐在工具间。"}, {ID: "s2", Text: "另一把铜钥匙。"}}}, Assets: []d.Asset{}, Runs: []d.Run{}}
	if err := store.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	s := &Service{Store: store, Agent: unitAgent{}, Authorize: freeAuthorization, Models: &modelregistry.Registry{Model: model}}
	p, err := s.Start(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func response(text string) ports.Response {
	return ports.Response{Actions: []d.Action{{ID: "save", Name: "save_catalog", Arguments: text, Status: "pending"}, {ID: "finish", Name: "finish_batch", Arguments: `{}`, Status: "pending"}}, Raw: json.RawMessage(`{"known":true}`), FinishReason: "tool_calls", Usage: billing.Usage{Input: 100, Output: 20, Known: true}}
}

func TestTwoPromptsAdoptNormalizePreserveIDsAndRecordActualResults(t *testing.T) {
	m := &fakeModel{}
	m.generate = func(r ports.Request) (ports.Response, error) {
		if m.count == 1 {
			return response("```json\n{\"assets\":[{\"kind\":\"character\",\"name\":\"祖父\",\"aliases\":null,\"sourceIds\":[\"s1\"]}]}\n```"), nil
		}
		var input struct {
			Assets []d.Asset `json:"assets"`
		}
		if json.Unmarshal([]byte(r.Input), &input) != nil || len(input.Assets) != 1 {
			t.Fatal("correction lacks saved catalog")
		}
		if strings.Contains(r.Input, "ports.Request") || strings.Contains(r.Input, "response") || strings.Contains(r.Input, "calls") {
			t.Fatal("action history leaked into correction")
		}
		input.Assets[0].Description = "姓名未说明"
		input.Assets = append(input.Assets, d.Asset{Kind: "prop", Name: "不同的钥匙", SourceIDs: []string{"s1", "s2"}})
		b, _ := json.Marshal(CatalogChangeSet{Assets: catalogInputs(input.Assets)})
		return response(string(b)), nil
	}
	s, p := fixture(t, m)
	if err := s.Execute(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Store.Snapshot(context.Background(), p.ID)
	r := got.LatestRun()
	if m.count != 2 || r.Status != "completed" || len(got.Assets) != 2 || r.Calls[0].After[0].ID != r.Calls[1].After[0].ID || got.UsedMicroCNY() != 720 {
		t.Fatalf("invalid result: %+v", got)
	}
	if len(r.Calls[0].Request) == 0 || len(r.Calls[0].Response) == 0 || r.Calls[0].Before == nil {
		t.Fatal("incomplete call evidence")
	}
}

func TestInvalidWritesAreAtomicAndDoNotRequireLiteralNames(t *testing.T) {
	source := d.ScriptSource{Spans: []d.SourceSpan{{ID: "s1"}, {ID: "s2"}}}
	current := []d.Asset{{ID: "a", Kind: "prop", Name: "铜钥匙", SourceIDs: []string{"s1"}}}
	valid := CatalogChangeSet{Assets: catalogInputs([]d.Asset{{ID: "a", Kind: "prop", Name: "另一把不同实物的钥匙", SourceIDs: []string{"s1", "s2"}}, {Kind: "prop", Name: "另一把不同实物的钥匙", SourceIDs: []string{"s2"}}})}
	if v, e := adoptTestDraft(valid, current, source); e != nil || len(v) != 2 {
		t.Fatalf("cross-span paraphrase or same name rejected: %v", e)
	}
	for _, bad := range []d.Asset{{ID: "foreign", Kind: "prop", Name: "钥匙", SourceIDs: []string{"s1"}}, {Kind: "prop", Name: "钥匙", SourceIDs: []string{"foreign"}}, {Kind: "prop", Name: "钥匙", SourceIDs: nil}, {ID: "a", Kind: "prop", Name: "钥匙", SourceIDs: []string{"s1"}}} {
		_, err := adoptTestDraft(CatalogChangeSet{Assets: catalogInputs([]d.Asset{current[0], bad})}, current, source)
		if !errors.Is(err, fault.ErrInvalid) {
			t.Fatal("invalid write accepted", bad)
		}
		if current[0].Name != "铜钥匙" {
			t.Fatal("rejection mutated current")
		}
	}
}

func TestFormatFailureSeparatedFromProviderFailure(t *testing.T) {
	for _, test := range []struct {
		name, text, finish, code string
		err                      error
	}{{"null", "{\"assets\":null}", "tool_calls", "AGENT_FAILED", nil}, {"unknown-field", "{\"assets\":[],\"issue\":{}}", "tool_calls", "AGENT_FAILED", nil}, {"truncated", "{\"assets\":[]}", "length", "MODEL_OUTPUT_TRUNCATED", nil}, {"provider", "", "", "MODEL_PROVIDER_FAILED", errors.New("connection lost")}} {
		t.Run(test.name, func(t *testing.T) {
			m := &fakeModel{generate: func(ports.Request) (ports.Response, error) {
				r := response(test.text)
				r.FinishReason = test.finish
				if test.err != nil {
					r = ports.Response{}
				}
				return r, test.err
			}}
			s, p := fixture(t, m)
			_ = s.Execute(context.Background(), p.ID)
			got, _ := s.Store.Snapshot(context.Background(), p.ID)
			if got.LatestRun().ErrorCode != test.code || len(got.Assets) != 0 || m.count != 1 {
				t.Fatal("failure mislabeled", got)
			}
			if test.err != nil && got.UsedMicroCNY() == 0 {
				t.Fatal("unknown fee released")
			}
		})
	}
}

func TestSavedResponseResumesWithoutRepeatingCallAndUnknownDoesNotResume(t *testing.T) {
	for _, status := range []string{"received", "pending", "unknown"} {
		t.Run(status, func(t *testing.T) {
			m := &fakeModel{generate: func(r ports.Request) (ports.Response, error) {
				var input struct {
					Assets []d.Asset `json:"assets"`
				}
				json.Unmarshal([]byte(r.Input), &input)
				b, _ := json.Marshal(CatalogChangeSet{Assets: catalogInputs(input.Assets)})
				return response(string(b)), nil
			}}
			s, p := fixture(t, m)
			p.LatestRun().End = len(p.ScriptSource.Spans)
			p.LatestRun().Calls = append(p.LatestRun().Calls, d.Call{ID: "c", Status: d.CallStatus(status), Stage: "extract", End: len(p.ScriptSource.Spans), Actions: response(`{"assets":[{"kind":"character","name":"祖父","sourceIds":["s1"]}]}`).Actions, FinishReason: "tool_calls", ReservedMicroCNY: 999, Before: []d.Asset{}})
			p.Revision++
			s.Store.Commit(context.Background(), p, p.Revision-1)
			if err := s.Execute(context.Background(), p.ID); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Store.Snapshot(context.Background(), p.ID)
			if status == "received" {
				if m.count != 1 || got.LatestRun().Status != "completed" || len(got.Assets) != 1 {
					t.Fatal("known response charged again", got)
				}
			} else if m.count != 0 || got.LatestRun().ErrorCode != "MODEL_RESULT_UNKNOWN" {
				t.Fatal("unknown request resent", got)
			}
		})
	}
}

func TestBudgetPreventsExternalRequest(t *testing.T) {
	m := &fakeModel{generate: func(ports.Request) (ports.Response, error) { t.Fatal("over budget call"); return ports.Response{}, nil }}
	s, p := fixture(t, m)
	p.LimitMicroCNY = 1
	p.Revision++
	s.Store.Commit(context.Background(), p, p.Revision-1)
	if err := s.Execute(context.Background(), p.ID); err != nil && !errors.Is(err, fault.ErrBudget) {
		t.Fatal(err)
	}
	got, _ := s.Store.Snapshot(context.Background(), p.ID)
	if m.count != 0 || got.LatestRun().ErrorCode != "BUDGET_EXHAUSTED" {
		t.Fatal("quota not protected")
	}
}

func TestExplicitRemainingBudgetSuppliesAffordableOutputWithoutTruncatingInput(t *testing.T) {
	m := &fakeModel{}
	s, p := fixture(t, m)
	p.LimitMicroCNY = 100000
	p.Revision++
	if err := s.Store.Commit(context.Background(), p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	m.generate = func(req ports.Request) (ports.Response, error) {
		current, _ := s.Store.Snapshot(context.Background(), p.ID)
		call := current.LatestRun().Calls[len(current.LatestRun().Calls)-1]
		tools, _ := json.Marshal(req.Tools)
		if req.MaxOutputTokens <= 0 || req.MaxOutputTokens >= 393216 || current.UsedMicroCNY() > current.LimitMicroCNY || call.ReservedMicroCNY != int64(inputSize(req)+len(tools))*2+int64(req.MaxOutputTokens)*8 {
			t.Fatal("output does not match the available allowance")
		}
		var input struct {
			ScriptSource []d.SourceSpan `json:"source"`
		}
		if json.Unmarshal([]byte(req.Input), &input) != nil || len(input.ScriptSource) != len(p.ScriptSource.Spans) {
			t.Fatal("budget allocation discarded source")
		}
		return response(`{"assets":[]}`), nil
	}
	if err := s.Execute(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Store.Snapshot(context.Background(), p.ID)
	if m.count != 2 || got.LatestRun().ErrorCode != "INVALID_INPUT" || got.UsedMicroCNY() != 720 {
		t.Fatal("affordable two-pass execution did not settle")
	}
}

func TestProBudgetReservationAndRecordedCostUseProPrices(t *testing.T) {
	m := &fakeModel{}
	s, p := fixture(t, m)
	ctx := context.Background()
	registry(s).
		Models = map[string]ports.Model{md.DeepSeekProModelID: m}
	p.LatestRun().Model = md.DeepSeekProModelID
	p.LimitMicroCNY = 200000
	p.Revision++
	if err := s.Store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	m.generate = func(req ports.Request) (ports.Response, error) {
		current, _ := s.Store.Snapshot(ctx, p.ID)
		call := current.LatestRun().Calls[len(current.LatestRun().Calls)-1]
		tools, _ := json.Marshal(req.Tools)
		expected := int64(inputSize(req)+len(tools))*9 + int64(req.MaxOutputTokens)*27
		if call.ReservedMicroCNY != expected || call.Pricing == nil || call.Pricing.Input != 9 || call.Pricing.Output != 27 || current.UsedMicroCNY() > p.LimitMicroCNY {
			t.Fatal("Pro allowance or price snapshot incorrect", call)
		}
		return response(`{"assets":[]}`), nil
	}
	if err := s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Store.Snapshot(ctx, p.ID)
	if m.count != 2 || got.LatestRun().ErrorCode != "INVALID_INPUT" || got.UsedMicroCNY() != 2880 {
		t.Fatal("Pro usage charged at another model's rate", got.UsedMicroCNY())
	}
}

func TestCancellationAccountsLateResponseWithoutAdopting(t *testing.T) {
	m := &fakeModel{}
	s, p := fixture(t, m)
	m.generate = func(ports.Request) (ports.Response, error) {
		latest, _ := s.Store.Snapshot(context.Background(), p.ID)
		if _, err := s.Cancel(context.Background(), p.ID, latest.Revision); err != nil {
			t.Fatal(err)
		}
		return response(`{"assets":[{"kind":"character","name":"祖父","sourceIds":["s1"]}]}`), nil
	}
	if err := s.Execute(context.Background(), p.ID); err != nil && !errors.Is(err, fault.ErrCancelled) {
		t.Fatal(err)
	}
	got, _ := s.Store.Snapshot(context.Background(), p.ID)
	if got.LatestRun().Status != "cancelled" || len(got.Assets) != 0 || got.UsedMicroCNY() != 360 {
		t.Fatal("late result escaped cancel", got)
	}
}

func TestLongTransportKeepsSourceScopeAndUsesNoOldHistory(t *testing.T) {
	// Keep a fixed material allowance after the business prompt. The complete
	// twenty-paragraph source still exceeds this simulated window and must batch.
	m := &fakeModel{window: len(CorrectPrompt) + 14000, output: 400}
	seen := map[string]int{}
	m.generate = func(req ports.Request) (ports.Response, error) {
		var input struct {
			ScriptSource []d.SourceSpan `json:"source"`
			Targets      []string       `json:"targetSpanIds"`
			Assets       []d.Asset      `json:"assets"`
		}
		if err := json.Unmarshal([]byte(req.Input), &input); err != nil {
			t.Fatal(err)
		}
		tools, _ := json.Marshal(req.Tools)
		if len(req.Input)+len(req.System)+len(tools)+128+req.MaxOutputTokens > m.window {
			t.Fatal("over capacity")
		}
		for _, id := range input.Targets {
			seen[id]++
		}
		if len(input.Assets) == 0 {
			input.Assets = []d.Asset{{Kind: "character", Name: "祖父", SourceIDs: []string{"s0"}}}
		}
		b, _ := json.Marshal(CatalogChangeSet{Assets: catalogInputs(input.Assets)})
		return response(string(b)), nil
	}
	s, p := fixture(t, m)
	p.ScriptSource.Spans = []d.SourceSpan{}
	for i := 0; i < 20; i++ {
		p.ScriptSource.Spans = append(p.ScriptSource.Spans, d.SourceSpan{ID: fmt.Sprintf("s%d", i), Text: strings.Repeat("完整原文", 150)})
	}
	p.Revision++
	s.Store.Commit(context.Background(), p, p.Revision-1)
	if err := s.Execute(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Store.Snapshot(context.Background(), p.ID)
	if got.LatestRun().Status != "completed" || m.count <= 2 {
		t.Fatalf("segmentation stopped: status=%s stage=%s cursor=%d calls=%d error=%s", got.LatestRun().Status, got.LatestRun().Stage, got.LatestRun().Cursor, m.count, got.LatestRun().Error)
	}
	for i := 0; i < 20; i++ {
		if seen[fmt.Sprintf("p%d", i+1)] != 2 {
			t.Fatal("target not processed once per prompt", i, seen)
		}
	}
}

func TestDialectMetadataNormalizationRetainsBusinessAndRejectsUnknownContent(t *testing.T) {
	text := `{"$schema":"https://json-schema.org/draft/2020-12/schema","assets":[{"kind":"character","name":"祖父","aliases":null,"sourceIds":["s1"]}]}`
	draft, err := Decode(text)
	if err != nil || len(draft.Assets) != 1 || draft.Assets[0].Name != "祖父" {
		t.Fatal("harmless metadata blocked business", err)
	}
	if _, err = Decode(`{"$schema":"https://json-schema.org/draft-07/schema","assets":[]}`); err != nil {
		t.Fatal("different metadata label was treated as a business difference", err)
	}
	if _, err = Decode(`{"$schema":{"characters":["lost"]},"assets":[]}`); err == nil {
		t.Fatal("non-metadata content silently discarded")
	}
	if _, err = Decode(`{"$schema":"https://json-schema.org/draft/2020-12/schema","assets":[],"claims":["lost"]}`); err == nil {
		t.Fatal("business content silently discarded")
	}
	if strings.Contains(OutputSchema(), `"$schema"`) {
		t.Fatal("dialect metadata still presented as output")
	}
}

func TestOneLargeParagraphUsesActualRemainingOutputCapacity(t *testing.T) {
	m := &fakeModel{window: 13000, output: 5000}
	s, p := fixture(t, m)
	p.ScriptSource.Spans = []d.SourceSpan{{ID: "s1", Text: strings.Repeat("原文", 850)}}
	req, end, err := s.request(context.Background(), p)
	tools, _ := json.Marshal(req.Tools)
	if err != nil || end != 1 || req.MaxOutputTokens <= 0 || req.MaxOutputTokens >= 5000 || inputSize(req)+len(tools)+req.MaxOutputTokens != m.window {
		t.Fatal("remaining capacity discarded", err, req.MaxOutputTokens)
	}
}

func TestSchemaDeclaresOperationallyRequiredNameAndSource(t *testing.T) {
	var shape struct {
		Properties map[string]struct {
			Items struct {
				Properties map[string]struct {
					MinLength int `json:"minLength"`
					MinItems  int `json:"minItems"`
				} `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(OutputSchema()), &shape); err != nil {
		t.Fatal(err)
	}
	fields := shape.Properties["assets"].Items.Properties
	if fields["name"].MinLength != 1 || fields["sourceIds"].MinItems != 1 {
		t.Fatal("wire schema permits missing operational information")
	}
}

func TestObjectChangesPreserveOmissionsAndRejectAmbiguousWritesAtomically(t *testing.T) {
	source := d.ScriptSource{Spans: []d.SourceSpan{{ID: "s1"}, {ID: "s2"}}}
	current := []d.Asset{
		{ID: "grandfather", Kind: "character", Name: "祖父", Description: "姓名未说明", SourceIDs: []string{"s1"}},
		{ID: "key-left", Kind: "prop", Name: "铜钥匙", SourceIDs: []string{"s1"}},
		{ID: "key-right", Kind: "prop", Name: "另一把铜钥匙", Description: "不同实物", SourceIDs: []string{"s2"}},
	}
	corrected := current[1]
	corrected.Description = "同一件实物被转交"
	got, err := adoptTestDraft(CatalogChangeSet{Assets: catalogInputs([]d.Asset{corrected})}, current, source)
	if err != nil || len(got) != 3 || got[0].Description != current[0].Description || got[2].Description != "不同实物" {
		t.Fatal("omission deleted or altered untouched objects", err, got)
	}
	// The model explicitly decides a merge. The program never merges same names.
	got, err = adoptTestDraft(CatalogChangeSet{Assets: catalogInputs([]d.Asset{corrected}), RemoveIDs: []string{"key-right"}}, current, source)
	if err != nil || len(got) != 2 || got[1].ID != "key-left" {
		t.Fatal("explicit merge not applied", err, got)
	}
	// A split retains the original ID and supplies the new object's assigned ID.
	got, err = adoptTestDraft(CatalogChangeSet{Assets: catalogInputs([]d.Asset{corrected, {Kind: "prop", Name: "铜钥匙", SourceIDs: []string{"s2"}}})}, current[:2], source)
	if err != nil || len(got) != 3 || got[1].ID != "key-left" || got[2].ID == "" || got[2].ID == "key-left" {
		t.Fatal("split lost identity", err, got)
	}
	for _, bad := range []CatalogChangeSet{
		{Assets: catalogInputs([]d.Asset{corrected}), RemoveIDs: []string{"key-left"}},
		{Assets: catalogInputs([]d.Asset{corrected}), RemoveIDs: []string{"foreign-project-id"}},
		{Assets: catalogInputs([]d.Asset{corrected, {Kind: "prop", Name: "坏引用", SourceIDs: []string{"foreign-source"}}}), RemoveIDs: []string{"key-right"}},
	} {
		if got, err = adoptTestDraft(bad, current, source); !errors.Is(err, fault.ErrInvalid) || got != nil || current[1].Description != "" || len(current) != 3 {
			t.Fatal("invalid partial changes adopted", err, got)
		}
	}
}

func TestActualChangeFeedbackCarriesIDsAndDeletedSourceWithoutDuplicatingDialogue(t *testing.T) {
	m := &fakeModel{}
	s, p := fixture(t, m)
	p.Assets = []d.Asset{{ID: "a", Kind: "character", Name: "祖父", SourceIDs: []string{"s1"}}, {ID: "b", Kind: "prop", Name: "错误对象", SourceIDs: []string{"s2"}}}
	p.LatestRun().End = 1
	p.LatestRun().Calls = []d.Call{{ID: "c", Stage: "extract", Start: 0, End: 1, Status: "received", FinishReason: "tool_calls", Actions: []d.Action{{ID: "save", Name: "save_catalog", Status: "pending", Arguments: `{"assets":[{"id":"a","kind":"character","name":"祖父","description":"姓名未说明","sourceIds":["s1"]},{"kind":"prop","name":"新增对象","sourceIds":["s1"]}],"removeIds":["b"]}`}}}}
	p.Revision++
	s.Store.Commit(context.Background(), p, p.Revision-1)
	result, err := s.executeTool(context.Background(), p.ID, "save")
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Changes []catalogChange `json:"changes"`
	}
	if json.Unmarshal([]byte(result.Text), &receipt) != nil || len(receipt.Changes) != 3 || receipt.Changes[0].Before == nil || receipt.Changes[0].After.Description != "姓名未说明" || receipt.Changes[1].Before.ID != "b" || receipt.Changes[1].After != nil || receipt.Changes[2].After.ID == "" {
		t.Fatal("actual before/after or ID missing", result.Text)
	}
	p, _ = s.Store.Snapshot(context.Background(), p.ID)
	req, _, err := s.request(context.Background(), p)
	var input struct {
		Sources  []d.SourceSpan `json:"source"`
		Feedback []struct {
			Changes []catalogChange `json:"changes"`
		} `json:"lastToolResults"`
	}
	if err != nil || json.Unmarshal([]byte(req.Input), &input) != nil || len(input.Sources) != 2 || len(input.Feedback) != 0 || len(req.Dialogue) != 2 || req.Dialogue[1].Content != result.Text {
		t.Fatal("deleted object source or receipt absent", err, req.Input)
	}
	if !strings.Contains(req.Input, receipt.Changes[2].After.ID) {
		t.Fatal("assigned ID not delivered")
	}
	// A repeated tool execution must return the exact receipt without reapplying.
	again, err := s.executeTool(context.Background(), p.ID, "save")
	stable, _ := s.Store.Snapshot(context.Background(), p.ID)
	if err != nil || again.Text != result.Text || len(stable.Assets) != 2 || stable.Assets[1].ID != p.Assets[1].ID {
		t.Fatal("tool replay duplicated a new object", err)
	}
}

func TestInsufficientMaximumOutputReservationDoesNotForceSingleParagraph(t *testing.T) {
	m := &fakeModel{}
	s, p := fixture(t, m)
	p.ScriptSource.Spans = []d.SourceSpan{{ID: "s1", Text: "完整第一段"}, {ID: "s2", Text: "完整第二段"}, {ID: "s3", Text: "完整第三段"}}
	req, _, err := s.request(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := json.Marshal(req.Tools)
	m.window = inputSize(req) + len(tools) + 1000
	m.output = m.window - 1
	req, end, err := s.request(context.Background(), p)
	if err != nil || end != 3 || req.MaxOutputTokens != 1000 {
		t.Fatal("maximum reservation forced a single paragraph", err, end, req.MaxOutputTokens)
	}
}

func TestCorrectionUsesSharedExactSourceAndKeepsWritableSchemaSeparate(t *testing.T) {
	m := &fakeModel{}
	s, p := fixture(t, m)
	p.Assets = []d.Asset{{ID: "a", Kind: "character", Name: "待核对人物", Description: "候选说明", SourceIDs: []string{"s1", "s2"}}}
	p.LatestRun().Stage = "correct"
	p.LatestRun().End = 1
	req, _, err := s.request(context.Background(), p)
	var input struct {
		Source []d.SourceSpan
		Assets []d.Asset
	}
	if err != nil || json.Unmarshal([]byte(req.Input), &input) != nil || len(input.Assets) != 1 || len(input.Source) != 2 || input.Source[1].Text != p.ScriptSource.Spans[1].Text || strings.Contains(req.Input, `"original"`) || strings.Join(input.Assets[0].SourceIDs, ",") != "p1,p2" {
		t.Fatal("shared exact original or compact references missing", err, req.Input)
	}
	if _, err = Decode(`{"assets":[{"id":"a","kind":"character","name":"人物","sourceIds":["s1"],"original":[{"id":"s1","text":"原文"}]}]}`); err == nil {
		t.Fatal("read-only original accidentally accepted as writable facts")
	}
	p.LatestRun().Stage = "extract"
	req, _, err = s.request(context.Background(), p)
	if err != nil || strings.Contains(req.Input, `"original"`) {
		t.Fatal("extraction duplicated supporting text", err)
	}
}

// Explicit finite permission for fictional test fixtures only.
func freeAuthorization(context.Context, d.Project, d.Run) (int64, error) {
	return 100000000, nil
}

func (m *memory) ListSummaries(ctx context.Context) ([]ports.ProjectSummary, error) {
	ps, err := m.List(ctx)
	out := []ports.ProjectSummary{}
	for _, p := range ps {
		if p.DeletedAt == nil {
			row := ports.ProjectSummary{ID: p.ID, Title: p.Title, Filename: p.ScriptSource.Filename, UploadedAt: p.UploadedAt, Revision: p.Revision}
			for _, a := range p.Assets {
				if a.Kind == "character" && a.ImageFileID != "" && len(row.PreviewImageFileIDs) < 3 {
					row.PreviewImageFileIDs = append(row.PreviewImageFileIDs, a.ImageFileID)
				}
			}
			if r := p.LatestRun(); r != nil {
				row.LatestRun = &ports.ProjectRunSummary{ID: r.ID, Contract: r.Contract, Status: string(r.Status), Stage: r.Stage}
			}
			out = append(out, row)
		}
	}
	return out, err
}
func (m *memory) RunningProjectIDs(ctx context.Context) ([]string, error) {
	ps, err := m.List(ctx)
	out := []string{}
	for _, p := range ps {
		if r := p.LatestRun(); p.DeletedAt == nil && r != nil && r.Status == "running" {
			out = append(out, p.ID)
		}
	}
	return out, err
}
func (m *memory) ModelReferences(ctx context.Context) ([]ports.ProjectModelReference, error) {
	ps, err := m.List(ctx)
	out := []ports.ProjectModelReference{}
	for _, p := range ps {
		ref := ports.ProjectModelReference{Runs: []ports.RunModelReference{}}
		if v := p.ProjectSettings; v != nil {
			ref.HasSettings = true
			ref.LLMModel = v.LLMModel
			ref.ImageModel = v.ImageModel
			ref.VideoModel = v.VideoModel
		}
		for _, r := range p.Runs {
			ref.Runs = append(ref.Runs, ports.RunModelReference{Model: r.Model, Status: string(r.Status)})
		}
		out = append(out, ref)
	}
	return out, err
}

// 注册表只注入测试模型；生产项目用例没有配置内部字段或固定模型旁路。
func registry(s *Service) *modelregistry.Registry { return s.Models.(*modelregistry.Registry) }
