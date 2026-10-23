package httpapi_test

import (
	"bytes"
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/adapter/httpapi"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/adapter/stylecatalog"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"

	"github.com/jackc/pgx/v5"
)

func openProjectContentPG(t *testing.T) *postgres.Store {
	t.Helper()
	url := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("isolated PG opt-in")
	}
	s, err := pgfixture.Open(t, context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func cleanProjectContentRow(t *testing.T, p d.Project) {
	t.Helper()
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), os.Getenv("DRAMA_TEST_DATABASE_URL"))
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(context.Background())
		if _, err = conn.Exec(context.Background(), "DELETE FROM drama_catalog_v1.projects WHERE id=$1", p.ID); err != nil {
			t.Error(err)
		}
	})
}

func contentService(t *testing.T, store *postgres.Store, model ports.Model) *app.Service {
	t.Helper()
	styles, err := stylecatalog.LoadStyles("../../../demo")
	if err != nil {
		t.Fatal(err)
	}
	return &app.Service{Resolver: store, Access: store, Credits: store, Prices: &frozenFixturePrices{version: 1}, Store: store, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Styles: styles, Authorize: freeAuthorization, Models: &modelregistry.Registry{Model: model}}
}

func createProjectContent(t *testing.T, s *app.Service, assets []d.Asset) d.Project {
	t.Helper()
	ctx := context.Background()
	p, err := fixtureProject(t, s, legacyHTTPActor).Create(ctx, "P4 虚构免费夹具", "fictional.txt", []byte("林岚，32岁修理师，左眉尾浅疤、黑发低马尾。\n她在修理间使用铜钥匙开门。\n晚宴上她身穿黑礼服。"), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	p.Assets = assets
	p.Runs = []d.Run{{ID: identity.New("p3"), Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "completed", Calls: []d.Call{{ID: "unknown-history", ReservedMicroCNY: 789}}}}
	p.Revision++
	if err = s.Store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	return p
}

func pgAssets() []d.Asset {
	return []d.Asset{{ID: "c", Kind: "character", Name: "林岚", Description: "完整 P3 说明：32岁修理师，左眉尾浅疤、黑发低马尾", Looks: []d.Look{{ID: "l", Name: "晚宴装", Description: "黑礼服"}}}, {ID: "loc", Kind: "location", Name: "修理间", Description: "有工作台的空间"}, {ID: "prop", Kind: "prop", Name: "铜钥匙", Description: "用于开门"}}
}

func emitProjectContent(w http.ResponseWriter, actions []d.Action) {
	calls := []any{}
	for _, a := range actions {
		calls = append(calls, map[string]any{"id": a.ID, "type": "function", "function": map[string]string{"name": a.Name, "arguments": a.Arguments}})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": identity.New("response"), "model": "deepseek-flash", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "tool_calls": calls}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 30}})
}

func contentAction(name string, v any) d.Action {
	raw, _ := json.Marshal(v)
	return d.Action{ID: identity.New("action"), Name: name, Arguments: string(raw), Status: "pending"}
}

func visualFixture() app.SaveVisualPrompts {
	return app.SaveVisualPrompts{Prompts: []app.VisualPromptInput{{Target: app.VisualTargetInput{AssetID: "c"}, Text: "林岚，32岁，左眉尾浅疤，黑发低马尾，深蓝工装", DesignNotes: "椭圆脸、深棕眼睛为设计"}, {Target: app.VisualTargetInput{AssetID: "c", LookID: "l"}, Text: "林岚，32岁，左眉尾浅疤，黑发低马尾，黑礼服", DesignNotes: "采用基础面貌"}, {Target: app.VisualTargetInput{AssetID: "loc"}, Text: "修理间环境，工作台", DesignNotes: "布局设计"}, {Target: app.VisualTargetInput{AssetID: "prop"}, Text: "铜钥匙物件本体", DesignNotes: "尺寸设计"}}}
}

func postProjectContent(t *testing.T, url, path string, v any) (int, []byte) {
	t.Helper()
	raw, _ := json.Marshal(v)
	res, err := http.Post(url+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body
}

func assertProjectContentView(t *testing.T, raw []byte, p d.Project) {
	t.Helper()
	var got struct {
		ID                  string
		Revision            uint64
		Assets              []httpapi.AssetView
		Outline             *d.Outline
		ProjectSettings     *d.ProjectSettings `json:"settings"`
		Style               *d.ProjectStyle
		VisualPrompts       []httpapi.VisualPromptView
		Runs                []httpapi.RunView
		Run                 *httpapi.RunView
		CatalogEditedByUser bool
		Used                int64 `json:"usedOrReservedMicroCny"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != p.ID || got.Revision != p.Revision || got.Used != p.UsedMicroCNY() || !reflect.DeepEqual(got.Outline, p.Outline) || !reflect.DeepEqual(got.ProjectSettings, p.ProjectSettings) || !reflect.DeepEqual(got.Style, p.Style) || got.CatalogEditedByUser != p.CatalogEditedByUser || len(got.Assets) != len(p.Assets) || len(got.Runs) != len(p.Runs) {
		t.Fatal("PG/API mismatch", string(raw))
	}
	if got.Run == nil || got.Run.ID != p.LatestRun().ID || got.Run.Status != p.LatestRun().Status || got.Run.SourceVersion != p.LatestRun().SourceVersion {
		t.Fatal("run projection mismatch")
	}
	for i, a := range got.Assets {
		expected := p.Assets[i]
		if a.ID != expected.ID || a.Description != expected.Description || a.Name != expected.Name || len(a.Looks) != len(expected.Looks) {
			t.Fatal("asset mismatch")
		}
		if a.Kind == "character" && a.Character == nil {
			t.Fatal("missing empty six fields")
		}
		if a.Kind != "character" && (a.Character != nil || len(a.Looks) != 0) {
			t.Fatal("location or prop got character shape")
		}
	}
	for i, v := range got.VisualPrompts {
		if v.Text != p.VisualPrompts[i].Text || v.Stale != p.VisualPrompts[i].Stale || v.ActionRef != p.VisualPrompts[i].ActionRef {
			t.Fatal("prompt projection mismatch")
		}
	}
	if bytes.Contains(raw, []byte(`"designNotes"`)) || bytes.Contains(raw, []byte(`"basePrompt"`)) {
		t.Fatal("internal prompt data leaked into project view")
	}
}

func TestProjectContentPostgresActualEinoWireReceiptsRevisionAndCRUD(t *testing.T) {
	// Freeze the approved business text at the actual HTTP boundary. This does
	// not judge semantics of fixture outputs; it verifies the prompt is delivered.
	approved, err := os.ReadFile("../../application/project/testdata/contracts/visual.txt")
	if err != nil {
		t.Fatal(err)
	}
	expectedVisualPrompt := string(approved)
	store := openProjectContentPG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var outlineCalls, visualCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire struct {
			Model    string `json:"model"`
			Messages []struct {
				Role, Content string
				ID            string `json:"tool_call_id"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name, Description string
					Parameters        any
				}
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
			return
		}
		if wire.Model != "deepseek-flash" || len(wire.Messages) < 2 {
			t.Error("actual model/source absent")
			return
		}
		contract := d.OutlineContract
		if strings.Contains(wire.Tools[0].Function.Name, "visual") {
			contract = d.VisualContract
			if wire.Messages[0].Role != "system" || wire.Messages[0].Content != expectedVisualPrompt+sourceInstruction(t) {
				t.Error("approved visual content rules differ from actual provider input")
			}
		}
		defs := app.TaskTools(contract)
		if len(wire.Tools) != len(defs) {
			t.Error("task tools not isolated")
		}
		for i, v := range defs {
			var expected any
			_ = json.Unmarshal(v.Parameters, &expected)
			if wire.Tools[i].Function.Name != v.Name || !reflect.DeepEqual(wire.Tools[i].Function.Parameters, expected) {
				t.Error("wire/schema disagreement")
			}
		}
		if !strings.Contains(wire.Messages[1].Content, "黑礼服") || strings.Contains(wire.Messages[1].Content, "spark-playlet") || strings.Contains(wire.Messages[1].Content, "16:9") {
			t.Error("full source omitted or style/settings leaked")
		}
		if contract == d.OutlineContract {
			n := outlineCalls.Add(1)
			if n == 1 {
				emitProjectContent(w, []d.Action{contentAction("save_outline", app.SaveOutline{Outline: d.OutlineInput{Protagonist: "林岚", Genre: "剧情", Summary: "修理师开门后参加晚宴", Background: "修理间与晚宴", Logline: "修理师的一天"}})})
				return
			}
			if n != 2 || wire.Messages[len(wire.Messages)-1].Role != "tool" || !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, `"after"`) {
				t.Error("outline actual receipt absent")
			}
			emitProjectContent(w, []d.Action{contentAction("finish_outline", struct{}{})})
			return
		}
		n := visualCalls.Add(1)
		if strings.Contains(wire.Messages[1].Content, "修理师的一天") {
			t.Error("outline entered visual inputs")
		}
		switch n {
		case 1:
			wrong := visualFixture()
			wrong.Prompts[1].Text = strings.Replace(wrong.Prompts[1].Text, "左眉尾", "右眉尾", 1)
			emitProjectContent(w, []d.Action{contentAction("save_visual_prompts", wrong), contentAction("finish_visual_prompts", struct{}{})})
		case 2:
			if !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, `"ok":false`) {
				t.Error("same-response finish rejection not delivered")
			}
			v := visualFixture()
			v.Prompts = v.Prompts[:1]
			v.Prompts[0].Text += "，保持正确特征并修订面貌设计"
			emitProjectContent(w, []d.Action{contentAction("save_visual_prompts", v)})
		case 3:
			if !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, `"invalidatedTargets":[{`) {
				t.Error("base invalidation receipt absent")
			}
			emitProjectContent(w, []d.Action{contentAction("finish_visual_prompts", struct{}{})})
		case 4:
			if !strings.Contains(wire.Messages[1].Content, "右眉尾浅疤") {
				t.Error("controlled mistake disappeared before actual repair")
			}
			if !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, `"ok":false`) {
				t.Error("stale look rejection absent")
			}
			v := visualFixture()
			v.Prompts = v.Prompts[1:2]
			v.Prompts[0].Text += "，采用修订的基础面貌"
			emitProjectContent(w, []d.Action{contentAction("save_visual_prompts", v)})
		case 5:
			if !strings.Contains(wire.Messages[len(wire.Messages)-1].Content, `"basePrompt"`) {
				t.Error("actual base ref absent")
			}
			emitProjectContent(w, []d.Action{contentAction("finish_visual_prompts", struct{}{})})
		default:
			t.Error("finish sent extra request")
			http.Error(w, "extra request", 500)
		}
	}))
	defer provider.Close()
	model, err := modeladapter.New(ctx, "fictional-loopback-key", provider.URL, md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	service := contentService(t, store, model)
	p := createProjectContent(t, service, pgAssets())
	host, handler := httpapi.New(configuredFixture(service), nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	server := httptest.NewServer(handler)
	defer server.Close()
	defer host.Shutdown(ctx)
	command := func(path string, v any, status int) {
		t.Helper()
		code, body := postProjectContent(t, server.URL, "/api/projects/"+p.ID+path, v)
		if code != status {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		if status < 300 {
			p, err = store.Snapshot(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	command("/settings", map[string]any{"revision": p.Revision, "settings": d.ProjectSettings{AspectRatio: "16:9", LLMModel: "deepseek-flash"}}, 200)
	confirmed := d.Clone(p)
	command("/settings", map[string]any{"revision": p.Revision, "settings": *p.ProjectSettings}, 200)
	command("/settings", map[string]any{"revision": p.Revision, "settings": d.ProjectSettings{AspectRatio: "9:16"}}, 409)
	current, _ := store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(current, confirmed) {
		t.Fatal("API changed confirmed settings")
	}

	command("/style", map[string]any{"revision": p.Revision, "styleId": service.Styles[0].ID, "promptVersion": service.Styles[0].PromptVersion}, 200)
	// 当前源 P3 完成即允许普通编辑；删除临时项后仍用同一冻结目录核对模型过程。
	command("/assets", map[string]any{"revision": p.Revision, "asset": d.AssetInput{Kind: "prop", Name: "x", Aliases: []string{}}}, 201)
	command("/assets/"+p.Assets[len(p.Assets)-1].ID+"/delete", map[string]any{"revision": p.Revision}, 200)
	command("/outline-runs", map[string]any{"revision": p.Revision, "billingBasis": quoteProjectContent(t, server.URL, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "outline"})}, 202)
	if err = host.WaitUntilIdle(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ = store.Snapshot(ctx, p.ID)
	if p.LatestRun().Status != "completed" || outlineCalls.Load() != 2 || len(p.Assets) != 3 {
		t.Fatal("outline failed", p.LatestRun().Status, p.LatestRun().Error)
	}
	command("/outline-runs", map[string]any{"revision": p.Revision}, 409)
	command("/visual-prompt-runs", map[string]any{"revision": p.Revision, "billingBasis": quoteProjectContent(t, server.URL, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "visual"})}, 202)
	if err = host.WaitUntilIdle(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ = store.Snapshot(ctx, p.ID)
	if p.LatestRun().Status != "completed" || visualCalls.Load() != 5 || !p.VisualComplete(p.LatestRun()) {
		t.Fatal("visual execution failed", p.LatestRun().Status, p.LatestRun().Error)
	}
	if p.Prompt(d.VisualTarget{AssetID: "loc"}).Text != "修理间环境，工作台" || p.Prompt(d.VisualTarget{AssetID: "prop"}).Text != "铜钥匙物件本体" {
		t.Fatal("correct content lost")
	}
	lookText := p.Prompt(d.VisualTarget{AssetID: "c", LookID: "l"}).Text
	if strings.Contains(lookText, "右眉尾") || !strings.Contains(lookText, "左眉尾浅疤，黑发低马尾，黑礼服") {
		t.Fatal("controlled scar correction lost correct characteristics")
	}
	savedText := p.Prompt(d.VisualTarget{AssetID: "c"}).Text
	assembled, err := service.ComposeVisualPrompt(ctx, p.ID, d.VisualTarget{AssetID: "c"})
	if err != nil || assembled != savedText+"\n\n风格要求:\n"+p.Style.Prompts.ZH {
		t.Fatal("central assembly incorrect", err)
	}
	res, err := http.Get(server.URL + "/api/projects/" + p.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	assertProjectContentView(t, body, p)
	count, used := visualCalls.Load()+outlineCalls.Load(), p.UsedMicroCNY()
	command("/assets/c/looks/l", map[string]any{"revision": p.Revision, "look": d.LookInput{Name: "晚宴装修订", Description: "完整用户改动\n"}}, 200)
	if p.Prompt(d.VisualTarget{AssetID: "c"}).Stale || !p.Prompt(d.VisualTarget{AssetID: "c", LookID: "l"}).Stale {
		t.Fatal("look invalidation mismatch")
	}
	for _, kind := range []string{"character", "location", "prop"} {
		v := d.AssetInput{Kind: kind, Name: " 同名 ", Aliases: []string{}}
		if kind == "character" {
			v.Character = &d.Character{}
		}
		command("/assets", map[string]any{"revision": p.Revision, "asset": v}, 201)
		id := p.Assets[len(p.Assets)-1].ID
		if p.Prompt(d.VisualTarget{AssetID: id}) != nil {
			t.Fatal("new asset got prompt")
		}
		command("/assets/"+id+"/delete", map[string]any{"revision": p.Revision}, 200)
	}
	command("/assets/c/looks", map[string]any{"revision": p.Revision, "look": d.LookInput{Name: "冬装"}}, 201)
	look := p.Asset("c").Looks[1]
	command("/assets/c/looks/"+look.ID+"/delete", map[string]any{"revision": p.Revision}, 200)
	command("/assets/c/looks/l/delete", map[string]any{"revision": p.Revision}, 200)
	if p.Asset("c") == nil || p.Prompt(d.VisualTarget{AssetID: "c"}) == nil {
		t.Fatal("last look deleted base")
	}
	command("/assets/c/delete", map[string]any{"revision": p.Revision}, 200)
	command("/runs", map[string]any{"revision": p.Revision}, 409)
	command("/visual-prompt-runs", map[string]any{"revision": p.Revision}, 409)
	if count != visualCalls.Load()+outlineCalls.Load() || used != p.UsedMicroCNY() {
		t.Fatal("CRUD sent model or changed fees")
	}
	restarted := openProjectContentPG(t)
	p2, err := restarted.Snapshot(ctx, p.ID)
	if err != nil || !reflect.DeepEqual(p2, p) {
		t.Fatal("restart changed payload")
	}
	fixtureHost, freshHandler := httpapi.New(contentService(t, restarted, nil), nil, "../../../demo", 0)
	freshHandler = withPostgresSession(t, fixtureHost, freshHandler, "workspace")
	rr := httptest.NewRecorder()
	freshHandler.ServeHTTP(rr, httptest.NewRequest("GET", "http://127.0.0.1/api/projects/"+p.ID, nil))
	assertProjectContentView(t, rr.Body.Bytes(), p2)
}

func TestProjectContentPostgresEmptyVisualPersistsWithoutModelPermitOrFees(t *testing.T) {
	store := openProjectContentPG(t)
	s := contentService(t, store, nil)
	s.Authorize = nil
	s.Agent = nil
	p := createProjectContent(t, s, []d.Asset{})
	host, handler := httpapi.New(configuredFixture(s), nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	defer host.Shutdown(context.Background())
	path := "http://127.0.0.1/api/projects/" + p.ID + "/visual-prompt-runs"
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, _ := json.Marshal(map[string]uint64{"revision": p.Revision})
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, httptest.NewRequest("POST", path, bytes.NewReader(raw)))
			statuses <- rr.Code
		}()
	}
	wg.Wait()
	close(statuses)
	ok := 0
	for c := range statuses {
		if c == 200 {
			ok++
		} else if c != 409 {
			t.Fatal("wrong conflict", c)
		}
	}
	if ok != 1 {
		t.Fatal("concurrent first admitted twice")
	}
	p, _ = store.Snapshot(context.Background(), p.ID)
	run := p.LatestRun()
	conn, err := pgx.Connect(context.Background(), os.Getenv("DRAMA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	var payload []byte
	err = conn.QueryRow(context.Background(), "SELECT payload FROM drama_catalog_v1.projects WHERE id=$1", p.ID).Scan(&payload)
	conn.Close(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var stored struct{ Runs []map[string]json.RawMessage }
	if json.Unmarshal(payload, &stored) != nil {
		t.Fatal("PG payload unreadable")
	}
	saved := stored.Runs[len(stored.Runs)-1]
	if string(saved["targets"]) != "[]" || string(saved["calls"]) != "[]" || saved["model"] != nil || saved["budgetMicroCny"] != nil {
		t.Fatal("stored empty exception differs from API contract")
	}
	if run.Error != d.EmptyVisualError || run.Status != "failed" || len(run.Calls) != 0 || p.UsedMicroCNY() != 789 || p.LatestRunByContract(d.CatalogExtractionContract).Status != "completed" {
		t.Fatal("zero branch mismatch")
	}
	restarted := openProjectContentPG(t)
	fresh := contentService(t, restarted, nil)
	fresh.Authorize = nil
	host2, handler2 := httpapi.New(configuredFixture(fresh), nil, "../../../demo", 0)
	handler2 = withPostgresSession(t, host2, handler2, "workspace")
	defer host2.Shutdown(context.Background())
	if err := host2.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handler2.ServeHTTP(rr, httptest.NewRequest("GET", "http://127.0.0.1/api/projects/"+p.ID, nil))
	assertProjectContentView(t, rr.Body.Bytes(), p)
	var view map[string]json.RawMessage
	_ = json.Unmarshal(rr.Body.Bytes(), &view)
	var r map[string]json.RawMessage
	_ = json.Unmarshal(view["run"], &r)
	if string(r["targets"]) != "[]" || r["model"] != nil || r["budgetMicroCny"] != nil {
		t.Fatal("empty exception fabricated fields")
	}
	raw, _ := json.Marshal(map[string]uint64{"revision": p.Revision})
	rr = httptest.NewRecorder()
	handler2.ServeHTTP(rr, httptest.NewRequest("POST", path, bytes.NewReader(raw)))
	if rr.Code != 409 || !strings.Contains(rr.Body.String(), "VISUAL_PROMPTS_ALREADY_STARTED") {
		t.Fatal("empty failure repeated")
	}
	after, _ := restarted.Snapshot(context.Background(), p.ID)
	if !reflect.DeepEqual(p, after) {
		t.Fatal("refresh/restart duplicated task")
	}
}

type contentScriptModel struct {
	count    atomic.Int32
	generate func(ports.Request) (ports.Response, error)
	window   int
}

func (m *contentScriptModel) Capacity() (int, int) {
	if m.window > 0 {
		return m.window, 100
	}
	return 1000000, 10000
}

func (m *contentScriptModel) Generate(ctx context.Context, req ports.Request, observe func(json.RawMessage) error) (ports.Response, error) {
	m.count.Add(1)
	raw, _ := json.Marshal(req)
	if err := observe(raw); err != nil {
		return ports.Response{}, err
	}
	return m.generate(req)
}

func contentResponse(a ...d.Action) ports.Response {
	return ports.Response{Actions: a, FinishReason: "tool_calls", Usage: billing.Usage{Known: true, Input: 100, Output: 30}, Raw: json.RawMessage(`{"fictional":true}`)}
}

func TestProjectContentSingleCategoryAndRunningRecoveryFromCommittedResponse(t *testing.T) {
	for _, contract := range []string{d.OutlineContract, d.VisualContract} {
		for _, status := range []string{"received", "unknown", "pending", "interrupted"} {
			t.Run(contract+"/"+status, func(t *testing.T) {
				store := openProjectContentPG(t)
				model := &contentScriptModel{}
				s := contentService(t, store, model)
				p := createProjectContent(t, s, []d.Asset{{ID: "prop", Kind: "prop", Name: "钥匙"}})
				ctx := context.Background()
				var err error
				if contract == d.OutlineContract {
					p, err = quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "outline"}).StartOutline(ctx, p.ID, p.Revision)
				} else {
					p, err = quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "visual"}).StartVisual(ctx, p.ID, p.Revision)
				}
				if err != nil {
					t.Fatal(err)
				}
				save := contentAction("save_outline", app.SaveOutline{Outline: d.OutlineInput{Summary: "虚构故事"}})
				finish := "finish_outline"
				if contract == d.VisualContract {
					save = contentAction("save_visual_prompts", app.SaveVisualPrompts{Prompts: []app.VisualPromptInput{{Target: app.VisualTargetInput{AssetID: "prop"}, Text: "铜钥匙本体", DesignNotes: "完整设计"}}})
					finish = "finish_visual_prompts"
				}
				r := p.LatestRun()
				// Use the normal Start* frozen identity; a fresh executor must receive
				// real access ports rather than fabricating a historical charge to resume.
				if r.Credit == nil || r.Credit.ActorID != legacyHTTPActor.UserID || r.Credit.AccountID != p.SpaceID || r.Credit.Amount != 0 {
					t.Fatal("normal free quotation did not freeze the initiating actor/account", r.Credit)
				}
				r.End = len(p.ScriptSource.Spans)
				r.Calls = append(r.Calls, d.Call{ID: "persisted", Stage: r.Stage, End: r.End, Status: d.CallStatus(status), Actions: []d.Action{save}, FinishReason: "tool_calls", ReservedMicroCNY: 999, Request: json.RawMessage(`{}`), Usage: billing.Usage{Known: status == "received"}})
				if status == "interrupted" {
					r.Calls[0].Status = "unknown"
					r.Calls[0].ErrorCode = "MODEL_RESPONSE_INTERRUPTED"
					r.Calls[0].Actions = nil
					r.Calls[0].FinishReason = ""
				}
				p.Revision++
				if err = s.Store.Commit(ctx, p, p.Revision-1); err != nil {
					t.Fatal(err)
				}
				model.generate = func(req ports.Request) (ports.Response, error) {
					if status == "interrupted" && model.count.Load() == 1 {
						if len(req.Dialogue) != 0 {
							t.Error("partial interrupted output entered dialogue")
						}
						return contentResponse(save), nil
					}
					if len(req.Dialogue) != 2 || !strings.Contains(req.Dialogue[1].Content, `"after"`) {
						t.Error("recovery did not forward actual applied receipt")
					}
					return contentResponse(contentAction(finish, struct{}{})), nil
				}
				restarted := openProjectContentPG(t)
				fresh := contentService(t, restarted, model)
				err = fresh.Execute(ctx, p.ID)
				after, _ := restarted.Snapshot(ctx, p.ID)
				if status == "received" || status == "interrupted" {
					expected := int32(1)
					if status == "interrupted" {
						expected = 2
					}
					if err != nil || after.LatestRun().Status != "completed" || model.count.Load() != expected {
						t.Fatal("running recovery failed", err, after.LatestRun())
					}
					if contract == d.VisualContract && (len(after.LatestRun().Targets) != 1 || len(after.VisualPrompts) != 1) {
						t.Fatal("single-category empty rule incorrect")
					}
				}
				if status == "unknown" || status == "pending" {
					if after.LatestRun().ErrorCode != "MODEL_RESULT_UNKNOWN" || model.count.Load() != 0 {
						t.Fatal("unclassified request resent", err)
					}
				}
				// A second fresh executor never revives completed/failed tasks.
				calls, rev := model.count.Load(), after.Revision
				if e := fresh.Execute(ctx, p.ID); e != nil {
					t.Fatal(e)
				}
				after2, _ := restarted.Snapshot(ctx, p.ID)
				if calls != model.count.Load() || after2.Revision != rev {
					t.Fatal("terminal task revived")
				}
			})
		}
	}
}

func TestProjectContentPostgresCancelLateResponseOnlyAccountsOriginalRun(t *testing.T) {
	store := openProjectContentPG(t)
	entered, release := make(chan struct{}), make(chan struct{})
	model := &contentScriptModel{}
	model.generate = func(ports.Request) (ports.Response, error) {
		close(entered)
		<-release
		return contentResponse(contentAction("save_visual_prompts", visualFixture())), nil
	}
	s := contentService(t, store, model)
	p := createProjectContent(t, s, pgAssets())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p, err := quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "visual"}).StartVisual(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Execute(ctx, p.ID) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	current, _ := store.Snapshot(ctx, p.ID)
	p, err = s.Cancel(ctx, p.ID, current.Revision)
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	after, _ := store.Snapshot(ctx, p.ID)
	if after.LatestRun().ID != p.LatestRun().ID || after.LatestRun().Status != "cancelled" || len(after.VisualPrompts) != 0 || model.count.Load() != 1 || after.UsedMicroCNY() != 789+440 || after.LatestRun().Calls[0].Response == "" {
		t.Fatal("cancel late adoption/accounting mismatch", after.LatestRun())
	}
	// 视觉取消不撤销 P3 完成权限，新任务独立创建，原取消任务和账目不复活。
	next, err := quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: after.Revision, Operation: "outline"}).StartOutline(ctx, p.ID, after.Revision)
	if err != nil || next.LatestRun().Status != "running" || next.Runs[len(next.Runs)-2].Status != "cancelled" || next.UsedMicroCNY() != after.UsedMicroCNY() || model.count.Load() != 1 {
		t.Fatal("new task changed cancelled ledger or made a model call", err)
	}
}

