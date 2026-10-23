package httpapi_test

import (
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/adapter/httpapi"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"

	"github.com/jackc/pgx/v5"
)

// A test-only pause AFTER a real commit makes the two crash boundaries
// repeatable. Persistence, locking, Eino HTTP and adoption remain production code.
// List is scoped to this test's row so recovery never launches another project.
type pausedProjectStore struct {
	ports.ProjectStore
	id, boundary, gate string
}

func (s pausedProjectStore) ForProjectSpace(owner string) ports.ProjectStore {
	s.ProjectStore = s.ProjectStore.(ports.ScopedProjects).ForProjectSpace(owner)
	return s
}

func (s pausedProjectStore) Commit(ctx context.Context, p d.Project, rev uint64) error {
	if p.ID != s.id {
		return fault.ErrInvalid
	}
	if err := s.ProjectStore.Commit(ctx, p, rev); err != nil {
		return err
	}
	r := p.LatestRun()
	matched := r != nil && len(r.Calls) == 1 && r.Calls[0].Status == d.CallStatus(s.boundary)
	if s.boundary == "tool-applied" && r != nil && len(r.Calls) == 1 && len(r.Calls[0].Actions) == 2 {
		matched = r.Calls[0].Actions[0].Status == "applied" && r.Calls[0].Actions[1].Status == "pending"
	}
	if s.boundary == "visual-stage" && r != nil {
		matched = r.Stage == "visualPrompt" && r.Status == "running" && len(r.Calls) == 2 && r.Calls[1].Status == "applied"
	}
	if matched {
		if err := os.WriteFile(s.gate, []byte(s.boundary), 0600); err != nil {
			return err
		}
		select {} // parent SIGKILLs this process; no simulated exception or rollback
	}
	return nil
}

// Re-executed only by TestRealProcessKillAndTCPFailureRecovery with a local
// provider endpoint and dummy key. It never loads .env.local or a real API key.
func TestFaultWorkerProcess(t *testing.T) {
	if os.Getenv("DRAMA_P3_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	ctx := context.Background()
	store, err := pgfixture.Open(t, ctx, os.Getenv("DRAMA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model, err := modeladapter.New(ctx, "dummy-local-fault-test", os.Getenv("DRAMA_P3_LOCAL_ENDPOINT"), md.DeepSeekModelID)
	if err != nil {
		t.Fatal(err)
	}
	scoped := pausedProjectStore{ProjectStore: store, id: os.Getenv("DRAMA_P3_TEST_PROJECT"), boundary: os.Getenv("DRAMA_P3_PAUSE"), gate: os.Getenv("DRAMA_P3_GATE")}
	service := &app.Service{Resolver: store, Access: store, Credits: store, Prices: &frozenFixturePrices{version: 1}, Authorize: freeAuthorization, Store: scoped, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: model}}
	host, handler := httpapi.New(configuredFixture(service), nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	defer server.Close()
	if err = host.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("DRAMA_P3_READY"), []byte("http://"+listener.Addr().String()), 0600); err != nil {
		t.Fatal(err)
	}
	select {} // actual persistent server, stopped by the parent process only
}

type faultChild struct {
	cmd     *exec.Cmd
	log     *os.File
	stopped bool
	exit    string
}

func (c *faultChild) kill() {
	if !c.stopped {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
		if c.cmd.ProcessState != nil {
			c.exit = c.cmd.ProcessState.String()
		}
		_ = c.log.Close()
		c.stopped = true
	}
}

func startFaultChild(t *testing.T, database, id, endpoint, boundary, dir string) (*faultChild, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "ready-"+identity.New("process"))
	log, err := os.CreateTemp(dir, "worker-*.log")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestFaultWorkerProcess$", "-test.timeout=90s")
	// Deliberately pass only the environment needed by this local test.
	cmd.Env = []string{"DRAMA_TEST_REDIS_ADDRESS=" + os.Getenv("DRAMA_TEST_REDIS_ADDRESS"), "DRAMA_TEST_REDIS_NAMESPACE=" + os.Getenv("DRAMA_TEST_REDIS_NAMESPACE"), "PATH=" + os.Getenv("PATH"), "DRAMA_P3_CHILD=1", "DRAMA_TEST_DATABASE_URL=" + database, "DRAMA_P3_TEST_PROJECT=" + id, "DRAMA_P3_LOCAL_ENDPOINT=" + endpoint, "DRAMA_P3_PAUSE=" + boundary, "DRAMA_P3_GATE=" + filepath.Join(dir, "gate"), "DRAMA_P3_READY=" + ready}
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	child := &faultChild{cmd: cmd, log: log}
	t.Cleanup(child.kill)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if raw, e := os.ReadFile(ready); e == nil {
			return child, string(raw)
		}
		time.Sleep(10 * time.Millisecond)
	}
	child.kill()
	raw, _ := os.ReadFile(log.Name())
	t.Fatalf("worker did not start: %s", raw)
	return nil, ""
}

