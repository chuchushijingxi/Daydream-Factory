package project

import (
	"context"
	"drama-agent/internal/application/ports"
	"errors"
	"log"
	"sync"
	"time"
)

// Workers 管理进程内执行、取消、恢复与关闭；项目锁仍由仓储保护跨进程隔离。
// 任务终止后等待迟到响应结算，费用与未知预留不因退出丢失。
type Workers struct {
	Updates  ports.UpdateBus
	Service  *Service
	ReadOnly bool
	mu       sync.Mutex
	workers  map[string]context.CancelFunc
	wg       sync.WaitGroup
	closed   bool
}

func NewWorkers(service *Service) *Workers {
	return &Workers{Service: service, workers: map[string]context.CancelFunc{}}
}
func (h *Workers) CancelWorker(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cancel := h.workers[id]; cancel != nil {
		cancel()
	}
}

func (h *Workers) Launch(id string) {
	if h.ReadOnly {
		return
	}
	initial, err := h.Service.Store.Snapshot(context.Background(), id)
	if err != nil || initial.DeletedAt != nil || initial.LatestRun() == nil {
		return
	}
	runID := initial.LatestRun().ID
	h.mu.Lock()
	if h.closed || h.workers[id] != nil {
		h.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.workers[id] = cancel
	h.wg.Add(1)
	h.mu.Unlock()
	go func() {
		defer h.wg.Done()
		defer cancel()
		if h.Updates != nil {
			go h.watchCancellation(ctx, id, runID, cancel)
		}
		defer func() {
			h.mu.Lock()
			delete(h.workers, id)
			h.mu.Unlock()
			// 取消后的旧请求可能仍在结算，新任务等待旧 worker 退出后再启动。
			current, err := h.Service.Store.Snapshot(context.Background(), id)
			if err == nil && current.LatestRun() != nil && current.LatestRun().Status == "running" && current.LatestRun().ID != runID {
				h.Launch(id)
			}
		}()
		for {
			err := h.Service.Execute(ctx, id)
			busy := errors.Is(err, errExecutionBusy)
			if err != nil && !busy {
				log.Printf("task execution %s stopped: %v", id, err)
			}
			current, err := h.Service.Store.Snapshot(context.Background(), id)
			if err != nil || ctx.Err() != nil || current.LatestRun() == nil || current.LatestRun().Status != "running" {
				break
			}
			if busy {
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				continue
			}
			if current.LatestRun().ID == runID {
				break
			}
			// 原子接续仍属于同次启动；保持 worker 在册，取消与等待覆盖整个流程。
			runID = current.LatestRun().ID
		}
	}()
}

// Hints make cancellation prompt across nodes. Periodic reconciliation covers
// disconnects and the commit-to-publish gap, always checking the exact run.
func (h *Workers) watchCancellation(ctx context.Context, id, runID string, cancel context.CancelFunc) {
	var updates <-chan struct{}
	closeWatch := func() {}
	defer func() { closeWatch() }()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		if updates == nil {
			ch, close, err := h.Updates.Watch(ctx, "project:"+id)
			if err == nil {
				updates, closeWatch = ch, close
			}
		}
		p, err := h.Service.Store.Snapshot(ctx, id)
		if err == nil {
			if p.DeletedAt != nil || p.LatestRun() == nil {
				cancel()
				return
			}
			for _, run := range p.Runs {
				if run.ID == runID && (run.Status == "cancelled" || run.Status == "failed") {
					cancel()
					return
				}
			}
			if p.LatestRun().Status != "running" {
				return
			}
			// Automatic continuation is the same worker's next run, not cancellation.
			runID = p.LatestRun().ID
		}
		select {
		case <-ctx.Done():
			return
		case _, ok := <-updates:
			if !ok {
				closeWatch()
				closeWatch = func() {}
				updates = nil
			}
		case <-tick.C:
		}
	}
}

func (h *Workers) Recover(ctx context.Context) error {
	if h.ReadOnly {
		return nil
	}
	projects, err := h.Service.Store.RunningProjectIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range projects {
		h.Launch(id)
	}
	return nil
}

func (h *Workers) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	h.closed = true
	for _, cancel := range h.workers {
		cancel()
	}
	h.mu.Unlock()
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitUntilIdle 仅供集成/测试验证等待后台任务退出；服务关闭使用 Shutdown，页面不能据此发起新任务。
func (h *Workers) WaitUntilIdle(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		h.mu.Lock()
		n := len(h.workers)
		h.mu.Unlock()
		if n == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunCreditRecovery 以真实账本回执核对被中断的终结提交；只做结算，不创建任务、不重发供应商请求。
func (h *Workers) RunCreditRecovery(ctx context.Context, interval time.Duration) {
	if h.ReadOnly || h.Service.Credits == nil {
		return
	}
	if interval <= 0 {
		log.Print("credit recovery interval is not configured")
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if e := h.Service.RecoverCredits(ctx); e != nil && ctx.Err() == nil {
			log.Printf("credit recovery failed: %v", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