type contentFailOnceStore struct {
	ports.ProjectStore
	failed atomic.Bool
}

func (s *contentFailOnceStore) Commit(ctx context.Context, p d.Project, rev uint64) error {
	if len(p.VisualPrompts) > 0 && !s.failed.Swap(true) {
		return errors.New("controlled atomic commit failure")
	}
	return s.ProjectStore.Commit(ctx, p, rev)
}

func TestProjectContentPostgresToolCommitFailureAndCapacityPreserveCatalog(t *testing.T) {
	for _, scenario := range []string{"commit", "capacity", "budget", "no-permit"} {
		t.Run(scenario, func(t *testing.T) {
			store := openProjectContentPG(t)
			model := &contentScriptModel{generate: func(ports.Request) (ports.Response, error) {
				return contentResponse(contentAction("save_visual_prompts", visualFixture())), nil
			}}
			s := contentService(t, store, model)
			p := createProjectContent(t, s, pgAssets())
			ctx := context.Background()
			p, err := quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "visual"}).StartVisual(ctx, p.ID, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			want := "AGENT_FAILED"
			switch scenario {
			case "capacity":
				model.window = 20
				want = "CAPACITY_EXCEEDED"
			case "budget":
				s.Authorize = func(context.Context, d.Project, d.Run) (int64, error) { return 1, nil }
				want = "BUDGET_EXHAUSTED"
			case "no-permit":
				s.Authorize = nil
				want = "EXECUTION_NOT_AUTHORIZED"
			case "commit":
				s.Store = &contentFailOnceStore{ProjectStore: store}
			}
			if err = s.Execute(ctx, p.ID); err == nil {
				t.Fatal("fault returned success")
			}
			after, _ := store.Snapshot(ctx, p.ID)
			if after.LatestRun().Status != "failed" || after.LatestRun().ErrorCode != want || len(after.VisualPrompts) != 0 || !reflect.DeepEqual(after.Assets, p.Assets) {
				t.Fatal("fault did not preserve atomic boundary", after.LatestRun())
			}
			if scenario != "commit" && model.count.Load() != 0 {
				t.Fatal("invalid external request sent")
			}
		})
	}
}