func waitFaultProject(t *testing.T, store ports.ProjectStore, id string, check func(d.Project) bool) d.Project {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var p d.Project
	for time.Now().Before(deadline) {
		var err error
		p, err = store.Snapshot(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if check(p) {
			return p
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("project did not reach expected boundary: %+v", p.LatestRun())
	return p
}

func TestRealProcessKillAndTCPFailureRecovery(t *testing.T) {
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("real PG opt-in; local endpoint only, no paid calls")
	}
	for _, scenario := range []string{"kill-pending", "kill-received", "kill-tool-applied", "kill-applied", "kill-interrupted", "kill-visual-stage", "tcp-response-cut", "connection-not-sent"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			store, err := pgfixture.Open(t, ctx, database)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			service := &app.Service{Resolver: store, Access: store, Credits: store, Prices: &frozenFixturePrices{version: 1}, Authorize: freeAuthorization, Store: store, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Models: &modelregistry.Registry{Model: &protocolModel{}}}
			p, err := fixtureProject(t, service, legacyHTTPActor).Create(ctx, "P3本地故障验证:"+scenario, "fault.txt", []byte("祖父把铜钥匙交给小雨。\n另一把铜钥匙放在门边。"), 0)
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
			p, err = quotedLegacyService(t, service, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "asset-preparation"}).Start(ctx, p.ID, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "kill-tool-applied" {
				p.Assets = []d.Asset{
					{ID: identity.New("asset"), Kind: "character", Name: "祖父", Description: "待修改说明", SourceIDs: []string{p.ScriptSource.Spans[0].ID}},
					{ID: identity.New("asset"), Kind: "prop", Name: "铜钥匙", SourceIDs: []string{p.ScriptSource.Spans[0].ID}},
					{ID: identity.New("asset"), Kind: "character", Name: "错误人群项", SourceIDs: []string{p.ScriptSource.Spans[1].ID}},
				}
				p.Revision++
				if err = store.Commit(ctx, p, p.Revision-1); err != nil {
					t.Fatal(err)
				}
			}
			var calls, visualCalls atomic.Int32
			releaseReply := make(chan struct{})
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var wire struct {
					Messages []struct {
						Role, Content string
						Reasoning     string `json:"reasoning_content"`
						ToolID        string `json:"tool_call_id"`
					} `json:"messages"`
					Tools []struct{ Function struct{ Name string } } `json:"tools"`
				}
				if json.NewDecoder(r.Body).Decode(&wire) != nil {
					t.Error("invalid wire")
					return
				}
				if modelregistry.FreeVisualWireReply(w, wire) {
					visualCalls.Add(1)
					return
				}
				calls.Add(1)
				expectedMessages := 2
				if scenario == "kill-applied" && calls.Load() == 2 {
					expectedMessages = 4
				}
				if len(wire.Messages) != expectedMessages {
					t.Error("invalid actual Eino request")
					return
				}
				if expectedMessages == 4 && (wire.Messages[2].Role != "assistant" || wire.Messages[2].Reasoning != "saved reasoning" || wire.Messages[3].Role != "tool" || wire.Messages[3].ToolID != "save") {
					t.Error("current task dialogue lost after SIGKILL")
					return
				}
				if scenario == "kill-pending" {
					<-releaseReply
					return
				}
				if scenario == "tcp-response-cut" || (scenario == "kill-interrupted" && calls.Load() == 1) {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Content-Length", "10000")
					w.WriteHeader(200)
					_, _ = w.Write([]byte(`{"id":"cut","choices":[`))
					w.(http.Flusher).Flush()
					conn, _, e := w.(http.Hijacker).Hijack()
					if e == nil {
						conn.Close() // real TCP close halfway through a response
					}
					return
				}
				var input struct {
					ScriptSource []d.SourceSpan `json:"source"`
					Assets       []d.Asset      `json:"assets"`
				}
				if json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil || len(input.ScriptSource) != 2 {
					t.Error("source not preserved")
					return
				}
				if len(input.Assets) == 0 {
					input.Assets = []d.Asset{{Kind: "character", Name: "祖父", SourceIDs: []string{input.ScriptSource[0].ID}}, {Kind: "prop", Name: "铜钥匙", SourceIDs: []string{input.ScriptSource[0].ID}}, {Kind: "prop", Name: "另一把铜钥匙", SourceIDs: []string{input.ScriptSource[1].ID}}}
				}
				draft := app.CatalogChangeSet{Assets: catalogInputs(input.Assets)}
				if scenario == "kill-tool-applied" && calls.Load() == 1 {
					replacement := input.Assets[0]
					replacement.Description = "姓名未说明"
					draft = app.CatalogChangeSet{Assets: catalogInputs([]d.Asset{replacement, {Kind: "prop", Name: "另一把铜钥匙", SourceIDs: []string{input.ScriptSource[1].ID}}}), RemoveIDs: []string{input.Assets[2].ID}}
				}
				text, _ := json.Marshal(draft)
				actions := []any{map[string]any{"id": "save", "type": "function", "function": map[string]string{"name": "save_catalog", "arguments": string(text)}}, map[string]any{"id": "finish", "type": "function", "function": map[string]string{"name": "finish_batch", "arguments": "{}"}}}
				if scenario == "kill-applied" {
					if calls.Load() == 1 {
						actions = actions[:1]
					}
					if calls.Load() == 2 {
						actions = actions[1:]
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "local-fault-fixture", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "reasoning_content": "saved reasoning", "tool_calls": actions}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 40, "total_tokens": 140}})
			}))
			defer provider.Close()
			defer close(releaseReply)
			endpoint := provider.URL
			if scenario == "connection-not-sent" {
				provider.Close()
			}
			dir := t.TempDir()
			boundary := strings.TrimPrefix(scenario, "kill-")
			if scenario == "kill-interrupted" {
				boundary = "unknown"
			}
			if boundary == "pending" || !strings.HasPrefix(scenario, "kill-") {
				boundary = ""
			}
			child, _ := startFaultChild(t, database, p.ID, endpoint, boundary, dir)
			beforeKill := waitFaultProject(t, store, p.ID, func(q d.Project) bool {
				if scenario == "kill-visual-stage" {
					_, err := os.Stat(filepath.Join(dir, "gate"))
					return err == nil && q.LatestRun().Stage == "visualPrompt" && q.LatestRun().Status == "running"
				}
				if len(q.LatestRun().Calls) != 1 && !(scenario == "tcp-response-cut" && len(q.LatestRun().Calls) == 3) {
					return false
				}
				c := q.LatestRun().Calls[0]
				switch scenario {
				case "kill-pending":
					return calls.Load() == 1 && c.Status == "pending" && len(c.Request) > 0
				case "kill-received", "kill-applied", "kill-tool-applied", "kill-interrupted":
					_, err := os.Stat(filepath.Join(dir, "gate"))
					if err != nil {
						return false
					}
					if scenario == "kill-tool-applied" {
						return c.Actions[0].Status == "applied" && c.Actions[1].Status == "pending"
					}
					return c.Status == d.CallStatus(boundary)
				default:
					return q.LatestRun().Status == "failed"
				}
			})
			child.kill()
			if !strings.Contains(child.exit, "killed") {
				t.Fatal("worker did not exit by actual SIGKILL", child.exit)
			}
			beforeRestartCalls := calls.Load()
			restarted, api := startFaultChild(t, database, p.ID, endpoint, "", dir)
			after := waitFaultProject(t, store, p.ID, func(q d.Project) bool { return q.LatestRun().Status != "running" })
			response, err := http.Get(api + "/api/projects/" + p.ID)
			if err != nil {
				t.Fatal(err)
			}
			var view struct {
				Assets []d.Asset                          `json:"assets"`
				Run    struct{ Status, ErrorCode string } `json:"run"`
				Used   int64                              `json:"usedOrReservedMicroCny"`
			}
			err = json.NewDecoder(response.Body).Decode(&view)
			response.Body.Close()
			if err != nil || response.StatusCode != 200 || view.Used != after.UsedMicroCNY() || view.Run.Status != string(after.LatestRun().Status) {
				t.Fatal("API and PG disagree", err)
			}
			restarted.kill()
			finalRun := after.LatestRun()
			if scenario == "kill-received" || scenario == "kill-applied" || scenario == "kill-tool-applied" || scenario == "kill-interrupted" || scenario == "kill-visual-stage" {
				expectedCalls := 2
				if scenario == "kill-applied" || scenario == "kill-interrupted" {
					expectedCalls = 3
				}
				expectedUsed := int64(expectedCalls) * 520
				firstSaved := 0
				if scenario == "kill-interrupted" {
					expectedUsed = 1040 + finalRun.Calls[0].ReservedMicroCNY
					firstSaved = 1
					if finalRun.Calls[0].Status != "unknown" || finalRun.Calls[0].ErrorCode != "MODEL_RESPONSE_INTERRUPTED" || len(finalRun.Calls[0].Actions) != 0 {
						t.Fatal("interrupted receipt or reservation lost on SIGKILL")
					}
				}
				if finalRun.Status != "completed" || int(calls.Load()) != expectedCalls || len(after.Assets) != 3 || after.UsedMicroCNY() != expectedUsed || len(finalRun.Calls) != expectedCalls+2 || visualCalls.Load() != 2 {
					t.Fatal("saved response repeated, lost or not recovered")
				}
				if scenario == "kill-tool-applied" && (after.Assets[0].Description != "姓名未说明" || after.Assets[1].ID != p.Assets[1].ID || after.Assets[2].Name != "另一把铜钥匙") {
					t.Fatal("partial replace/delete/add or untouched object lost during actual recovery")
				}
				for i, a := range after.Assets {
					if a.ID != finalRun.Calls[firstSaved].After[i].ID || a.ID != view.Assets[i].ID {
						t.Fatal("identity changed during recovery or readback")
					}
					if len(beforeKill.Assets) > 0 && a.ID != beforeKill.Assets[i].ID {
						t.Fatal("saved catalog tool was applied again after crash")
					}
				}
			} else {
				if finalRun.Status != "failed" || calls.Load() != beforeRestartCalls || len(after.Assets) != 0 {
					t.Fatal("unknown/failed request resent or adopted")
				}
				if scenario == "connection-not-sent" {
					if calls.Load() != 0 || after.UsedMicroCNY() != 0 || finalRun.Calls[0].Status != "rejected" {
						t.Fatal("definitely unsent request was charged")
					}
				} else if after.UsedMicroCNY() != beforeKill.UsedMicroCNY() || after.UsedMicroCNY() == 0 {
					t.Fatal("unknown reservation was released or changed")
				}
				if scenario == "tcp-response-cut" && !strings.Contains(finalRun.Calls[0].Response, `"id":"cut"`) {
					t.Fatal("partial wire response lost")
				}
				if scenario == "tcp-response-cut" && (calls.Load() != 3 || len(finalRun.Calls) != 3 || finalRun.ErrorCode != "MODEL_RESPONSE_INTERRUPTED") {
					t.Fatal("transport retry limit lost across restart")
				}
			}
			// A second actual restart must neither resend nor change the result.
			third, _ := startFaultChild(t, database, p.ID, endpoint, "", dir)
			time.Sleep(150 * time.Millisecond)
			stable, err := store.Snapshot(ctx, p.ID)
			third.kill()
			if err != nil || calls.Load()+visualCalls.Load() != beforeRestartCalls+int32(len(finalRun.Calls)-len(beforeKill.LatestRun().Calls)) || stable.Revision != after.Revision {
				t.Fatalf("second restart repeated work: err=%v calls=%d expected=%d revision=%d expected=%d run=%+v", err, calls.Load(), beforeRestartCalls+int32(len(finalRun.Calls)-len(beforeKill.LatestRun().Calls)), stable.Revision, after.Revision, stable.LatestRun())
			}
			if root := os.Getenv("DRAMA_P3_EVIDENCE_DIR"); root != "" {
				data, _ := json.MarshalIndent(map[string]any{"scenario": scenario, "provider": "local HTTP protocol fixture; not semantic proof or actual invoice", "actualSIGKILL": true, "workerExit": child.exit, "workerPID": child.cmd.Process.Pid, "beforeKill": beforeKill, "afterRestart": after, "callsBeforeRestart": beforeRestartCalls, "callsAfterTwoRestarts": calls.Load(), "apiReadback": view, "passed": true}, "", "  ")
				file, e := os.OpenFile(filepath.Join(root, "p3-fault-"+scenario+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if e != nil {
					t.Fatal(e)
				}
				_, e = file.Write(data)
				closeErr := file.Close()
				if e != nil || closeErr != nil {
					t.Fatal(e, closeErr)
				}
			}
			t.Logf("%s: killed and restarted twice; endpoint calls=%d, result=%s/%s, used/reserved=%d", scenario, calls.Load(), finalRun.Status, finalRun.ErrorCode, after.UsedMicroCNY())
		})
	}
}

func (s pausedProjectStore) RunningProjectIDs(ctx context.Context) ([]string, error) {
	p, err := s.Snapshot(ctx, s.id)
	if err != nil {
		return nil, err
	}
	if r := p.LatestRun(); r != nil && r.Status == "running" {
		return []string{s.id}, nil
	}
	return []string{}, nil
}
