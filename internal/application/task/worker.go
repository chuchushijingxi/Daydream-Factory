package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/task"
)

func next(t *d.Task, delay int64) {
	at := time.Now().UTC().Add(time.Duration(delay) * time.Millisecond)
	t.NextRunAt = &at
}
func stop(t *d.Task, status d.Status, code, message string) error {
	if err := t.Transition(status); err != nil {
		return err
	}
	t.State.ErrorCode = code
	t.State.Error = message
	t.NextRunAt = nil
	return nil
}
func (s *Service) persist(ctx context.Context, t d.Task, release bool) (d.Task, error) {
	return s.Store.CommitTask(ctx, t, t.Revision, t.LeaseToken, release)
}
func (s *Service) Drive(ctx context.Context, id string) error {
	if s.ReadOnly {
		return nil
	}
	lease := s.Lease
	if lease < 3*time.Millisecond {
		return fmt.Errorf("task lease is not configured")
	}
	t, ok, err := s.Store.ClaimTask(ctx, id, identity.New("claim"), lease)
	if err != nil || !ok {
		return err
	}
	execution, cancel := context.WithCancel(ctx)
	defer cancel()
	execution = context.WithValue(execution, capacityCancelKey{}, context.CancelFunc(cancel))
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	claimID, claimToken := t.ID, t.LeaseToken
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-execution.Done():
				return
			case <-ticker.C:
				if err := s.Store.RenewTask(execution, claimID, claimToken, lease); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(done); wg.Wait() }()
	if t.Status.Terminal() {
		if t.Billing().Status != "known" && t.Remote() != nil {
			return s.execute(execution, t, 1, func() error { return s.query(execution, t, true) })
		}
		t.NextRunAt = nil
		_, err = s.persist(execution, t, true)
		return err
	}
	// 已持久化的明确失败是原请求事实，不能因恢复时观察期限已过而退回 unknown。
	if observation := t.State.LastObservation; observation != nil && observation.Receipt.Outcome == "failed" {
		if err = s.apply(&t, observation.Receipt, false); err != nil {
			return err
		}
		_, err = s.persist(execution, t, true)
		return err
	}
	if desc := t.Description(); desc != nil {
		return s.execute(execution, t, 2, func() error { return s.save(execution, t, *desc) })
	}
	if remote := t.Remote(); remote != nil {
		if time.Now().After(t.State.Policy.ObserveUntil) {
			if e := stop(&t, d.Unknown, "TASK_STATUS_UNKNOWN", "已超出可靠观察期限，不能推断厂商最终结果"); e != nil {
				return e
			}
			_, err = s.persist(execution, t, true)
			return err
		}
		return s.execute(execution, t, 1, func() error { return s.query(execution, t, false) })
	}
	sub := t.LatestSubmission()
	// may_have_sent 是发送门的持久事实；显式 unknown 析取防御历史数据行，
	// 正常路径 Disposition 已把 unknown 结果归入 may_have_sent。
	if sub != nil && (sub.Disposition == d.DispositionMayHaveSent || (sub.Receipt != nil && sub.Receipt.Outcome == "unknown")) {
		if e := stop(&t, d.Unknown, "TASK_SUBMISSION_UNKNOWN", "可能已提交，缺少可靠受理回执；不会重新生成"); e != nil {
			return e
		}
		_, err = s.persist(execution, t, true)
		return err
	}
	if t.Status == d.Unknown {
		t.NextRunAt = nil
		_, err = s.persist(execution, t, true)
		return err
	}
	// 队列积压不能让从未发送的任务在原观察窗口结束后才扣款、外发；
	// 已有提交或 unknown 仍沿原证据恢复，不能以超时推断其最终结果。
	if sub == nil && time.Now().After(t.State.Policy.ObserveUntil) {
		if e := stop(&t, d.Failed, "TASK_START_EXPIRED", "任务已超过启动期限，未提交生成"); e != nil {
			return e
		}
		_, err = s.persist(execution, t, true)
		return err
	}
	return s.execute(execution, t, 0, func() error { return s.submit(execution, t) })
}