func TestProjectContentHTTPDTOSizeSameOriginAndReadOnlyFields(t *testing.T) {
	store := openProjectContentPG(t)
	s := contentService(t, store, nil)
	p := createProjectContent(t, s, []d.Asset{})
	fixtureHost, handler := httpapi.New(configuredFixture(s), nil, "../../../demo", 0)
	handler = withPostgresSession(t, fixtureHost, handler, "workspace")
	path := "http://127.0.0.1/api/projects/" + p.ID + "/settings"
	for _, test := range []struct {
		raw    string
		status int
	}{{`{"revision":2,"settings":{"aspectRatio":"","llmModel":"","imageModel":""}}`, 400}, {`{"revision":2,"settings":{"aspectRatio":"","llmModel":"","imageModel":"","videoModel":null}}`, 400}, {`{"revision":2,"settings":{"aspectRatio":"","llmModel":"","imageModel":"","videoModel":"","unknown":"x"}}`, 400}, {`{"revision":2,"settings":{"aspectRatio":"","llmModel":"","imageModel":"","videoModel":""}}{}`, 400}, {strings.Repeat("x", (1<<20)+1), 413}} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest("POST", path, strings.NewReader(test.raw)))
		if rr.Code != test.status {
			t.Fatal("DTO size/type contract", rr.Code, rr.Body.String())
		}
	}
	raw := fmt.Sprintf(`{"revision":%d}`, p.Revision)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://127.0.0.1/api/projects/"+p.ID+"/visual-prompt-runs", strings.NewReader(raw))
	req.Header.Set("Origin", "http://foreign.invalid")
	handler.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatal("new routes bypass same-origin")
	}
}

