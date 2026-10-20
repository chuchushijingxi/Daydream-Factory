package httpapi_test

import (
	"bytes"
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/imagethumbnail"
	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/adapter/taskruntime"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/modelusage"
	"drama-agent/internal/application/ports"
	project "drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
	"github.com/jackc/pgx/v5"
)

type mockDrawOnce struct{ reads atomic.Int32 }

// Faults occur around the real PG transaction, never instead of its effects.
type mediaCreditFaults struct {
	ports.CreditLedger
	deductUnknown, refundBefore, refundUnknown atomic.Bool
	deductCalls                                atomic.Int32
	entered, proceed                           chan struct{}
}

type mediaSafeRetries struct {
	ports.TaskProvider
	attempts atomic.Int32
	outbound atomic.Int32
}

func (p *mediaSafeRetries) Submit(ctx context.Context, b td.Binding, id string, raw json.RawMessage) (td.Receipt, error) {
	if p.attempts.Add(1) <= 2 {
		return td.Receipt{Outcome: "not_sent", Billing: td.Billing{Status: "known"}}, nil
	}
	p.outbound.Add(1)
	return p.TaskProvider.Submit(ctx, b, id, raw)
}

func (f *mediaCreditFaults) Deduct(ctx context.Context, c credits.Change) (credits.Entry, error) {
	f.deductCalls.Add(1)
	e, err := f.CreditLedger.Deduct(ctx, c)
	if err != nil {
		return e, err
	}
	if f.entered != nil {
		close(f.entered)
		<-f.proceed
	}
	if f.deductUnknown.Swap(false) {
		return credits.Entry{}, fault.New("CREDIT_COMMIT_UNKNOWN", "injected lost debit acknowledgement")
	}
	return e, nil
}
func (f *mediaCreditFaults) Refund(ctx context.Context, c credits.Change) (credits.Entry, error) {
	if f.refundBefore.Swap(false) {
		return credits.Entry{}, errors.New("injected refund unavailable before commit")
	}
	e, err := f.CreditLedger.Refund(ctx, c)
	if err == nil && f.refundUnknown.Swap(false) {
		return credits.Entry{}, fault.New("CREDIT_COMMIT_UNKNOWN", "injected lost refund acknowledgement")
	}
	return e, err
}

func (r *mockDrawOnce) Read(p []byte) (int, error) {
	if r.reads.Add(1) != 1 {
		return 0, errors.New("random must not be read again")
	}
	p[0] = 49
	return 1, nil
}