// 各节点独立的阶段并发上限；大批量媒体保存不得占满全部查询槽位。
func (s *Service) execute(ctx context.Context, t d.Task, stage int, action func() error) error {
	s.gatesOnce.Do(func() {
		limits := [3]int{s.SubmitConcurrency, s.QueryConcurrency, s.SaveConcurrency}
		for i, n := range limits {
			if n > 0 {
				s.gates[i] = make(chan struct{}, n)
			}
		}
	})
	if s.gates[stage] == nil || s.CapacityRetry <= 0 {
		return fmt.Errorf("task concurrency is not configured")
	}
	select {
	case s.gates[stage] <- struct{}{}:
		defer func() { <-s.gates[stage] }()
		if s.Capacity != nil {
			limit := [3]int{s.SubmitConcurrency, s.QueryConcurrency, s.SaveConcurrency}[stage]
			lease, ok, err := s.Capacity.AcquireTaskCapacity(ctx, t.State.Binding, []string{"submit", "query", "save"}[stage], limit)
			if err != nil {
				return err
			}
			if !ok {
				next(&t, s.CapacityRetry.Milliseconds())
				_, err = s.persist(ctx, t, true)
				return err
			}
			defer lease.Release()
			// action closes over the execution context. Cancelling the permit's
			// context must cancel that same execution, including the outbound gate.
			cancelExecution, ok := ctx.Value(capacityCancelKey{}).(context.CancelFunc)
			if ok {
				stop := context.AfterFunc(lease.Context, cancelExecution)
				defer stop()
			}
			if err = lease.Check(ctx); err != nil {
				return err
			}
		}
		return action()
	default:
		next(&t, s.CapacityRetry.Milliseconds())
		_, err := s.persist(ctx, t, true)
		return err
	}
}

type capacityCancelKey struct{}