func TestProjectContentNaturalCompleteCatalogBeforeControlledFaultChecks(t *testing.T) {
	store := openProjectContentPG(t)
	m := &contentScriptModel{}
	s := contentService(t, store, m)
	p := createProjectContent(t, s, pgAssets())
	ctx := context.Background()
	m.generate = func(req ports.Request) (ports.Response, error) {
		if m.count.Load() == 1 {
			return contentResponse(contentAction("save_visual_prompts", visualFixture())), nil
		}
		if len(req.Dialogue) != 2 || !strings.Contains(req.Dialogue[1].Content, `"after"`) || !strings.Contains(req.Input, "林岚，32岁，左眉尾浅疤，黑发低马尾，黑礼服") {
			t.Error("natural complete content and receipt absent")
		}
		return contentResponse(contentAction("finish_visual_prompts", struct{}{})), nil
	}
	p, err := quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "visual"}).StartVisual(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	p, _ = store.Snapshot(ctx, p.ID)
	if p.LatestRun().Status != "completed" || !p.VisualComplete(p.LatestRun()) || m.count.Load() != 2 || len(p.Asset("c").Looks) != 1 {
		t.Fatal("natural full-directory fixture failed")
	}
	for _, v := range p.VisualPrompts {
		if strings.Contains(v.Text, "右眉尾") {
			t.Fatal("natural fixture was mixed with controlled error")
		}
	}
}

