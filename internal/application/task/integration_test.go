package task_test

import (
	"context"
	"drama-agent/internal/testsupport/pgfixture"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"drama-agent/internal/adapter/mockmedia"
	mq "drama-agent/internal/adapter/nsq"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/task"
	testconfig "drama-agent/internal/testsupport/config"
)

type resolver struct{ paid bool }

func (r resolver) ResolveTask(_ context.Context, id, operation string) (d.Binding, error) {
	actual := "mock-image"
	if operation == "video.generate" {
		actual = "mock_video"
	}
	return d.Binding{ConnectionID: "mock", Protocol: md.MockProtocol, ModelID: id, RequestModelID: actual, Free: !r.paid}, nil
}

type noQueue struct {
	fail bool
	ids  []string
}

func (q *noQueue) PublishTasks(_ context.Context, ids []string) error {
	if q.fail {
		return errors.New("injected publication outage")
	}
	q.ids = append(q.ids, ids...)
	return nil
}
func (q *noQueue) ConsumeTasks(context.Context, func(context.Context, string) error) error {
	return nil
}

var taskSchema sync.Once
var taskSchemaError error

func fixture(t *testing.T) (*app.Service, *postgres.Store) {
	t.Helper()
	url := os.Getenv("DRAMA_TASK_TEST_PG")
	if url == "" {
		t.Skip("set DRAMA_TASK_TEST_PG to an isolated drama_task_test database")
	}
	if !strings.Contains(url, "/drama_task_test?") {
		t.Fatal("task integration tests require isolated drama_task_test database")
	}
	ctx := context.Background()
	store, err := pgfixture.Open(t, ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if os.Getenv("DRAMA_TASK_WORKER_CHILD") != "1" {
		taskSchema.Do(func() { taskSchemaError = store.Migrate(ctx) })
		if taskSchemaError != nil {
			t.Fatal(taskSchemaError)
		}
	}
	endpoint := os.Getenv("DRAMA_TASK_TEST_S3")
	if endpoint == "" {
		t.Fatal("DRAMA_TASK_TEST_S3 required")
	}
	storage := testconfig.Example().Storage
	storage.Endpoint, storage.Bucket, storage.AccessKey, storage.SecretKey = endpoint, "task-tests", "task_test", "local_task_test"
	objects, err := s3objects.New(storage)
	if err != nil {
		t.Fatal(err)
	}
	if err = objects.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	return &app.Service{Capacity: redisruntime.ConnectDatabase(t, url), Store: postgres.NewTaskStore(store), Resolver: resolver{}, Provider: mockmedia.TaskProvider{}, Results: &s3objects.TaskResults{Objects: objects, ProbePath: testconfig.Example().Media.FFprobe, ProbeTimeout: testconfig.Example().Media.ProbeTimeout}, Queue: &noQueue{}, SubmitConcurrency: 8, QueryConcurrency: 16, SaveConcurrency: 4, ObserveTimeout: 2 * time.Minute, SaveTimeout: 5 * time.Minute, EvidenceTimeout: 10 * time.Second, CapacityRetry: 100 * time.Millisecond, Lease: 300 * time.Millisecond, Policy: d.Policy{PollMillis: 20, RetryMillis: 20, MaxSubmissions: 3}}, store
}
func command(scope, kind string, delay int) app.Command {
	ratio := "1:1"
	if kind == "video" {
		ratio = ""
	}
	body, _ := json.Marshal(mockmedia.TaskInput{Prompt: "完整测试提示词", AspectRatio: ratio, DelayMS: delay})
	return app.Command{ScopeID: scope, CommandID: identity.New("command"), Operation: kind + ".generate", ModelID: "fixture_" + kind, Input: body, CallerContext: json.RawMessage(`{"source":1}`)}
}
func start(t *testing.T, s *app.Service, c app.Command) app.View {
	t.Helper()
	v, e := s.Submit(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func finish(t *testing.T, s *app.Service, v app.View) d.Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := s.Drive(context.Background(), v.ID); err != nil {
			t.Fatal(err)
		}
		got, err := s.Store.Task(context.Background(), v.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status.Terminal() || (got.Status == d.Unknown && got.NextRunAt == nil) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not settle", v.ID)
	return d.Task{}
}
func TestPersistentImageVideoAndCommandIsolation(t *testing.T) {
	s, _ := fixture(t)
	scope := identity.New("test")
	for _, kind := range []string{"image", "video"} {
		c := command(scope, kind, 60)
		v := start(t, s, c)
		same := start(t, s, c)
		if same.ID != v.ID {
			t.Fatal("same command duplicated")
		}
		changed := c
		changed.Input = json.RawMessage(`{"prompt":"changed","aspectRatio":"1:1","delayMs":0,"forceFail":false}`)
		if _, e := s.Submit(context.Background(), changed); e == nil {
			t.Fatal("same command changed intent accepted")
		}
		got := finish(t, s, v)
		if got.Status != d.Succeeded || len(got.State.Submissions) != 1 || len(got.State.Result.Files) != 1 {
			t.Fatalf("invalid task: %+v", got)
		}
		raw, meta, e := s.File(context.Background(), scope, v.ID, got.State.Result.Files[0].ID)
		if e != nil || int64(len(raw)) != meta.SizeBytes {
			t.Fatal("result not readable", e)
		}
		if kind == "video" && (meta.DurationSeconds == nil || *meta.DurationSeconds < 10 || meta.Width != 1920) {
			t.Fatal("video metadata not inspected")
		}
		if _, e = s.Get(context.Background(), "other", v.ID); !errors.Is(e, fault.ErrNotFound) {
			t.Fatal("scope leaked", e)
		}
		if _, _, e = s.File(context.Background(), "other", v.ID, meta.ID); !errors.Is(e, fault.ErrNotFound) {
			t.Fatal("file scope leaked", e)
		}
		cold, e := s.Store.TaskInput(context.Background(), v.ID)
		if e != nil || string(gotInput(t, cold.Input)) != string(gotInput(t, c.Input)) {
			t.Fatal("input changed", e)
		}
	}
}
func gotInput(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	json.Unmarshal(raw, &v)
	b, _ := json.Marshal(v)
	return b
}
func TestActiveKeyClaimCancellationAndScanSession(t *testing.T) {
	s, _ := fixture(t)
	s2, _ := fixture(t)
	ctx := context.Background()
	scope := identity.New("test")
	c := command(scope, "image", 0)
	c.ActiveKey = "opaque-target"
	v := start(t, s, c)
	c.CommandID = identity.New("command")
	if _, err := s2.Submit(ctx, c); err == nil {
		t.Fatal("active target accepted twice")
	}
	var claims atomic.Int32
	var holder d.Task
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, ok, e := s.Store.ClaimTask(ctx, v.ID, identity.New("claim"), 100*time.Millisecond)
			if e != nil {
				t.Error(e)
			}
			if ok {
				claims.Add(1)
				mu.Lock()
				holder = got
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("claims=%d", claims.Load())
	}
	time.Sleep(130 * time.Millisecond)
	newer, ok, e := s2.Store.ClaimTask(ctx, v.ID, "new-token", time.Second)
	if e != nil || !ok {
		t.Fatal("expired lease not recovered", e)
	}
	holder.Status = d.Failed
	if _, e = s.Store.CommitTask(ctx, holder, holder.Revision, holder.LeaseToken, true); !errors.Is(e, fault.ErrConflict) {
		t.Fatal("old holder committed", e)
	}
	if _, e = s.Cancel(ctx, "other", v.ID, newer.Revision); !errors.Is(e, fault.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = s.Cancel(ctx, scope, v.ID, newer.Revision-1); !errors.Is(e, fault.ErrConflict) {
		t.Fatal("stale cancel", e)
	}
	cancelled, e := s.Cancel(ctx, scope, v.ID, newer.Revision)
	if e != nil || cancelled.Status != d.Cancelled {
		t.Fatal(e)
	}
	if _, e = s.Cancel(ctx, scope, v.ID, 1); e != nil {
		t.Fatal("repeat cancel not idempotent", e)
	}
	if _, e = s2.Store.CommitTask(ctx, newer, newer.Revision, newer.LeaseToken, true); !errors.Is(e, fault.ErrConflict) {
		t.Fatal("cancel not fencing", e)
	}
	start(t, s, c)
	release, ok, e := s.Store.AcquireTaskScan(ctx)
	if e != nil || !ok {
		t.Fatal(e)
	}
	_, ok, e = s2.Store.AcquireTaskScan(ctx)
	if e != nil || ok {
		t.Fatal("two scan owners", e)
	}
	release.Release()
	release2, ok, e := s2.Store.AcquireTaskScan(ctx)
	if e != nil || !ok {
		t.Fatal("session lock not released", e)
	}
	release2.Release()
}

func TestScanCancellationTimeoutAndBoundedCursor(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	scannerCtx, cancelScanner := context.WithCancel(ctx)
	lock, ok, e := s.Store.AcquireTaskScan(scannerCtx)
	if e != nil || !ok {
		t.Fatal(e)
	}
	cancelScanner()
	if e = lock.Check(ctx); e == nil {
		t.Fatal("disconnected scanner kept publishing authority")
	}
	lock.Release()
	lock, ok, e = s.Store.AcquireTaskScan(ctx)
	if e != nil || !ok {
		t.Fatal("scan session not recoverable", e)
	}
	lock.Release()
	timeout, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, e = s.Store.AcquireTaskScan(timeout); e == nil {
		t.Fatal("cancelled scan accepted")
	}
	q := &noQueue{}
	s.Queue = q
	ids := map[string]bool{}
	for i := 0; i < 9; i++ {
		ids[start(t, s, command(identity.New("scope"), "image", 0)).ID] = true
	}
	q.ids = nil
	runtime := &app.Runtime{Service: s, Batch: 3, MaxBatches: 1, ScanTimeout: time.Second}
	for i := 0; i < 100 && len(ids) > 0; i++ {
		before := len(q.ids)
		if e = runtime.Scan(ctx); e != nil {
			t.Fatal(e)
		}
		if len(q.ids)-before > 3 {
			t.Fatal("unbounded scan batch")
		}
		for _, id := range q.ids {
			delete(ids, id)
		}
	}
	if len(ids) > 0 {
		t.Fatal("bounded cursor starved later tasks")
	}
}

type injectedProvider struct {
	ports.TaskProvider
	submissions atomic.Int32
	mode        string
	entered     chan struct{}
	release     chan struct{}
}

func (p *injectedProvider) Submit(ctx context.Context, b d.Binding, id string, raw json.RawMessage) (d.Receipt, error) {
	p.submissions.Add(1)
	if p.entered != nil {
		close(p.entered)
		<-p.release
	}
	if p.mode == "unknown" {
		return d.Receipt{}, errors.New("response lost after external send")
	}
	if p.mode == "not_sent" {
		return d.Receipt{Outcome: "not_sent", Raw: json.RawMessage(`{"sent":false}`), Billing: d.Billing{Status: "known"}}, nil
	}
	r, e := p.TaskProvider.Submit(ctx, b, id, raw)
	if p.mode == "paid" {
		r.Billing = d.Billing{Status: "known", KnownMicroCNY: 123}
		r.Raw = json.RawMessage(`{"accepted":true,"cost":123}`)
	}
	return r, e
}

type resultFailure struct {
	ports.TaskResultStore
	fail atomic.Bool
}

func (r *resultFailure) SaveTaskOutput(ctx context.Context, id, key string, m ports.MediaResult) (d.Output, error) {
	if r.fail.Load() {
		return d.Output{}, errors.New("object service unavailable")
	}
	return r.TaskResultStore.SaveTaskOutput(ctx, id, key, m)
}
func TestUnknownSafeRetryStorageRecoveryAndCancelLateEvidence(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	scope := identity.New("test")
	p := &injectedProvider{TaskProvider: mockmedia.TaskProvider{}, mode: "unknown"}
	s.Provider = p
	unknown := finish(t, s, start(t, s, command(scope, "image", 0)))
	for i := 0; i < 5; i++ {
		if e := s.Drive(ctx, unknown.ID); e != nil {
			t.Fatal(e)
		}
	}
	if unknown.Status != d.Unknown || unknown.NextRunAt != nil || p.submissions.Load() != 1 {
		t.Fatal("unknown resent")
	}
	p.mode = "not_sent"
	p.submissions.Store(0)
	failed := finish(t, s, start(t, s, command(scope, "image", 0)))
	if failed.Status != d.Failed || len(failed.State.Submissions) != 3 || p.submissions.Load() != 3 {
		t.Fatal("bounded retries not persisted")
	}
	p.mode = ""
	r := &resultFailure{TaskResultStore: s.Results}
	r.fail.Store(true)
	s.Results = r
	v := start(t, s, command(scope, "image", 0))
	for i := 0; i < 8; i++ {
		s.Drive(ctx, v.ID)
		time.Sleep(25 * time.Millisecond)
	}
	waiting, e := s.Store.Task(ctx, v.ID)
	if e != nil || waiting.Status != d.Running || waiting.State.ErrorCode != "TASK_STORAGE_RETRY" || len(waiting.State.Submissions) != 1 {
		t.Fatal("storage outage discarded original result", e)
	}
	r.fail.Store(false)
	if got := finish(t, s, v); got.Status != d.Succeeded || len(got.State.Submissions) != 1 {
		t.Fatal("storage recovery regenerated")
	}
	s2, _ := fixture(t)
	s2.Resolver = resolver{paid: true}
	late := &injectedProvider{TaskProvider: mockmedia.TaskProvider{}, mode: "paid", entered: make(chan struct{}), release: make(chan struct{})}
	s2.Provider = late
	v = start(t, s2, command(scope, "image", 0))
	done := make(chan error, 1)
	go func() { done <- s2.Drive(ctx, v.ID) }()
	<-late.entered
	current, e := s2.Store.Task(ctx, v.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s2.Cancel(ctx, scope, v.ID, current.Revision); e != nil {
		t.Fatal(e)
	}
	close(late.release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	got, e := s2.Store.Task(ctx, v.ID)
	if e != nil || got.Status != d.Cancelled || got.State.Result != nil || got.Billing().KnownMicroCNY != 123 || len(got.State.Submissions) != 1 {
		t.Fatal("late fee/evidence lost or terminal revived", e)
	}
}

func nsqQueue(t *testing.T, topic string) *mq.Queue {
	t.Helper()
	address := os.Getenv("DRAMA_TASK_TEST_NSQ")
	if address == "" {
		t.Fatal("DRAMA_TASK_TEST_NSQ required")
	}
	settings := testconfig.Example().NSQ
	settings.Address, settings.Topic, settings.Channel = address, topic, "workers"
	q, e := mq.New(settings)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(q.Close)
	return q
}
func TestNSQWorkerProcess(t *testing.T) {
	if os.Getenv("DRAMA_TASK_WORKER_CHILD") != "1" {
		t.Skip("worker subprocess")
	}
	s, _ := fixture(t)
	s.Queue = nsqQueue(t, os.Getenv("DRAMA_TASK_TOPIC"))
	s.Lease = time.Second
	if path := os.Getenv("DRAMA_TASK_STALL_DIR"); path != "" {
		s.Provider = &stallProvider{TaskProvider: s.Provider, path: path, prompt: os.Getenv("DRAMA_TASK_STALL_PROMPT")}
	}
	runtime := &app.Runtime{Service: s, ScanInterval: 100 * time.Millisecond, Batch: 100, MaxBatches: 4, ScanTimeout: 5 * time.Second}
	if e := runtime.Run(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func process(t *testing.T, topic string, extra ...string) *exec.Cmd {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestNSQWorkerProcess$", "-test.timeout=60s")
	c.Env = append(os.Environ(), "DRAMA_TASK_WORKER_CHILD=1", "DRAMA_TASK_TOPIC="+topic)
	c.Env = append(c.Env, extra...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if e := c.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if c.ProcessState == nil {
			c.Process.Kill()
			c.Wait()
		}
	})
	return c
}

// Stops only after the durable send gate and the external accepted fact, before PG receipt commit.
type stallProvider struct {
	ports.TaskProvider
	path   string
	prompt string
}

func (p *stallProvider) Submit(ctx context.Context, b d.Binding, id string, raw json.RawMessage) (d.Receipt, error) {
	r, e := p.TaskProvider.Submit(ctx, b, id, raw)
	if e != nil {
		return r, e
	}
	var input mockmedia.TaskInput
	if json.Unmarshal(raw, &input) != nil || input.Prompt != p.prompt {
		return r, e
	}
	if e = os.WriteFile(p.path+"/accepted", []byte(id), 0600); e != nil {
		return r, e
	}
	for {
		if _, e = os.Stat(p.path + "/release"); e == nil {
			return r, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestStoppedProcessLeaseTakeoverAndLateReceipt(t *testing.T) {
	s, _ := fixture(t)
	topic := identity.New("stall")
	s.Queue = nsqQueue(t, topic)
	dir := t.TempDir()
	scope := identity.New("test")
	prompt := identity.New("stall_prompt")
	a := process(t, topic, "DRAMA_TASK_STALL_DIR="+dir, "DRAMA_TASK_STALL_PROMPT="+prompt)
	c := command(scope, "image", 0)
	c.Input, _ = json.Marshal(mockmedia.TaskInput{Prompt: prompt, AspectRatio: "1:1"})
	v := start(t, s, c)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(dir + "/accepted"); e == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	accepted, e := os.ReadFile(dir + "/accepted")
	if e != nil {
		t.Fatal("A never accepted", e)
	}
	if e = a.Process.Signal(syscall.SIGSTOP); e != nil {
		t.Fatal(e)
	}
	b := process(t, topic)
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, e := s.Store.Task(context.Background(), v.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Status == d.Unknown && got.NextRunAt == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	got, e := s.Store.Task(context.Background(), v.ID)
	if e != nil || got.Status != d.Unknown || len(got.State.Submissions) != 1 {
		t.Fatal("B resent unresolved generation", e, got.Status)
	}
	if e = os.WriteFile(dir+"/release", []byte("ok"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = a.Process.Signal(syscall.SIGCONT); e != nil {
		t.Fatal(e)
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, e = s.Store.Task(context.Background(), v.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Status == d.Succeeded {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if got.Status != d.Succeeded || len(got.State.Submissions) != 1 || got.State.Submissions[0].ID != string(accepted) || got.Remote() == nil {
		t.Fatal("late original receipt not recovered", got.Status)
	}
	a.Process.Kill()
	a.Wait()
	b.Process.Kill()
	b.Wait()
}

type commitFault struct {
	ports.TaskStore
	count int
	at    int
	after bool
}

func (s *commitFault) CommitTask(ctx context.Context, t d.Task, revision uint64, token string, release bool) (d.Task, error) {
	s.count++
	if s.count == s.at && !s.after {
		return d.Task{}, errors.New("injected DB outage before commit")
	}
	got, e := s.TaskStore.CommitTask(ctx, t, revision, token, release)
	if e == nil && s.count == s.at && s.after {
		return d.Task{}, errors.New("injected lost commit response")
	}
	return got, e
}
func TestSendGateCommitAmbiguityAndReceiptWriteFailure(t *testing.T) {
	for _, at := range []int{2, 3} {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			s, _ := fixture(t)
			p := &injectedProvider{TaskProvider: s.Provider}
			s.Provider = p
			faulty := &commitFault{TaskStore: s.Store, at: at, after: at == 2}
			s.Store = faulty
			v := start(t, s, command(identity.New("test"), "image", 0))
			e := s.Drive(context.Background(), v.ID)
			if at == 2 {
				if e == nil || p.submissions.Load() != 0 {
					t.Fatal("uncertain send gate permitted external call")
				}
				time.Sleep(350 * time.Millisecond)
				got := finish(t, s, v)
				if got.Status != d.Unknown || p.submissions.Load() != 0 {
					t.Fatal("uncertain gate resent")
				}
			}
			if at == 3 {
				if e != nil {
					t.Fatal(e)
				}
				got := finish(t, s, v)
				if got.Status != d.Succeeded || p.submissions.Load() != 1 {
					t.Fatal("receipt write loss regenerated")
				}
			}
		})
	}
}

func TestBrokerRestartPGAcceptanceAndRecovery(t *testing.T) {
	if os.Getenv("DRAMA_TASK_TEST_BROKER_RESTART") != "1" {
		t.Skip("explicit isolated broker restart opt-in")
	}
	// Validate the explicit target before starting work or changing any service.
	name := os.Getenv("DRAMA_TASK_TEST_NSQ_CONTAINER")
	if name == "" {
		t.Fatal("explicit isolated NSQ container required")
	}
	label, e := exec.Command("docker", "inspect", "--format", "{{index .Config.Labels \"drama-agent.component\"}}", name).Output()
	if e != nil || strings.TrimSpace(string(label)) != "task-test-nsq" {
		t.Fatal("not this test's broker", e)
	}
	var bindings struct {
		Published  []struct{ HostIP, HostPort string }
		Configured []struct{ HostIP, HostPort string }
	}
	bound, e := exec.Command("docker", "inspect", "--format", `{"published":{{json (index .NetworkSettings.Ports "4150/tcp")}},"configured":{{json (index .HostConfig.PortBindings "4150/tcp")}}}`, name).Output()
	if e != nil {
		t.Fatal("inspect isolated broker ports", e)
	}
	if e = json.Unmarshal(bound, &bindings); e != nil {
		t.Fatal(e)
	}
	if len(bindings.Published) != 1 || len(bindings.Configured) != 1 {
		t.Fatal("one fixed loopback broker binding required")
	}
	published, configured := bindings.Published[0], bindings.Configured[0]
	if published.HostIP != "127.0.0.1" || configured != published || configured.HostPort == "" || net.JoinHostPort(published.HostIP, published.HostPort) != os.Getenv("DRAMA_TASK_TEST_NSQ") {
		t.Fatal("test broker requires a fixed isolated loopback port matching the NSQ address")
	}
	s, _ := fixture(t)
	topic := identity.New("broker")
	s.Queue = nsqQueue(t, topic)
	worker := process(t, topic)
	scope := identity.New("test")
	v := start(t, s, command(scope, "image", 3000))
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, e := s.Store.Task(ctx, v.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Remote() != nil {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if out, e := exec.Command("docker", "stop", name).CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	t.Cleanup(func() { exec.Command("docker", "start", name).Run() })
	queued := start(t, s, command(scope, "video", 0))
	if out, e := exec.Command("docker", "start", name).CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	deadline = time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		a, e := s.Store.Task(ctx, v.ID)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.Store.Task(ctx, queued.ID)
		if e != nil {
			t.Fatal(e)
		}
		if a.Status == d.Succeeded && b.Status == d.Succeeded {
			if len(a.State.Submissions) != 1 || len(b.State.Submissions) != 1 {
				t.Fatal("broker restart duplicate submission")
			}
			worker.Process.Kill()
			worker.Wait()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("broker reconnect/recovery timeout")
}
func TestTwoProcessesNSQRestartRecoveryAndHundredTasks(t *testing.T) {
	s, _ := fixture(t)
	topic := identity.New("test")
	s.Queue = nsqQueue(t, topic)
	scope := identity.New("test")
	// PG acceptance with a failed initial publish is repaired by startup scan in independent processes.
	s.Queue = &noQueue{fail: true}
	recovering := start(t, s, command(scope, "video", 400))
	s.Queue = nsqQueue(t, topic)
	a := process(t, topic)
	b := process(t, topic)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		got, e := s.Store.Task(context.Background(), recovering.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Remote() != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if e := a.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	a.Wait()
	ids := make([]string, 100)
	latencies := make([]time.Duration, 100)
	began := time.Now()
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			at := time.Now()
			v, e := s.Submit(context.Background(), command(scope, "image", 10000))
			latencies[i] = time.Since(at)
			if e != nil {
				t.Error(e)
				return
			}
			ids[i] = v.ID
			accepted.Add(1)

		}(i)
	}
	wg.Wait()
	duplicates := make([]string, 0, 300)
	for _, id := range ids {
		duplicates = append(duplicates, id, id, id)
	}
	if e := s.Queue.PublishTasks(context.Background(), duplicates); e != nil {
		t.Fatal(e)
	}
	if accepted.Load() != 100 {
		t.Fatal("not all accepted")
	}
	deadline = time.Now().Add(30 * time.Second)
	remaining := 100
	maxInFlight := 0
	for time.Now().Before(deadline) {
		remaining = 0
		inFlight := 0
		for _, id := range ids {
			got, e := s.Store.Task(context.Background(), id)
			if e != nil {
				t.Fatal(e)
			}
			if len(got.State.Submissions) > 1 {
				t.Fatal("duplicate external submission", id)
			}
			if got.Remote() != nil && got.Status == d.Running && time.Now().Before(got.Remote().ReadyAt) {
				inFlight++
			}
			if got.Status != d.Succeeded {
				remaining++
			}
		}
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		if remaining == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if remaining != 0 {
		t.Fatalf("remaining=%d", remaining)
	}
	got, e := s.Store.Task(context.Background(), recovering.ID)
	if e != nil || got.Status != d.Succeeded || len(got.State.Submissions) != 1 {
		t.Fatal("crashed worker recovery", e, got.Status)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	fmt.Printf("task load evidence: 100 concentrated, maxInFlight=%d, 300 duplicate notifications, zero duplicate submissions, accept p95=%s p99=%s, completed=%s\n", maxInFlight, latencies[94], latencies[98], time.Since(began))
	if maxInFlight != 100 {
		t.Fatalf("did not exercise 100 simultaneously in-flight tasks: %d", maxInFlight)
	}
	if e = b.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	b.Wait()
}
