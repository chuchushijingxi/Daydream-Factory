package httpapi_test

import (
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"drama-agent/internal/adapter/einoagent"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/adapter/scriptimport"
	app "drama-agent/internal/application/project"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"

	"github.com/jackc/pgx/v5"
)

// Real Eino -> application -> DeepSeek adapter -> actual HTTP. Protocol
// fixtures prove transport, scope reset and recovery, not model understanding.
func TestActualEinoTaskDialogueAndRecovery(t *testing.T) {
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
	for _, recovered := range []bool{false, true} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Messages []struct {
					Role, Content string
					Reasoning     string `json:"reasoning_content"`
					Calls         []struct {
						ID       string
						Function struct{ Name, Arguments string }
					} `json:"tool_calls"`
					ToolID string `json:"tool_call_id"`
				}
				Tools []struct {
					Function struct {
						Name, Description string
						Parameters        any
					}
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if modelregistry.FreeVisualWireReply(w, body) {
				return
			}
			calls++
			var material map[string]json.RawMessage
			if err := json.Unmarshal([]byte(body.Messages[1].Content), &material); err != nil {
				t.Fatal(err)
			}
			if _, exists := material["title"]; exists {
				t.Error("display-only project title entered the actual provider request")
			}
			n := calls
			if recovered {
				n++
			}
			prompt := app.ExtractPrompt
			if n >= 3 {
				prompt = app.CorrectPrompt
			}
			if len(body.Messages) < 2 || body.Messages[0].Role != "system" || body.Messages[0].Content != prompt+sourceInstruction(t) || body.Messages[1].Role != "user" {
				t.Errorf("call %d actual provider prompt differs from current business task", n)
			}
			if len(body.Tools) != len(app.Tools()) {
				t.Fatal("actual provider tool list differs from execution")
			}
			for i, definition := range app.Tools() {
				var parameters any
				if err := json.Unmarshal(definition.Parameters, &parameters); err != nil {
					t.Fatal(err)
				}
				actual := body.Tools[i].Function
				if actual.Name != definition.Name || actual.Description != definition.Description || !reflect.DeepEqual(actual.Parameters, parameters) {
					t.Errorf("call %d actual tool differs from parser/adoption schema: %s", n, definition.Name)
				}
			}
			expected := 2
			if n == 2 || n == 4 {
				expected = 4
			}
			if len(body.Messages) != expected {
				t.Errorf("call %d old batch dialogue or missing pair: %d", n, len(body.Messages))
			}
			if expected == 4 && len(body.Messages) == 4 {
				a, tool := body.Messages[2], body.Messages[3]
				id := fmt.Sprintf("tool-%d", n-1)
				if a.Role != "assistant" || a.Reasoning != fmt.Sprintf("reason-%d", n-1) || len(a.Calls) != 1 || a.Calls[0].ID != id || tool.Role != "tool" || tool.ToolID != id || tool.Content == "" {
					t.Errorf("assistant/reasoning/tool not continued: %+v", body.Messages)
				}
			}
			var input struct {
				ScriptSource []d.SourceSpan `json:"source"`
				Assets       []d.Asset
			}
			json.Unmarshal([]byte(body.Messages[1].Content), &input)
			name, args := "finish_batch", "{}"
			if n == 1 || n == 3 {
				name = "save_catalog"
				assets := []d.Asset{{Kind: "character", Name: "祖父", SourceIDs: []string{input.ScriptSource[0].ID}}}
				if n == 3 {
					assets[0].ID = input.Assets[0].ID
					assets[0].Description = "姓名未说明"
				}
				raw, _ := json.Marshal(app.CatalogChangeSet{Assets: catalogInputs(assets)})
				args = string(raw)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "history-fixture", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "reasoning_content": fmt.Sprintf("reason-%d", n), "tool_calls": []any{map[string]any{"id": fmt.Sprintf("tool-%d", n), "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20}})
		}))
		model, err := modeladapter.New(ctx, "local-only", server.URL, md.DeepSeekModelID)
		if err != nil {
			t.Fatal(err)
		}
		service := &app.Service{Authorize: freeAuthorization, Store: store, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: model}}
		p, err := service.Create(ctx, "display-only unrelated title", "x.txt", []byte("祖父留下钥匙。"), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer func(id string) {
			db, e := pgx.Connect(ctx, database)
			if e == nil {
				defer db.Close(ctx)
				db.Exec(ctx, "DELETE FROM drama_catalog_v1.projects WHERE id=$1", id)
			}
		}(p.ID)
		p, err = service.Start(ctx, p.ID, p.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if recovered {
			// A complete response + committed save survived process restart. Runner
			// must seed this exact pair and continue, without another save or new ID.
			asset := d.Asset{ID: "retained-id", Kind: "character", Name: "祖父", SourceIDs: []string{p.ScriptSource.Spans[0].ID}}
			draft, _ := json.Marshal(app.CatalogChangeSet{Assets: catalogInputs([]d.Asset{{Kind: asset.Kind, Name: asset.Name, SourceIDs: asset.SourceIDs}})})
			receipt := `{"ok":true,"savedCount":1}`
			p.Assets = []d.Asset{asset}
			p.LatestRun().End = 1
			p.LatestRun().Calls = []d.Call{{ID: "persisted-call", Stage: "extract", End: 1, Status: "applied", Reasoning: "reason-1", FinishReason: "tool_calls", Actions: []d.Action{{ID: "tool-1", Name: "save_catalog", Arguments: string(draft), Status: "applied", Result: receipt}}}}
			rev := p.Revision
			p.Revision++
			if err = store.Commit(ctx, p, rev); err != nil {
				t.Fatal(err)
			}
		}
		if err = service.Execute(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
		p, err = store.Snapshot(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if p.LatestRun().Status != "completed" || len(p.Assets) != 1 || p.Assets[0].Description != "姓名未说明" || (recovered && p.Assets[0].ID != "retained-id") {
			t.Fatal("continuation/recovery failed")
		}
		if p.Title != "display-only unrelated title" {
			t.Fatal("removing model metadata changed the stored display title")
		}
		for _, call := range p.LatestRun().Calls {
			if len(call.Request) > 0 {
				var request map[string]any
				json.Unmarshal(call.Request, &request)
				if !reflect.DeepEqual(request["messages"].([]any)[0].(map[string]any)["role"], "system") {
					t.Fatal("wire receipt differs")
				}
			}
		}
		server.Close()
	}
}