func TestProjectContentEinoRejectsForeignToolsThenRepairsThroughRealReceipt(t *testing.T) {
	for _, contract := range []string{d.OutlineContract, d.VisualContract} {
		t.Run(contract, func(t *testing.T) {
			store := openProjectContentPG(t)
			model := &contentScriptModel{}
			s := contentService(t, store, model)
			p := createProjectContent(t, s, []d.Asset{{ID: "prop", Kind: "prop", Name: "钥匙"}})
			before := d.Clone(p)
			ctx := context.Background()
			var err error
			if contract == d.OutlineContract {
				p, err = quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "outline"}).StartOutline(ctx, p.ID, p.Revision)
			} else {
				p, err = quotedLegacyService(t, s, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "visual"}).StartVisual(ctx, p.ID, p.Revision)
			}
			if err != nil {
				t.Fatal(err)
			}
			model.generate = func(req ports.Request) (ports.Response, error) {
				n := model.count.Load()
				if len(req.Tools) != len(app.TaskTools(contract)) || strings.Contains(req.Tools[0].Name, "catalog") {
					t.Error("P3 tool exposed in new task")
				}
				switch n {
				case 1:
					return contentResponse(contentAction("save_catalog", app.CatalogChangeSet{Assets: catalogInputs([]d.Asset{})})), nil
				case 2:
					if len(req.Dialogue) != 2 || !strings.Contains(req.Dialogue[1].Content, `"ok":false`) {
						t.Error("foreign tool rejection never delivered")
					}
					if contract == d.OutlineContract {
						return contentResponse(contentAction("save_outline", app.SaveOutline{Outline: d.OutlineInput{Summary: "完整虚构故事"}})), nil
					}
					return contentResponse(contentAction("save_visual_prompts", app.SaveVisualPrompts{Prompts: []app.VisualPromptInput{{Target: app.VisualTargetInput{AssetID: "prop"}, Text: "钥匙本体", DesignNotes: "设计"}}})), nil
				case 3:
					finish := "finish_outline"
					if contract == d.VisualContract {
						finish = "finish_visual_prompts"
					}
					return contentResponse(contentAction(finish, struct{}{})), nil
				}
				return ports.Response{}, fmt.Errorf("extra request")
			}
			if err = s.Execute(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
			p, _ = store.Snapshot(ctx, p.ID)
			if p.LatestRun().Status != "completed" || p.LatestRun().Calls[0].Actions[0].Status != "rejected" || !reflect.DeepEqual(p.Assets, before.Assets) {
				t.Fatal("tool boundary changed catalog or failed to repair")
			}
		})
	}
}
