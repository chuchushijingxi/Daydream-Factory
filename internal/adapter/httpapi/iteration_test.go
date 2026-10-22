package httpapi_test

import (
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"

	"github.com/jackc/pgx/v5"
)

type continuingModel struct {
	t     *testing.T
	count int
}

func (m *continuingModel) Capacity() (int, int) { return 1000000, 393216 }

func (m *continuingModel) Generate(ctx context.Context, req ports.Request, observe func(json.RawMessage) error) (ports.Response, error) {
	wire, _ := json.Marshal(req)
	if err := observe(wire); err != nil {
		return ports.Response{}, err
	}
	if reply, ok := modelregistry.FreeVisualReply(req); ok {
		return reply, nil
	}
	m.count++
	var input struct {
		ScriptSource []d.SourceSpan    `json:"source"`
		Assets       []d.Asset         `json:"assets"`
		Feedback     []json.RawMessage `json:"lastToolResults"`
	}
	if err := json.Unmarshal([]byte(req.Input), &input); err != nil {
		return ports.Response{}, err
	}
	if req.CurrentState != "" {
		var state struct {
			Assets []d.Asset `json:"assets"`
		}
		if err := json.Unmarshal([]byte(req.CurrentState), &state); err != nil {
			m.t.Fatal(err)
		}
		input.Assets = state.Assets
	}
	if len(req.Tools) != 2 || len(input.ScriptSource) != 2 {
		m.t.Fatal("missing actual tools or source")
	}
	name, args := "save_catalog", ""
	switch m.count {
	case 1:
		input.Assets = []d.Asset{{Kind: "character", Name: "祖父", Description: "错误属性待模型修订", SourceIDs: []string{input.ScriptSource[0].ID}}}
	case 2, 6:
		name, args = "finish_batch", "{}"
	case 3:
		if len(input.Assets) != 1 || input.Assets[0].ID == "" {
			m.t.Fatal("exact saved IDs not delivered")
		}
		input.Assets[0].Description = "姓名未说明"
		input.Assets = append(input.Assets, d.Asset{Kind: "prop", Name: "钥匙", SourceIDs: []string{input.ScriptSource[0].ID}})
	case 4:
		input.Assets = []d.Asset{{Kind: "prop", Name: "坏引用", SourceIDs: []string{"foreign-project-source"}}}
	case 5:
		if len(input.Assets) != 2 || !hasSourceRejection(req.Dialogue) {
			m.t.Fatal("rejection lost current catalog or actionable feedback")
		}
		input.Assets = []d.Asset{{Kind: "prop", Name: "镜子", SourceIDs: []string{input.ScriptSource[1].ID}}}
	default:
		m.t.Fatal("agent did not stop after explicit finish")
	}
	if args == "" {
		b, _ := json.Marshal(app.CatalogChangeSet{Assets: catalogInputs(input.Assets)})
		args = string(b)
	}
	actions := []d.Action{{ID: identity.New("tool"), Name: name, Arguments: args, Status: "pending"}}
	if m.count == 4 {
		actions = append(actions, d.Action{ID: identity.New("tool"), Name: "finish_batch", Arguments: "{}", Status: "pending"})
	}
	return ports.Response{Actions: actions, FinishReason: "tool_calls", Raw: json.RawMessage(`{"fixture":true}`), Usage: billing.Usage{Known: true, Input: 100, Output: 20}}, nil
}

// This tests execution and feedback using a scripted model, not semantics.
func TestEinoContinuesPastTwoCallsAndRepairsRejectedWrite(t *testing.T) {
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("real PG opt-in")
	}
	ctx := context.Background()
	store, err := pgfixture.Open(t, ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m := &continuingModel{t: t}
	s := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Authorize: freeAuthorization, Store: store, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: m}}
	p, err := fixtureProject(t, s, legacyHTTPActor).Create(ctx, "Eino iteration contract", "x.txt", []byte("祖父留下钥匙。\n小雨看向镜子。"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		conn, e := pgx.Connect(ctx, database)
		if e == nil {
			defer conn.Close(ctx)
			_, _ = conn.Exec(ctx, "DELETE FROM drama_catalog_v1.projects WHERE id=$1", p.ID)
		}
	}()
	p, err = quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "asset-preparation"}).Start(ctx, p.ID, p.Revision)
	if err == nil {
		err = s.Execute(ctx, p.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	p, err = store.Snapshot(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := p.LatestRun()
	if m.count != 6 || r.Status != "completed" || len(p.Assets) != 3 || p.Assets[0].Description != "姓名未说明" || p.Assets[0].ID != r.Calls[0].After[0].ID || r.Calls[3].Actions[0].Status != "rejected" || r.Calls[3].Actions[1].Status != "rejected" || len(r.Calls[3].Before) != 2 || len(r.Calls[3].After) != 0 || p.UsedMicroCNY() != 2160 {
		t.Fatalf("iteration/adoption/accounting contract failed: calls=%d status=%s error=%s assets=%+v cost=%d", m.count, r.Status, r.Error, p.Assets, p.UsedMicroCNY())
	}
}

func hasSourceRejection(messages []ports.Message) bool {
	calls := map[string]d.Action{}
	for _, message := range messages {
		for _, action := range message.Actions {
			calls[action.ID] = action
		}
	}
	for _, message := range messages {
		if message.Role != "tool" || calls[message.ToolCallID].Name != "save_catalog" {
			continue
		}
		var result struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(message.Content), &result) == nil && !result.OK && strings.Contains(result.Error, "unknown source reference") {
			return true
		}
	}
	return false
}