func (s *Service) raw(ctx context.Context, id, key string, body json.RawMessage) (json.RawMessage, *d.Blob, error) {
	if len(body) <= 64<<10 {
		return body, nil, nil
	}
	blob, err := s.Results.SaveTaskRaw(ctx, id, key, body)
	if err != nil {
		return nil, nil, err
	}
	return nil, &blob, nil
}
func (s *Service) receipt(ctx context.Context, id, key string, r d.Receipt) (d.Receipt, error) {
	raw, blob, err := s.raw(ctx, id, key, r.Raw)
	if err == nil {
		r.Raw = raw
		r.RawObject = blob
	}
	if err != nil {
		// 完整响应保留优先于控制任务行体积；对象存储不可用不得抹掉
		// 已知回执或费用证据。
		log.Printf("task %s raw receipt kept in PG until object storage recovers: %v", id, err)
		r.Raw = append(json.RawMessage(nil), r.Raw...)
		return r, nil
	}
	return r, nil
}
func (s *Service) preserve(ctx context.Context, t d.Task, e ports.TaskEvidence, original error) error {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.EvidenceTimeout)
	defer cancel()
	if _, err := s.Store.AppendTaskEvidence(c, t.ID, e); err != nil {
		return fmt.Errorf("save provider evidence: %w", errors.Join(original, err))
	}
	// 过期持有者只能补记证据，不能继续执行或采用。
	return nil
}
func (s *Service) apply(t *d.Task, r d.Receipt, settlement bool) error {
	if settlement {
		if r.Billing.Status == "known" {
			if sub := t.LatestSubmission(); sub != nil && sub.Receipt != nil {
				sub.Receipt.Billing = r.Billing
			}
			t.NextRunAt = nil
		} else if time.Now().Before(t.State.Policy.SaveUntil) {
			next(t, t.State.Policy.PollMillis)
		} else {
			// 结算轮询到保存期限即停止；费用未知与预留证据保留在已存提交里，
			// 不释放预留、不改写终态。迟到回执仍可经 AppendTaskEvidence 再核对一次。
			t.NextRunAt = nil
		}
		return nil
	}
	t.State.ErrorCode = ""
	t.State.Error = ""
	switch r.Outcome {
	case "accepted":
		if r.Remote == nil || r.Remote.ID == "" {
			return stop(t, d.Unknown, "TASK_SUBMISSION_UNKNOWN", "受理回执缺少可靠任务ID")
		}
		next(t, t.State.Policy.PollMillis)
	case "pending":
		next(t, t.State.Policy.PollMillis)
	case "completed":
		if r.Result == nil || (len(r.Result.Files) == 0 && len(r.Result.Data) == 0) {
			return stop(t, d.Failed, "TASK_RESULT_INVALID", "厂商完成回执缺少必要结果")
		}
		if err := t.Transition(d.Running); err != nil {
			return err
		}
		next(t, 0)
	case "failed":
		return stop(t, d.Failed, r.ErrorCode, r.Error)
	case "not_sent":
		if len(t.State.Submissions) >= t.State.Policy.MaxSubmissions {
			return stop(t, d.Failed, "MODEL_PROVIDER_FAILED", "安全运输重试次数已用尽")
		}
		next(t, t.State.Policy.RetryMillis)
	default:
		return stop(t, d.Unknown, "TASK_STATUS_UNKNOWN", "厂商状态或运输结果无法确认")
	}
	if t.Status == d.Unknown {
		return t.Transition(d.Running)
	}
	return nil
}
func (s *Service) submit(ctx context.Context, t d.Task) error {
	if len(t.State.Submissions) >= t.State.Policy.MaxSubmissions {
		if e := stop(&t, d.Failed, "MODEL_PROVIDER_FAILED", "提交次数已用尽"); e != nil {
			return e
		}
		_, err := s.persist(ctx, t, true)
		return err
	}
	snapshot, err := s.Store.TaskInput(ctx, t.ID)
	if err != nil {
		return err
	}
	if t.State.RequirePermit {
		if s.Permit == nil {
			return fmt.Errorf("task submission permit is unavailable")
		}
		rejection, e := s.Permit.ConfirmTaskSubmission(ctx, t, snapshot)
		if e != nil {
			// An unconfirmed prerequisite must not become a provider submission.
			// Release the lease for recovery, keeping the immutable caller intent.
			next(&t, t.State.Policy.RetryMillis)
			t.State.ErrorCode = "TASK_PREREQUISITE_PENDING"
			t.State.Error = "任务启动条件尚未确认，后台核对后继续"
			if _, save := s.persist(ctx, t, true); save != nil {
				return errors.Join(e, save)
			}
			return e
		}
		if rejection != nil {
			if e = stop(&t, d.Failed, rejection.Code, rejection.Message); e != nil {
				return e
			}
			_, e = s.persist(ctx, t, true)
			return e
		}
	}
	request, err := s.Provider.Prepare(ctx, t.State.Binding, snapshot.Input)
	if err != nil {
		if e := stop(&t, d.Failed, "TASK_PROTOCOL_UNSUPPORTED", err.Error()); e != nil {
			return e
		}
		_, e := s.persist(ctx, t, true)
		return e
	}
	sub := d.Submission{ID: identity.New("submission"), Disposition: d.DispositionPrepared, StartedAt: time.Now().UTC()}
	sub.Request, sub.RequestObject, err = s.raw(ctx, t.ID, sub.ID+"-request", request)
	if err != nil {
		return err
	}
	t.State.Submissions = append(t.State.Submissions, sub)
	if err = t.Transition(d.Running); err != nil {
		return err
	}
	t, err = s.persist(ctx, t, false)
	if err != nil {
		return err
	}
	t.State.Submissions[len(t.State.Submissions)-1].Disposition = d.DispositionMayHaveSent
	t, err = s.persist(ctx, t, false)
	if err != nil {
		return err
	}
	// 只有确认持久化的发送门才允许真实外发；提交丢失绝不据此重发。
	r, callErr := s.Provider.Submit(ctx, t.State.Binding, sub.ID, request)
	if callErr != nil {
		r = d.Receipt{Outcome: "unknown", Raw: json.RawMessage(`{"transport":"interrupted"}`), Error: callErr.Error(), Billing: d.Billing{Status: "unknown"}}
	}
	capture, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.EvidenceTimeout)
	defer cancel()
	r, err = s.receipt(capture, t.ID, sub.ID+"-response", r)
	if err != nil {
		return err
	}
	t.State.Submissions[len(t.State.Submissions)-1].Receipt = &r
	t.State.Submissions[len(t.State.Submissions)-1].Disposition = r.Disposition()
	if err = s.apply(&t, r, false); err != nil {
		return err
	}
	if _, err = s.persist(capture, t, true); err != nil {
		return s.preserve(capture, t, ports.TaskEvidence{SubmissionID: sub.ID, Receipt: &r}, err)
	}
	return nil
}
func (s *Service) query(ctx context.Context, t d.Task, settlement bool) error {
	r, callErr := s.Provider.Query(ctx, t.State.Binding, *t.Remote())
	capture, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.EvidenceTimeout)
	defer cancel()
	if callErr != nil {
		// 查询运输中断不代表原生成失败，不能据此改写任务状态或结果。
		r = d.Receipt{Outcome: "pending", Raw: json.RawMessage(`{"query":"interrupted"}`), Error: callErr.Error(), Billing: d.Billing{Status: "pending"}}
	}
	id := identity.New("observation")
	var err error
	r, err = s.receipt(capture, t.ID, id, r)
	if err != nil {
		return err
	}
	o := d.Observation{ID: id, At: time.Now().UTC(), Receipt: r}
	t.Observe(o)
	if r.Billing.Status == "known" {
		if sub := t.LatestSubmission(); sub != nil && sub.Receipt != nil {
			sub.Receipt.Billing = r.Billing
		}
	}
	if err = s.apply(&t, r, settlement); err != nil {
		return err
	}
	if _, err = s.persist(capture, t, true); err != nil {
		return s.preserve(capture, t, ports.TaskEvidence{Observation: &o}, err)
	}
	return nil
}
func (s *Service) save(ctx context.Context, t d.Task, desc d.Description) error {
	if time.Now().After(t.State.Policy.SaveUntil) {
		if e := stop(&t, d.Failed, "TASK_STORAGE_FAILED", "原结果保存期限已到，不重新生成"); e != nil {
			return e
		}
		_, err := s.persist(ctx, t, true)
		return err
	}
	if t.Status == d.Unknown {
		if err := t.Transition(d.Running); err != nil {
			return err
		}
	}
	if t.State.Result == nil {
		t.State.Result = &d.Result{Data: desc.Data, Files: []d.Output{}}
	}
	for _, source := range desc.Files {
		found := false
		for _, out := range t.State.Result.Files {
			if out.ID == source.ID {
				found = true
			}
		}
		if found {
			continue
		}
		snapshot, err := s.Store.TaskInput(ctx, t.ID)
		if err != nil {
			return err
		}
		media, err := s.Provider.Fetch(ctx, t.State.Binding, source, snapshot.Input)
		if err == nil {
			var out d.Output
			out, err = s.Results.SaveTaskOutput(ctx, t.ID, source.ID, media)
			if err == nil {
				t.State.Result.Files = append(t.State.Result.Files, out)
			}
		}
		if err != nil {
			var failure *fault.Error
			if errors.As(err, &failure) && failure.Code == "TASK_RESULT_INVALID" {
				if e := stop(&t, d.Failed, failure.Code, failure.Message); e != nil {
					return e
				}
				_, e := s.persist(ctx, t, true)
				return e
			}
			t.State.ErrorCode = "TASK_STORAGE_RETRY"
			t.State.Error = err.Error()
			next(&t, t.State.Policy.RetryMillis)
			_, e := s.persist(ctx, t, true)
			return e
		}
		// 每次领取只保存一个文件；重复保存使用同一对象键，天然幂等。
		next(&t, 0)
		_, err = s.persist(ctx, t, true)
		return err
	}
	if err := t.Transition(d.Succeeded); err != nil {
		return err
	}
	t.NextRunAt = nil
	t.State.ErrorCode = ""
	t.State.Error = ""
	if !t.State.Binding.Free && t.Billing().Status != "known" && t.Remote() != nil {
		next(&t, t.State.Policy.PollMillis)
	}
	_, err := s.persist(ctx, t, true)
	return err
}