func TestMockBehaviorRealPGHTTPFrozenDelayProbabilityAndRecovery(t *testing.T) {
	path := os.Getenv("DRAMA_TEST_CONFIG_FILE")
	if path == "" {
		t.Skip("dedicated accounts PG/S3/NSQ required")
	}
	cfg, err := appconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(cfg.Database.URL)
	if !strings.HasPrefix(u.Path, "/drama_accounts_test_") || !strings.HasPrefix(cfg.Storage.Bucket, "drama-script-test-accounts-") {
		t.Fatal("dedicated resources required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	store, err := pgfixture.Open(t, ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	before, err := store.ModelConfigSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		current, e := store.ModelConfigSnapshot(context.Background())
		if e == nil {
			before.Revision = current.Revision + 1
			e = store.ModelConfigCommit(context.Background(), before, current.Revision)
		}
		if e != nil {
			t.Error("restore isolated registry", e)
		}
	}()
	cipher, err := postgres.OpenModelCipher(filepath.Join(t.TempDir(), "mock.key"), true)
	if err != nil {
		t.Fatal(err)
	}
	draw := &mockDrawOnce{}
	configuration := &modelconfig.Service{Store: store, Projects: store, Cipher: cipher, MockRandom: draw, Factory: func(context.Context, string, string, string, md.ConfiguredModel) (ports.Model, error) {
		t.Error("Mock invoked text model")
		return nil, errors.New("text model forbidden")
	}}
	objects, err := s3objects.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	if err = objects.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	files := &uploadapp.Service{Store: store, Objects: objects, Thumbnails: imagethumbnail.FFmpeg{Settings: cfg.Media}, CommitCheckTimeout: cfg.Uploads.CommitCheckTimeout, CleanupTimeout: cfg.Uploads.CleanupTimeout}
	cfg.NSQ.Topic = "drama_mock_behavior_test_" + strings.TrimPrefix(identity.New("q"), "q_")
	cfg.Tasks.PollInterval = 20 * time.Millisecond
	runtime, queue, err := taskruntime.New(cfg, store, objects, configuration, redisruntime.ConnectDatabase(t, cfg.Database.URL), files)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	configuration.Tasks = runtime.Service
	runtime.Service.Permit = &project.MediaCreditPermit{Ledger: store, Tasks: runtime.Service.Store}
	accountService := &accounts.Service{Store: store, Hasher: passwordhash.Bcrypt{}}
	newUser := func() accounts.User {
		email := identity.New("mock") + "@example.test"
		challenge := accounts.RandomToken()
		if e := store.PutVerification(ctx, challenge, email, "register", accounts.Digest(challenge+":483726"), time.Now().Add(time.Minute)); e != nil {
			t.Fatal(e)
		}
		if e := store.ActivateVerification(ctx, challenge, true); e != nil {
			t.Fatal(e)
		}
		_, code, e := accountService.CreateCode(ctx, ports.Actor{UserID: "isolated-fixture", Role: ports.RoleOperator}, time.Now().Add(time.Hour))
		if e != nil {
			t.Fatal(e)
		}
		user, e := accountService.Register(ctx, email, "Mock行为隔离验证", "mock-fixture-password", "483726", code)
		if e != nil {
			t.Fatal(e)
		}
		return user
	}
	owner, other := newUser(), newUser()
	// Explicit role fixture in this guarded isolated database only; authentication
	// itself still reads the actual PG role/session, with no production bypass.
	if _, err = conn.Exec(ctx, "UPDATE drama_catalog_v1.users SET role='operator' WHERE id=$1", owner.ID); err != nil {
		t.Fatal(err)
	}
	p := d.Project{ID: identity.New("mock_behavior"), OwnerID: owner.ID, Revision: 1, Title: "Mock图片视频免费隔离验证", ScriptSource: d.ScriptSource{Version: 1}, Assets: []d.Asset{{ID: "asset", Kind: "prop", Name: "铜钥匙"}}, Runs: []d.Run{{ID: "catalog", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: d.RunCompleted}}, VisualPrompts: []d.VisualPrompt{{VisualPromptInput: d.VisualPromptInput{Target: d.VisualTarget{AssetID: "asset"}, Text: "铜制钥匙，完整免费夹具外观"}, SourceVersion: 1, Origin: "user"}}}
	seedMediaHTTPStoryboard(&p)
	if err = store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	service := &project.Service{Credits: store, Store: store, Models: configuration, Prices: configuration, Files: files, MediaTasks: runtime.Service}
	h, handler := httpapi.New(service, configuration, "../../../demo", 0)
	h.Accounts = accountService
	h.Authenticator = accountService
	h.Files = files
	defer h.Shutdown(context.Background())
	server := httptest.NewServer(handler)
	defer server.Close()
	request := func(method, path, token string, body any) (int, []byte, http.Header) {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r, e := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		if token != "" {
			addSessionCookie(r, token)
		}
		if strings.HasSuffix(path, "/file") {
			r.Header.Set("Range", "bytes=0-31")
		}
		resp, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		data, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, data, resp.Header
	}
	login := func(email, audience string) string {
		status, raw, headers := request("POST", "/api/auth/login", "", map[string]string{"email": email, "password": "mock-fixture-password", "audience": audience})
		if status != 200 {
			t.Fatal("login", status, string(raw))
		}
		cookies := (&http.Response{Header: headers}).Cookies()
		if len(cookies) != 1 {
			t.Fatal("session cookie")
		}
		return cookies[0].Value
	}
	op := login(owner.Email, "operations")
	workspace := login(owner.Email, "workspace")
	foreign := login(other.Email, "workspace")
	preset, _ := md.OfficialPreset("mock")
	on := true
	input := modelconfig.ModelConfigInput{Revision: before.Revision, Connection: modelconfig.ConnectionInput{ID: preset.ID, Name: preset.Name, Kind: preset.Kind, Protocol: preset.Protocol}}
	for _, m := range preset.Models {
		input.Connection.Models = append(input.Connection.Models, modelconfig.ModelEnableInput{ID: m.ID, ModelID: m.ModelID, Category: m.Category, Enabled: &on, CreditPrice: &credits.PriceRule{}, MockBehavior: &md.MockBehavior{}})
	}
	save := func(image, video md.MockBehavior) {
		t.Helper()
		input.Connection.Models[0].MockBehavior = &image
		input.Connection.Models[1].MockBehavior = &video
		status, raw, _ := request("POST", "/api/model-config", op, input)
		if status != 200 {
			t.Fatal("CAS save", status, string(raw))
		}
		var view modelconfig.ModelConfigView
		if json.Unmarshal(raw, &view) != nil {
			t.Fatal("view")
		}
		input.Revision = view.Revision
		status, raw, _ = request("GET", "/api/model-config", op, nil)
		if status != 200 {
			t.Fatal(status, string(raw))
		}
		var read modelconfig.ModelConfigView
		json.Unmarshal(raw, &read)
		for _, c := range read.Connections {
			if c.ID == "mock" && (*c.Models[0].MockBehavior != image || *c.Models[1].MockBehavior != video) {
				t.Fatal("independent config did not persist")
			}
		}
	}
	save(md.MockBehavior{DelaySeconds: 1, FailurePercent: 50}, md.MockBehavior{DelaySeconds: 1, FailurePercent: 100})
	base := "/api/projects/" + p.ID + "/media-tasks"
	start := func(model string, target project.MediaTarget) (project.MediaCommand, project.MediaView) {
		t.Helper()
		command := project.MediaCommand{CommandID: identity.New("command"), Revision: p.Revision, ModelID: model, Target: target}
		code, quoted, _ := request("POST", "/api/projects/"+p.ID+"/quote", workspace, project.QuoteInput{Revision: p.Revision, Operation: "media", ModelID: model, Target: &target})
		var quote project.ChargeQuote
		if code != 200 || json.Unmarshal(quoted, &quote) != nil {
			t.Fatal("quote", code, string(quoted))
		}
		command.BillingBasis = quote.Basis
		status, raw, _ := request("POST", base, workspace, command)
		var view project.MediaView
		if status != 202 || json.Unmarshal(raw, &view) != nil {
			t.Fatal("start", status, string(raw))
		}
		return command, view
	}
	// Ordinary callers cannot supply simulation overrides or operate on another owner.
	for _, field := range []string{"delayMs", "forceFail", "delaySeconds", "failurePercent", "videoOptions"} {
		status, _, _ := request("POST", base, workspace, map[string]any{"commandId": identity.New("bad"), "revision": p.Revision, "modelId": md.MockImageModelID, "target": map[string]string{"assetId": "asset"}, field: 1})
		if status != 400 {
			t.Fatal("override accepted", field, status)
		}
	}
	status, _, _ := request("POST", base, foreign, project.MediaCommand{CommandID: identity.New("foreign"), Revision: 1, ModelID: md.MockImageModelID, Target: project.MediaTarget{AssetID: "asset"}})
	if status != 404 {
		t.Fatal("foreign start", status)
	}
	command, first := start(md.MockImageModelID, project.MediaTarget{AssetID: "asset"})
	saved, err := runtime.Service.Store.Task(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	var frozen md.MockExecution
	if json.Unmarshal(saved.State.Binding.ProviderConfig, &frozen) != nil || !frozen.Fail || frozen.DelaySeconds != 1 || draw.reads.Load() != 1 {
		t.Fatal("start decision not durable", frozen, draw.reads.Load())
	}
	save(md.MockBehavior{}, md.MockBehavior{DelaySeconds: 1, FailurePercent: 100})
	status, raw, _ := request("POST", base, workspace, command)
	var same project.MediaView
	json.Unmarshal(raw, &same)
	if status != 202 || same.ID != first.ID || draw.reads.Load() != 1 {
		t.Fatal("duplicate command redrew", status, string(raw))
	}
	// New runtime over the same PG and private result store is the restart boundary.
	restarted, q2, err := taskruntime.New(cfg, store, objects, configuration, redisruntime.ConnectDatabase(t, cfg.Database.URL), files)
	if err != nil {
		t.Fatal(err)
	}
	defer q2.Close()
	restarted.Service.Permit = &project.MediaCreditPermit{Ledger: store, Tasks: restarted.Service.Store}
	finish := func(v project.MediaView, want td.Status) {
		t.Helper()
		started := time.Now()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
			if e := restarted.Service.Drive(ctx, v.ID); e != nil {
				t.Fatal(e)
			}
			code, data, _ := request("GET", base+"/"+v.ID, workspace, nil)
			if code != 200 || json.Unmarshal(data, &v) != nil {
				t.Fatal("read", code, string(data))
			}
			if v.Status.Terminal() {
				if v.Status != want {
					t.Fatal("terminal", v.Status, v.Error)
				}
				if want == td.Failed && time.Since(started) < time.Second {
					t.Fatal("seconds delay skipped")
				}
				if v.Billing.Status != "known" || v.Billing.KnownMicroCNY != 0 {
					t.Fatal("Mock cost")
				}
				if want == td.Succeeded {
					if v.Result == nil || len(v.Result.Files) != 1 {
						t.Fatal("missing result")
					}
					f := v.Result.Files[0]
					code, body, headers := request("GET", f.ContentURL, workspace, nil)
					if code != 206 || len(body) != 32 || headers.Get("ETag") != "\""+f.SHA256+"\"" {
						t.Fatal("private range result", code)
					}
					code, _, _ = request("GET", f.ContentURL, foreign, nil)
					if code != 404 {
						t.Fatal("foreign file", code)
					}
				}
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("timeout")
	}
	finish(first, td.Failed)
	if draw.reads.Load() != 1 {
		t.Fatal("recovery redrew")
	}
	_, image := start(md.MockImageModelID, project.MediaTarget{AssetID: "asset"})
	finish(image, td.Succeeded)
	_, videoFail := start(md.MockVideoModelID, project.MediaTarget{EpisodeID: "episode", ShotID: "shot"})
	finish(videoFail, td.Failed)
	save(md.MockBehavior{}, md.MockBehavior{})
	_, video := start(md.MockVideoModelID, project.MediaTarget{EpisodeID: "episode", ShotID: "shot"})
	finish(video, td.Succeeded)
	// Concurrent starts publish only the winning persisted task; every caller
	// receives that task's frozen decision, regardless of losing preparations.
	save(md.MockBehavior{DelaySeconds: 1, FailurePercent: 50}, md.MockBehavior{})
	configuration.MockRandom = nil
	concurrent := project.MediaCommand{CommandID: identity.New("concurrent"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: project.MediaTarget{AssetID: "asset"}}
	quoteStatus, quoteBody, _ := request("POST", "/api/projects/"+p.ID+"/quote", workspace, project.QuoteInput{Revision: p.Revision, Operation: "media", ModelID: concurrent.ModelID, Target: &concurrent.Target})
	var concurrentQuote project.ChargeQuote
	if quoteStatus != 200 || json.Unmarshal(quoteBody, &concurrentQuote) != nil {
		t.Fatal("concurrent quote", quoteStatus)
	}
	concurrent.BillingBasis = concurrentQuote.Basis
	ids := make(chan string, 8)
	var starts sync.WaitGroup
	for i := 0; i < 8; i++ {
		starts.Add(1)
		go func() {
			defer starts.Done()
			code, body, _ := request("POST", base, workspace, concurrent)
			var v project.MediaView
			if code != 202 || json.Unmarshal(body, &v) != nil {
				t.Errorf("concurrent start %d", code)
				return
			}
			ids <- v.ID
		}()
	}
	starts.Wait()
	close(ids)
	winner := ""
	for id := range ids {
		if winner == "" {
			winner = id
		}
		if id != winner {
			t.Fatal("concurrent command accepted multiple tasks")
		}
	}
	if winner == "" {
		t.Fatal("no concurrent receipt")
	}
	winnerTask, err := runtime.Service.Store.Task(ctx, winner)
	if err != nil || json.Unmarshal(winnerTask.State.Binding.ProviderConfig, &frozen) != nil {
		t.Fatal(err)
	}
	code, body, _ := request("GET", base+"/"+winner, workspace, nil)
	var winnerView project.MediaView
	if code != 200 || json.Unmarshal(body, &winnerView) != nil {
		t.Fatal("winner read")
	}
	want := td.Succeeded
	if frozen.Fail {
		want = td.Failed
	}
	finish(winnerView, want)
	// Cancellation after a durable submit retains the frozen provider evidence,
	// and a later drive cannot resurrect outputs or adopt anything.
	save(md.MockBehavior{DelaySeconds: 60, FailurePercent: 100}, md.MockBehavior{})
	_, canceled := start(md.MockImageModelID, project.MediaTarget{AssetID: "asset"})
	if err = restarted.Service.Drive(ctx, canceled.ID); err != nil {
		t.Fatal(err)
	}
	code, body, _ = request("GET", base+"/"+canceled.ID, workspace, nil)
	if code != 200 || json.Unmarshal(body, &canceled) != nil {
		t.Fatal("cancel read")
	}
	code, body, _ = request("POST", base+"/"+canceled.ID+"/cancel", workspace, map[string]any{"revision": canceled.Revision})
	if code != 200 {
		t.Fatal("cancel", code, string(body))
	}
	if err = restarted.Service.Drive(ctx, canceled.ID); err != nil {
		t.Fatal(err)
	}
	finalCancel, err := runtime.Service.Store.Task(ctx, canceled.ID)
	if err != nil || finalCancel.Status != td.Cancelled || finalCancel.State.Result != nil || finalCancel.Remote() == nil {
		t.Fatal("canceled output revived or submit evidence lost", err)
	}
	// JSON decimals are rejected without advancing the connection CAS.
	rawInput, _ := json.Marshal(input)
	var malformed map[string]any
	json.Unmarshal(rawInput, &malformed)
	malformed["connection"].(map[string]any)["models"].([]any)[0].(map[string]any)["mockBehavior"] = map[string]any{"delaySeconds": 0.5, "failurePercent": 100}
	code, _, _ = request("POST", "/api/model-config", op, malformed)
	if code != 400 {
		t.Fatal("fractional seconds accepted", code)
	}
	unchanged, err := configuration.Snapshot(ctx)
	if err != nil || unchanged.Revision != input.Revision {
		t.Fatal("invalid input changed CAS", err)
	}
	// Entire old management protocol is gone; retained task rows are not deleted.
	for _, path := range []string{"/api/model-config/mock-generations", "/api/model-config/mock-tasks", "/api/model-config/mock-tasks/old/outputs/old/file"} {
		for _, method := range []string{"GET", "POST"} {
			code, _, _ := request(method, path, op, map[string]any{})
			if code != 404 && code != 405 {
				t.Fatal("retired route remains", method, path, code)
			}
		}
	}
	last, err := runtime.Service.Store.Task(ctx, first.ID)
	if err != nil || !bytes.Equal(last.State.Binding.ProviderConfig, saved.State.Binding.ProviderConfig) {
		t.Fatal("frozen evidence changed", err)
	}
	// Positive Mock pricing uses the same ledger as language generation. All
	// credits and deliberate interruptions below stay in the guarded test DB.
	for category, amount := range map[string]int64{credits.Gift: 30, credits.Recharge: 970} {
		if _, e := store.Add(ctx, credits.Change{AccountID: owner.ID, Amount: amount, Category: category, OperationID: identity.New("fixture-add"), Source: "isolated-fixture", Subject: owner.ID, Reason: "media-credit-integration"}); e != nil {
			t.Fatal(e)
		}
	}
	initialBalance, e := store.Balance(ctx, owner.ID)
	if e != nil {
		t.Fatal(e)
	}
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 5}
	input.Connection.Models[1].CreditPrice = &credits.PriceRule{UnitPrice: 10}
	save(md.MockBehavior{}, md.MockBehavior{})
	quote := func(pid, token, model string, target project.MediaTarget) project.ChargeQuote {
		t.Helper()
		code, body, _ := request("POST", "/api/projects/"+pid+"/quote", token, project.QuoteInput{Revision: 1, Operation: "media", ModelID: model, Target: &target})
		var q project.ChargeQuote
		if code != 200 || json.Unmarshal(body, &q) != nil {
			t.Fatal("media quote", code, string(body))
		}
		return q
	}
	imageTarget := project.MediaTarget{AssetID: "asset"}
	videoTarget := project.MediaTarget{EpisodeID: "episode", ShotID: "shot"}
	get := func(id string) project.MediaView {
		t.Helper()
		code, body, _ := request("GET", base+"/"+id, workspace, nil)
		var v project.MediaView
		if code != 200 || json.Unmarshal(body, &v) != nil {
			t.Fatal("credit GET", code, string(body))
		}
		return v
	}
	var countBefore int
	if e = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.tasks WHERE owner_id=$1`, owner.ID).Scan(&countBefore); e != nil {
		t.Fatal(e)
	}
	q := quote(p.ID, workspace, md.MockImageModelID, imageTarget)
	for i := 0; i < 3; i++ {
		again := quote(p.ID, workspace, md.MockImageModelID, imageTarget)
		if again.Basis != q.Basis || again.Unit != "image" || again.Quantity != 1 || again.Amount != 5 {
			t.Fatal("unstable image quote", again)
		}
	}
	var countAfter int
	if e = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.tasks WHERE owner_id=$1`, owner.ID).Scan(&countAfter); e != nil || countAfter != countBefore {
		t.Fatal("quote created task", e)
	}
	balance, _ := store.Balance(ctx, owner.ID)
	if balance != initialBalance {
		t.Fatal("quote charged")
	}
	for token, want := range map[string]int{"": 401, foreign: 404} {
		code, _, _ := request("POST", "/api/projects/"+p.ID+"/quote", token, project.QuoteInput{Revision: 1, Operation: "media", ModelID: md.MockImageModelID, Target: &imageTarget})
		if code != want {
			t.Fatal("quote ownership", code, want)
		}
	}
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 6}
	save(md.MockBehavior{}, md.MockBehavior{})
	code, body, _ = request("POST", base, workspace, project.MediaCommand{CommandID: identity.New("stale-basis"), Revision: 1, ModelID: md.MockImageModelID, Target: imageTarget, BillingBasis: q.Basis})
	if code != 409 || !strings.Contains(string(body), "BILLING_BASIS_CHANGED") {
		t.Fatal("price change silently accepted", code, string(body))
	}
	code, body, _ = request("POST", base, workspace, project.MediaCommand{CommandID: identity.New("missing-basis"), Revision: 1, ModelID: md.MockImageModelID, Target: imageTarget})
	if code != 409 || !strings.Contains(string(body), "BILLING_BASIS_CHANGED") {
		t.Fatal("new start bypassed basis", code, string(body))
	}
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 5}
	save(md.MockBehavior{}, md.MockBehavior{})
	paidCommand, paidImage := start(md.MockImageModelID, imageTarget)
	if paidImage.Credit == nil || paidImage.Credit.Basis.Amount != 5 || paidImage.Credit.Prepaid != 0 || paidImage.Credit.Charged != 0 {
		t.Fatal("202 invented debit", paidImage.Credit)
	}
	for i := 0; i < 8; i++ {
		starts.Add(1)
		go func() {
			defer starts.Done()
			code, body, _ := request("POST", base, workspace, paidCommand)
			var v project.MediaView
			if code != 202 || json.Unmarshal(body, &v) != nil || v.ID != paidImage.ID {
				t.Errorf("paid concurrent command replay %d", code)
			}
		}()
	}
	starts.Wait()
	// The server accepts once, then both prices and process instances change.
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 9}
	save(md.MockBehavior{}, md.MockBehavior{})
	faults := &mediaCreditFaults{CreditLedger: store}
	faults.deductUnknown.Store(true)
	restarted.Service.Permit = &project.MediaCreditPermit{Ledger: faults, Tasks: restarted.Service.Store}
	if e = restarted.Service.Drive(ctx, paidImage.ID); e == nil {
		t.Fatal("debit unknown not surfaced")
	}
	unknownTask, e := restarted.Service.Store.Task(ctx, paidImage.ID)
	if e != nil || len(unknownTask.State.Submissions) != 0 || unknownTask.Status.Terminal() {
		t.Fatal("unconfirmed debit emitted or failed", e)
	}
	confirmed := get(paidImage.ID)
	if confirmed.Credit.Prepaid != 5 || confirmed.Credit.Charged != 0 || confirmed.Credit.Settlement != "pending" {
		t.Fatal("unconfirmed ack projection", confirmed.Credit)
	}
	time.Sleep(25 * time.Millisecond)
	finish(paidImage, td.Succeeded)
	if faults.deductCalls.Load() != 1 {
		t.Fatal("recovery repeated original debit", faults.deductCalls.Load())
	}
	paidImage = get(paidImage.ID)
	if paidImage.Credit.Charged != 5 || paidImage.Credit.Basis.Rule.UnitPrice != 5 || paidImage.Credit.Settlement != "settled" {
		t.Fatal("frozen successful price", paidImage.Credit)
	}
	code, body, _ = request("POST", base, workspace, paidCommand)
	if code != 202 || !strings.Contains(string(body), paidImage.ID) {
		t.Fatal("paid command replay", code, string(body))
	}
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 5}
	save(md.MockBehavior{}, md.MockBehavior{})
	retries := &mediaSafeRetries{TaskProvider: restarted.Service.Provider}
	restarted.Service.Provider = retries
	_, retriedImage := start(md.MockImageModelID, imageTarget)
	finish(retriedImage, td.Succeeded)
	restarted.Service.Provider = retries.TaskProvider
	retriedImage = get(retriedImage.ID)
	retryTask, e := restarted.Service.Store.Task(ctx, retriedImage.ID)
	if e != nil || len(retryTask.State.Submissions) != 3 || retries.outbound.Load() != 1 || retriedImage.Credit.Prepaid != 5 || retriedImage.Credit.Charged != 5 {
		t.Fatal("safe retries recharged or emitted twice", e)
	}
	qv := quote(p.ID, workspace, md.MockVideoModelID, videoTarget)
	if qv.Unit != "second" || qv.Quantity != 10 || qv.Amount != 100 {
		t.Fatal("video billing quantity", qv)
	}
	_, paidVideo := start(md.MockVideoModelID, videoTarget)
	finish(paidVideo, td.Succeeded)
	paidVideo = get(paidVideo.ID)
	if paidVideo.Credit.Charged != 100 || paidVideo.Result.Files[0].DurationSeconds == nil || *paidVideo.Result.Files[0].DurationSeconds != 10 {
		t.Fatal("duration/charge conflated", paidVideo.Credit)
	}
	// Known failure refunds through the original transaction; recovery is safe
	// across concurrent nodes and does not generate additional submissions.
	save(md.MockBehavior{}, md.MockBehavior{DelaySeconds: 1, FailurePercent: 100})
	_, paidFailure := start(md.MockVideoModelID, videoTarget)
	finish(paidFailure, td.Failed)
	failedBefore := get(paidFailure.ID)
	if failedBefore.Credit.Settlement != "refund-pending" || failedBefore.Credit.Refunded != 0 {
		t.Fatal("unsubmitted refund shown arrived")
	}
	var recoveries sync.WaitGroup
	for i := 0; i < 3; i++ {
		recoveries.Add(1)
		go func() {
			defer recoveries.Done()
			if e := service.RecoverMediaCredits(ctx); e != nil {
				t.Error(e)
			}
		}()
	}
	recoveries.Wait()
	failedAfter := get(paidFailure.ID)
	if failedAfter.Credit.Settlement != "refunded" || failedAfter.Credit.Refunded != 100 || failedAfter.Credit.Charged != 0 {
		t.Fatal("full failure refund", failedAfter.Credit)
	}
	save(md.MockBehavior{DelaySeconds: 60}, md.MockBehavior{})
	_, paidCancel := start(md.MockImageModelID, imageTarget)
	if e = restarted.Service.Drive(ctx, paidCancel.ID); e != nil {
		t.Fatal(e)
	}
	paidCancel = get(paidCancel.ID)
	code, body, _ = request("POST", base+"/"+paidCancel.ID+"/cancel", workspace, map[string]any{"revision": paidCancel.Revision})
	if code != 200 {
		t.Fatal("paid cancel", code, string(body))
	}
	service.Credits = faults
	faults.refundBefore.Store(true)
	if e = service.RecoverMediaCredits(ctx); e == nil {
		t.Fatal("refund failure swallowed")
	}
	if v := get(paidCancel.ID); v.Credit.Settlement != "refund-pending" || v.Credit.Refunded != 0 {
		t.Fatal("failed refund claimed success", v.Credit)
	}
	faults.refundUnknown.Store(true)
	if e = service.RecoverMediaCredits(ctx); e == nil {
		t.Fatal("refund commit unknown swallowed")
	}
	if e = service.RecoverMediaCredits(ctx); e != nil {
		t.Fatal(e)
	}
	if v := get(paidCancel.ID); v.Credit.Settlement != "refunded" || v.Credit.Refunded != 5 || v.Result != nil {
		t.Fatal("refund reconcile", v.Credit)
	}
	// Cancellation can commit while the actual debit transaction is in flight.
	barrier := &mediaCreditFaults{CreditLedger: store, entered: make(chan struct{}), proceed: make(chan struct{})}
	restarted.Service.Permit = &project.MediaCreditPermit{Ledger: barrier, Tasks: restarted.Service.Store}
	_, raceCancel := start(md.MockImageModelID, imageTarget)
	driven := make(chan error, 1)
	go func() { driven <- restarted.Service.Drive(ctx, raceCancel.ID) }()
	select {
	case <-barrier.entered:
	case <-ctx.Done():
		t.Fatal("debit race timeout")
	}
	raceCancel = get(raceCancel.ID)
	code, body, _ = request("POST", base+"/"+raceCancel.ID+"/cancel", workspace, map[string]any{"revision": raceCancel.Revision})
	if code != 200 {
		t.Fatal("concurrent cancel", code, string(body))
	}
	close(barrier.proceed)
	if e = <-driven; !errors.Is(e, fault.ErrConflict) {
		t.Fatal("cancel outbound CAS", e)
	}
	if e = service.RecoverMediaCredits(ctx); e != nil {
		t.Fatal(e)
	}
	raceTask, e := restarted.Service.Store.Task(ctx, raceCancel.ID)
	if e != nil || len(raceTask.State.Submissions) != 0 || raceTask.Status != td.Cancelled {
		t.Fatal("cancel race emitted", e)
	}
	if v := get(raceCancel.ID); v.Credit.Refunded != 5 {
		t.Fatal("cancel race lost debit", v.Credit)
	}
	restarted.Service.Permit = &project.MediaCreditPermit{Ledger: store, Tasks: restarted.Service.Store}
	// Another real account with no available credits cannot emit. A promotion
	// then permits the same local model with zero entries, retaining original price.
	otherProject := d.Clone(p)
	otherProject.ID = identity.New("zero_balance_media")
	otherProject.OwnerID = other.ID
	if e = store.Create(ctx, otherProject); e != nil {
		t.Fatal(e)
	}
	otherBase := "/api/projects/" + otherProject.ID + "/media-tasks"
	otherBalance, _ := store.Balance(ctx, other.ID)
	if otherBalance != 0 {
		t.Fatal("isolated zero balance fixture changed", otherBalance)
	}
	startOther := func() project.MediaView {
		t.Helper()
		q := quote(otherProject.ID, foreign, md.MockImageModelID, imageTarget)
		code, body, _ := request("POST", otherBase, foreign, project.MediaCommand{CommandID: identity.New("other-media"), Revision: 1, ModelID: md.MockImageModelID, Target: imageTarget, BillingBasis: q.Basis})
		var v project.MediaView
		if code != 202 || json.Unmarshal(body, &v) != nil {
			t.Fatal("other start", code, string(body))
		}
		return v
	}
	save(md.MockBehavior{}, md.MockBehavior{})
	insufficient := startOther()
	if e = restarted.Service.Drive(ctx, insufficient.ID); e != nil {
		t.Fatal(e)
	}
	denied, e := restarted.Service.Store.Task(ctx, insufficient.ID)
	if e != nil || denied.Status != td.Failed || denied.State.ErrorCode != "CREDIT_INSUFFICIENT" || len(denied.State.Submissions) != 0 {
		t.Fatal("insufficient emitted", e)
	}
	begin, end := time.Now().UTC().Add(-time.Minute), time.Now().UTC().Add(time.Minute)
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 5, FreeEnabled: true, FreeStart: &begin, FreeEnd: &end}
	save(md.MockBehavior{}, md.MockBehavior{})
	promoted := startOther()
	if promoted.Credit == nil || !promoted.Credit.Basis.Free || promoted.Credit.Basis.OriginalAmount != 5 || promoted.Credit.Basis.Amount != 0 {
		t.Fatal("promotion basis", promoted.Credit)
	}
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 7}
	save(md.MockBehavior{}, md.MockBehavior{})
	for i := 0; i < 10; i++ {
		if e = restarted.Service.Drive(ctx, promoted.ID); e != nil {
			t.Fatal(e)
		}
		saved, e := restarted.Service.Store.Task(ctx, promoted.ID)
		if e != nil {
			t.Fatal(e)
		}
		if saved.Status.Terminal() {
			if saved.Status != td.Succeeded {
				t.Fatal("promotion failed")
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	_, otherEntries, e := store.CreditEntries(ctx, other.ID, 1, 100)
	if e != nil || otherEntries != 0 {
		t.Fatal("zero promotion created ledger", otherEntries, e)
	}
	// Authoritative views and balances agree, without leaking private input.
	balance, e = store.Balance(ctx, owner.ID)
	if e != nil || balance != initialBalance-110 {
		t.Fatal("ledger balance mismatch", balance, initialBalance, e)
	}
	usage, e := store.ModelUsage(ctx, modelusage.Filter{View: "tasks", UserID: owner.ID, TaskID: paidImage.ID, Page: 1, PageSize: 20, Sort: "asc"})
	if e != nil || usage.Total != 1 || usage.Items[0].Prepaid != 5 || usage.Items[0].Charged != 5 || usage.Items[0].DeductionID == nil {
		t.Fatal("usage not using media ledger", usage, e)
	}
	var basis credits.Charge
	if json.Unmarshal(usage.Items[0].Basis, &basis) != nil || basis.ID != paidImage.Credit.Basis.ID {
		t.Fatal("usage basis lost")
	}
	encoded, _ := json.Marshal(usage)
	for _, private := range []string{"铜制钥匙", "callerContext", "providerConfig", "credential", "objectKey"} {
		if bytes.Contains(encoded, []byte(private)) {
			t.Fatal("usage leaked private input", private)
		}
	}
	history, e := store.CreditHistory(ctx, owner.ID, credits.HistoryFilter{Category: "all", Direction: "consumed", Page: 1, PageSize: 50})
	if e != nil || history.Summary.SettledConsumed != 110 || history.Summary.Pending != 0 || len(history.Analysis) != 1 || history.Analysis[0].Source != "media-generation" || history.Analysis[0].Amount != 110 {
		t.Fatal("media consumption projection", history, e)
	}

	// A resolution price is part of the immutable quote, request and PG task.
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 5, ResolutionPrices: map[string]int64{"2K": 9, "4K": 17}}
	save(md.MockBehavior{}, md.MockBehavior{})
	selected := md.MediaOptions{Resolution: "2K"}
	status, raw, _ = request("POST", "/api/projects/"+p.ID+"/quote", workspace, project.QuoteInput{Revision: p.Revision, Operation: "media", ModelID: md.MockImageModelID, Target: &imageTarget, Options: &selected})
	var resolutionQuote project.ChargeQuote
	if status != 200 || json.Unmarshal(raw, &resolutionQuote) != nil || resolutionQuote.Amount != 9 || resolutionQuote.Rule.UnitPrice != 9 || len(resolutionQuote.Rule.ResolutionPrices) != 0 {
		t.Fatal("resolution quote", status, string(raw))
	}
	mismatched := project.MediaCommand{CommandID: identity.New("wrong-spec"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: imageTarget, Options: &md.MediaOptions{Resolution: "1K"}, BillingBasis: resolutionQuote.Basis}
	status, raw, _ = request("POST", base, workspace, mismatched)
	if status != 409 || !bytes.Contains(raw, []byte("BILLING_BASIS_CHANGED")) {
		t.Fatal("resolution not bound", status, string(raw))
	}
	command2K := project.MediaCommand{CommandID: identity.New("image-2K"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: imageTarget, Options: &selected, BillingBasis: resolutionQuote.Basis}
	status, raw, _ = request("POST", base, workspace, command2K)
	var image2K project.MediaView
	if status != 202 || json.Unmarshal(raw, &image2K) != nil || image2K.Options != selected {
		t.Fatal("2K start", status, string(raw))
	}
	input.Connection.Models[0].CreditPrice.ResolutionPrices["2K"] = 12
	save(md.MockBehavior{}, md.MockBehavior{})
	finish(image2K, td.Succeeded)
	image2K = get(image2K.ID)
	if image2K.Credit.Charged != 9 || image2K.Credit.Basis.Rule.UnitPrice != 9 || image2K.Result.Files[0].Width != paidImage.Result.Files[0].Width*2 || image2K.Result.Files[0].Height != paidImage.Result.Files[0].Height*2 {
		t.Fatal("2K dimensions/frozen price", image2K)
	}
	replayStatus, replayRaw, _ := request("POST", base, workspace, command2K)
	var replay2K project.MediaView
	if replayStatus != 202 || json.Unmarshal(replayRaw, &replay2K) != nil || replay2K.ID != image2K.ID || replay2K.Credit.Charged != 9 {
		t.Fatal("2K idempotency", replayStatus, string(replayRaw))
	}
	balance, e = store.Balance(ctx, owner.ID)
	if e != nil || balance != initialBalance-119 {
		t.Fatal("resolution ledger", balance, e)
	}
	t.Log("resolution quote 2K=9, actual 2x JPEG dimensions, accepted price survives change to 12, basis mismatch refused, replay creates no second debit")
	t.Log("isolated positive image=5/video=10*10; frozen price, debit/refund commit-unknown, zero-balance promotion, insufficient outbound gate, cancellation race, concurrent recovery and PG/HTTP/usage/ledger agreement verified")

	// All public selectable combinations quote against the operator's per-tier
	// rates. These are isolated prices; no production model or balance is touched.
	input.Connection.Models[1].CreditPrice = &credits.PriceRule{UnitPrice: 10, ResolutionPrices: map[string]int64{"480p": 3, "720p": 6}}
	save(md.MockBehavior{}, md.MockBehavior{})
	bases := map[string]bool{}
	quoteOptions := func(options md.MediaOptions) project.ChargeQuote {
		t.Helper()
		code, raw, _ := request("POST", "/api/projects/"+p.ID+"/quote", workspace, project.QuoteInput{Revision: p.Revision, Operation: "media", ModelID: md.MockVideoModelID, Target: &videoTarget, Options: &options})
		var quoted project.ChargeQuote
		if code != 200 || json.Unmarshal(raw, &quoted) != nil {
			t.Fatal("selected video quote", code, string(raw))
		}
		return quoted
	}
	for resolution, rate := range map[string]int64{"480p": 3, "720p": 6, "1080p": 10} {
		for seconds := 1; seconds <= 10; seconds++ {
			options := md.MediaOptions{Resolution: resolution, DurationSeconds: float64(seconds)}
			q := quoteOptions(options)
			if q.Quantity != int64(seconds) || q.Amount != int64(seconds)*rate || q.Rule.UnitPrice != rate || q.MediaOptions == nil || *q.MediaOptions != options || bases[q.Basis] {
				t.Fatal("wrong selected quote", q)
			}
			bases[q.Basis] = true
		}
	}
	consumed := int64(119)
	for _, test := range []struct {
		resolution             string
		seconds, width, height int
		amount                 int64
	}{{"480p", 1, 854, 480, 3}, {"720p", 5, 1280, 720, 30}, {"1080p", 10, 1920, 1080, 100}} {
		options := md.MediaOptions{Resolution: test.resolution, DurationSeconds: float64(test.seconds)}
		q := quoteOptions(options)
		command := project.MediaCommand{CommandID: identity.New("video-options"), Revision: p.Revision, ModelID: md.MockVideoModelID, Target: videoTarget, Options: &options, BillingBasis: q.Basis}
		status, raw, _ := request("POST", base, workspace, command)
		var view project.MediaView
		if status != 202 || json.Unmarshal(raw, &view) != nil || view.Options != options {
			t.Fatal("selected start", status, string(raw))
		}
		if test.resolution == "480p" {
			input.Connection.Models[1].CreditPrice.ResolutionPrices["480p"] = 8
			save(md.MockBehavior{}, md.MockBehavior{})
		}
		// Restart between acceptance and execution; selected output and price
		// remain the immutable task facts, including after a model price change.
		next, nextQueue, err := taskruntime.New(cfg, store, objects, configuration, redisruntime.ConnectDatabase(t, cfg.Database.URL), files)
		if err != nil {
			t.Fatal(err)
		}
		defer nextQueue.Close()
		next.Service.Permit = &project.MediaCreditPermit{Ledger: store, Tasks: next.Service.Store}
		restarted = next
		finish(view, td.Succeeded)
		view = get(view.ID)
		f := view.Result.Files[0]
		if view.Options != options || view.Credit.Charged != test.amount || view.Credit.Basis.Quantity != int64(test.seconds) || f.Width != test.width || f.Height != test.height || f.DurationSeconds == nil || *f.DurationSeconds != float64(test.seconds) {
			t.Fatal("selected PG/HTTP output or frozen price mismatch", view)
		}
		inputSnapshot, err := next.Service.Input(ctx, "project:"+p.ID, view.ID)
		var frozenOptions md.MediaOptions
		if err != nil || json.Unmarshal(inputSnapshot.Input, &frozenOptions) != nil || frozenOptions != options {
			t.Fatal("transport input lost selection", err)
		}
		req, err := http.NewRequestWithContext(ctx, "GET", server.URL+f.ContentURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		addSessionCookie(req, workspace)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		media, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || int64(len(media)) != f.SizeBytes {
			t.Fatal("full private MP4", err)
		}
		file := filepath.Join(t.TempDir(), "saved.mp4")
		if err = os.WriteFile(file, media, 0600); err != nil {
			t.Fatal(err)
		}
		probe, err := exec.CommandContext(ctx, cfg.Media.FFprobe, "-v", "error", "-show_entries", "stream=width,height:format=duration", "-of", "json", file).Output()
		if err != nil {
			t.Fatal(err)
		}
		var actual struct {
			Streams []struct{ Width, Height int }
			Format  struct{ Duration string }
		}
		if json.Unmarshal(probe, &actual) != nil || len(actual.Streams) != 1 || actual.Streams[0].Width != test.width || actual.Streams[0].Height != test.height || actual.Format.Duration != strconv.Itoa(test.seconds)+".000000" {
			t.Fatal("private HTTP bytes do not match selected output", string(probe))
		}
		t.Logf("persisted %s %ds charge=%d actual=%s", test.resolution, test.seconds, view.Credit.Charged, probe)
		status, raw, _ = request("POST", base, workspace, command)
		var replay project.MediaView
		if status != 202 || json.Unmarshal(raw, &replay) != nil || replay.ID != view.ID || replay.Credit.Charged != test.amount {
			t.Fatal("selected command replay", status, string(raw))
		}
		changed := command
		changed.Options = &md.MediaOptions{Resolution: test.resolution, DurationSeconds: float64(test.seconds%10 + 1)}
		status, raw, _ = request("POST", base, workspace, changed)
		if status != 409 || !bytes.Contains(raw, []byte("COMMAND_ID_CONFLICT")) {
			t.Fatal("changed original options accepted", status, string(raw))
		}
		consumed += test.amount
	}
	balance, e = store.Balance(ctx, owner.ID)
	if e != nil || balance != initialBalance-consumed {
		t.Fatal("selected video ledger balance", balance, consumed, e)
	}
	for _, options := range []md.MediaOptions{{Resolution: "1080p", DurationSeconds: 10.793333}, {Resolution: "480p", DurationSeconds: 0}, {Resolution: "720p", DurationSeconds: 1.5}, {Resolution: "1080p", DurationSeconds: 11}, {Resolution: "4K", DurationSeconds: 5}} {
		code, raw, _ := request("POST", "/api/projects/"+p.ID+"/quote", workspace, project.QuoteInput{Revision: p.Revision, Operation: "media", ModelID: md.MockVideoModelID, Target: &videoTarget, Options: &options})
		if code != 400 || !bytes.Contains(raw, []byte("MEDIA_OPTIONS_INVALID")) {
			t.Fatal("unsupported quote accepted", options, code, string(raw))
		}
	}

	// Per-tier availability shares the same connection CAS and the original
	// accepted task survives a later switch change, including omitted options.
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 20, ResolutionPrices: map[string]int64{"2K": 12, "4K": 3}}
	input.Connection.Models[0].ResolutionEnabled = map[string]bool{"1K": true, "2K": true, "4K": true}
	save(md.MockBehavior{DelaySeconds: 60}, md.MockBehavior{})
	tierCommand, tierTask := start(md.MockImageModelID, imageTarget)
	if tierTask.Options.Resolution != "4K" || tierTask.Credit.Basis.Rule.UnitPrice != 3 {
		t.Fatal("highest enabled default not frozen", tierTask)
	}
	input.Connection.Models[0].ResolutionEnabled["4K"] = false
	input.Connection.Models[1].ResolutionEnabled = map[string]bool{"480p": true, "720p": true, "1080p": false}
	save(md.MockBehavior{}, md.MockBehavior{})
	for _, blocked := range []struct {
		model   string
		target  project.MediaTarget
		options md.MediaOptions
	}{{md.MockImageModelID, imageTarget, md.MediaOptions{Resolution: "4K"}}, {md.MockVideoModelID, videoTarget, md.MediaOptions{Resolution: "1080p", DurationSeconds: 5}}} {
		code, raw, _ := request("POST", "/api/projects/"+p.ID+"/quote", workspace, project.QuoteInput{Revision: p.Revision, Operation: "media", ModelID: blocked.model, Target: &blocked.target, Options: &blocked.options})
		if code != 400 || !bytes.Contains(raw, []byte("MEDIA_OPTIONS_INVALID")) {
			t.Fatal("disabled tier quoted", code, string(raw))
		}
		command := project.MediaCommand{CommandID: identity.New("closed-tier"), Revision: p.Revision, ModelID: blocked.model, Target: blocked.target, Options: &blocked.options, BillingBasis: tierCommand.BillingBasis}
		code, raw, _ = request("POST", base, workspace, command)
		if code != 400 || !bytes.Contains(raw, []byte("MEDIA_OPTIONS_INVALID")) {
			t.Fatal("disabled tier submitted", code, string(raw))
		}
	}
	status, raw, _ = request("POST", base, workspace, tierCommand)
	var tierReplay project.MediaView
	if status != 202 || json.Unmarshal(raw, &tierReplay) != nil || tierReplay.ID != tierTask.ID || tierReplay.Options.Resolution != "4K" || tierReplay.Credit.Basis.Rule.UnitPrice != 3 {
		t.Fatal("switch rewrote accepted command", status, string(raw))
	}
	status, raw, _ = request("POST", base+"/"+tierTask.ID+"/cancel", workspace, map[string]any{"revision": tierTask.Revision})
	if status != 200 {
		t.Fatal("cancel highest default", status, string(raw))
	}
	// An omitted options object must produce the same 2K in quote, frozen task,
	// private result and history. It must not fall back to supplier default 1K.
	_, defaultImage := start(md.MockImageModelID, imageTarget)
	finish(defaultImage, td.Succeeded)
	defaultImage = get(defaultImage.ID)
	if defaultImage.Options.Resolution != "2K" || defaultImage.Credit.Charged != 12 || defaultImage.Result.Files[0].Width != paidImage.Result.Files[0].Width*2 || defaultImage.Result.Files[0].Height != paidImage.Result.Files[0].Height*2 {
		t.Fatal("default quote/transport/result disagree", defaultImage)
	}
	input.Connection.Models[0].ResolutionEnabled = map[string]bool{}
	input.Connection.Models[1].ResolutionEnabled = map[string]bool{}
	save(md.MockBehavior{}, md.MockBehavior{})
	code, raw, _ = request("GET", "/api/models", workspace, nil)
	var candidates ports.ModelOptions
	if code != 200 || json.Unmarshal(raw, &candidates) != nil || len(candidates.Image) != 0 || len(candidates.Video) != 0 {
		t.Fatal("all-off candidates visible", code, string(raw))
	}
	// Strict JSON and unknown capability IDs reject the whole write.
	for _, resolutionEnabled := range []any{map[string]any{"4K": 1}, map[string]bool{"8K": true}} {
		payload, _ := json.Marshal(input)
		var invalid map[string]any
		json.Unmarshal(payload, &invalid)
		invalid["connection"].(map[string]any)["models"].([]any)[0].(map[string]any)["resolutionEnabled"] = resolutionEnabled
		code, raw, _ = request("POST", "/api/model-config", op, invalid)
		after, e := store.ModelConfigSnapshot(ctx)
		if code != 400 || e != nil || after.Revision != input.Revision {
			t.Fatal("invalid tier write was not atomic", code, string(raw), e)
		}
	}
	t.Log("tier CAS persisted image/video independently; 4K=3 wins over 2K=12, disabled quote/start rejected, original 4K command replayed then cancelled, default 2K JPEG persisted and charged 12, all-off candidates hidden, invalid writes atomic")
	t.Log("all 30 video options quoted; selected 480p/1s,720p/5s,1080p/10s persisted through restart and private HTTP; frozen per-tier charges/replays and invalid options verified")
	t.Logf("real PG/HTTP private image/video: delay=1s and probability=0/50/100; restart/idempotent draw=%d; project=%s", draw.reads.Load(), p.ID)
}
