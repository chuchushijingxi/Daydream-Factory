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
	"sync"
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

// Real HTTP response cuts through the production adapter, Eino and PG.
// Fixtures verify transport recovery; they do not assert LLM understanding.
func TestActualInterruptedCorrectionRetriesWithSavedCatalogAndDialogue(t *testing.T) {
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("real PG opt-in; local HTTP only")
	}
	ctx := context.Background()
	store, err := pgfixture.Open(t, ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var mu sync.Mutex
	requests := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var wire map[string]any
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
			return
		}
		if modelregistry.FreeVisualWireReply(w, wire) {
			return
		}
		requests = append(requests, wire)
		n := len(requests)
		messages := wire["messages"].([]any)
		var input struct {
			ScriptSource []d.SourceSpan `json:"source"`
			Assets       []d.Asset      `json:"assets"`
		}
		if json.Unmarshal([]byte(messages[1].(map[string]any)["content"].(string)), &input) != nil || len(input.ScriptSource) != 2 {
			t.Error("complete source absent")
			return
		}
		name, args := "save_catalog", ""
		if n == 1 {
			assets := []d.Asset{{Kind: "character", Name: "祖父", Description: "姓名未说明", SourceIDs: []string{input.ScriptSource[0].ID}}, {Kind: "prop", Name: "铜钥匙", SourceIDs: []string{input.ScriptSource[0].ID}}, {Kind: "prop", Name: "另一把铜钥匙", SourceIDs: []string{input.ScriptSource[1].ID}}}
			raw, _ := json.Marshal(app.CatalogChangeSet{Assets: catalogInputs(assets)})
			args = string(raw)
		} else {
			if len(input.Assets) != 3 {
				t.Error("interruption lost saved catalog")
				return
			}
			if n == 2 || n == 3 {
				if len(messages) != 2 {
					t.Error("previous stage/partial dialogue leaked")
				}
				asset := input.Assets[0]
				asset.Description = "姓名未说明；将钥匙交给小雨"
				raw, _ := json.Marshal(app.CatalogChangeSet{Assets: catalogInputs([]d.Asset{asset})})
				args = string(raw)
			} else {
				if len(messages) != 4 || messages[2].(map[string]any)["reasoning_content"] != "complete correction reasoning" || messages[3].(map[string]any)["tool_call_id"] != "tool-3" {
					t.Error("committed current dialogue missing")
				}
				name, args = "finish_batch", "{}"
			}
		}
		actions := []any{map[string]any{"id": fmt.Sprintf("tool-%d", n), "type": "function", "function": map[string]string{"name": name, "arguments": args}}}
		if n == 1 {
			actions = append(actions, map[string]any{"id": "extract-finish", "type": "function", "function": map[string]string{"name": "finish_batch", "arguments": "{}"}})
		}
		if n == 2 || n == 4 {
			// The body even contains a complete-looking destructive tool call.
			// HTTP framing is incomplete, so nothing in it may be adopted.
			raw, _ := json.Marshal(app.CatalogChangeSet{Assets: catalogInputs([]d.Asset{}), RemoveIDs: []string{input.Assets[0].ID, input.Assets[1].ID, input.Assets[2].ID}})
			actions = []any{map[string]any{"id": "partial-delete", "type": "function", "function": map[string]string{"name": "save_catalog", "arguments": string(raw)}}}
		}
		body, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("local-response-%d", n), "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "reasoning_content": "complete correction reasoning", "tool_calls": actions}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20}})
		if n == 2 || n == 4 {
			w.Header().Set("Content-Length", fmt.Sprint(len(body)+100))
			w.Write(body)
			w.(http.Flusher).Flush()
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		w.Write(body)
	}))
	defer server.Close()
	model, err := modeladapter.New(ctx, "local-only", server.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	service := &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Authorize: freeAuthorization, Store: store, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: model}}
	p, err := fixtureProject(t, service, legacyHTTPActor).Create(ctx, "local retry test", "retry.txt", []byte("祖父将铜钥匙交给小雨。\n另一把铜钥匙放在门边。"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		conn, e := pgx.Connect(ctx, database)
		if e == nil {
			defer conn.Close(ctx)
			conn.Exec(ctx, "DELETE FROM drama_catalog_v1.projects WHERE id=$1", p.ID)
		}
	}()
	p, err = quotedLegacyService(t, service, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "asset-preparation"}).Start(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Execute(ctx, p.ID); err != nil {
		if failed, readErr := store.Snapshot(ctx, p.ID); readErr == nil && failed.LatestRun() != nil {
			for _, call := range failed.LatestRun().Calls {
				t.Logf("call %s stage=%s status=%s actions=%+v", call.ID, call.Stage, call.Status, call.Actions)
			}
		}
		t.Fatal(err)
	}
	p, err = store.Snapshot(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	r := p.LatestRun()
	if len(requests) != 5 || len(r.Calls) != 7 || r.Status != "completed" || len(p.Assets) != 3 || !reflect.DeepEqual(requests[1], requests[2]) || !reflect.DeepEqual(requests[3], requests[4]) {
		t.Fatal("retry changed actual wire material or failed to complete")
	}
	if p.Assets[0].Description != "姓名未说明；将钥匙交给小雨" {
		t.Fatal("correction lost")
	}
	var unknownReserve int64
	for _, i := range []int{1, 3} {
		c := r.Calls[i]
		unknownReserve += c.ReservedMicroCNY
		if c.Status != "unknown" || c.ErrorCode != "MODEL_RESPONSE_INTERRUPTED" || len(c.Actions) != 0 || c.Usage.Known || c.Response == "" {
			t.Fatal("partial data adopted or unknown usage invented")
		}
	}
	if p.UsedMicroCNY() != 1080+unknownReserve {
		t.Fatal("retry accounting lost")
	}
	for i, a := range p.Assets {
		if a.ID != r.Calls[0].After[i].ID {
			t.Fatal("correct object identity lost")
		}
	}
}