type Runtime struct {
	Service      *Service
	ScanInterval time.Duration
	Batch        int
	MaxBatches   int
	ScanTimeout  time.Duration
	cursor       *ports.TaskCursor
}

func (r *Runtime) Scan(ctx context.Context) error {
	if r.Service.ReadOnly {
		return nil
	}
	timeout := r.ScanTimeout
	if timeout <= 0 {
		return fmt.Errorf("task scan timeout is not configured")
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lock, ok, err := r.Service.Store.AcquireTaskScan(c)
	if err != nil || !ok {
		return err
	}
	defer lock.Release()
	batch := r.Batch
	if batch <= 0 {
		return fmt.Errorf("task scan batch is not configured")
	}
	rounds := r.MaxBatches
	if rounds <= 0 {
		return fmt.Errorf("task scan rounds are not configured")
	}
	for i := 0; i < rounds; i++ {
		if err := lock.Check(c); err != nil {
			return err
		}
		rows, err := r.Service.Store.DueTasks(c, r.cursor, batch)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			r.cursor = nil
			return nil
		}
		ids := make([]string, len(rows))
		for j, t := range rows {
			ids[j] = t.ID
		}
		if err = lock.Check(c); err != nil {
			return err
		}
		if err = r.Service.Queue.PublishTasks(c, ids); err != nil {
			return err
		}
		last := rows[len(rows)-1]
		r.cursor = &ports.TaskCursor{At: last.At, ID: last.ID}
		if len(rows) < batch {
			r.cursor = nil
			return nil
		}
	}
	return nil
}
func (r *Runtime) Run(ctx context.Context) error {
	if r.Service.ReadOnly {
		return nil
	}
	interval := r.ScanInterval
	if interval <= 0 {
		return fmt.Errorf("task scan interval is not configured")
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := r.Scan(ctx); err != nil {
			log.Printf("task recovery: %v", err)
		}
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if err := r.Scan(ctx); err != nil {
					log.Printf("task recovery: %v", err)
				}
			}
		}
	}()
	err := r.Service.Queue.ConsumeTasks(ctx, r.Service.Drive)
	close(done)
	wg.Wait()
	return err
}
