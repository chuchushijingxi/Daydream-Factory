// Package taskruntime assembles the same task execution resources for API and workers.
package taskruntime

import (
	"context"
	"fmt"
	"log"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/mediatools"
	"drama-agent/internal/adapter/mockmedia"
	"drama-agent/internal/adapter/nsq"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/task"
	d "drama-agent/internal/domain/task"
)

func New(c appconfig.Config, store *postgres.Store, objects *s3objects.Store, resolver ports.TaskResolver, capacity ports.TaskCapacity, generated ...ports.GeneratedFiles) (*app.Runtime, *nsq.Queue, error) {
	if capacity == nil {
		return nil, nil, fmt.Errorf("task runtime requires shared Redis capacity")
	}
	var files ports.GeneratedFiles
	if len(generated) > 0 {
		files = generated[0]
	}
	q, err := nsq.New(c.NSQ)
	if err != nil {
		return nil, nil, err
	}
	t := c.Tasks
	// 缩略图、视频核对与 Mock 视频共用一套媒体工具：Compose 中是独立媒体服务，主机直跑时是本机命令。
	tools := mediatools.New(c.Media)
	s := &app.Service{
		Capacity:      capacity,
		SpaceResolver: store, SpaceAccess: store,
		Store: postgres.NewTaskStore(store), Resolver: resolver, Provider: mockmedia.TaskProvider{Encoder: tools},
		Results: &s3objects.TaskResults{ReadOnly: c.ReadOnly, Thumbnails: tools, Videos: tools, Spaces: postgres.NewTaskStore(store), Files: files, Objects: objects}, Queue: q, ReadOnly: c.ReadOnly,
		Lease: t.Lease, SubmitConcurrency: t.SubmitConcurrency, QueryConcurrency: t.QueryConcurrency, SaveConcurrency: t.SaveConcurrency,
		ObserveTimeout: t.ObserveTimeout, SaveTimeout: t.SaveTimeout, EvidenceTimeout: t.EvidenceTimeout, CapacityRetry: t.CapacityRetry,
		Policy: d.Policy{PollMillis: t.PollInterval.Milliseconds(), RetryMillis: t.RetryInterval.Milliseconds(), MaxSubmissions: t.MaxSubmissions},
	}
	return &app.Runtime{Service: s, ScanInterval: t.ScanInterval, Batch: t.ScanBatch, MaxBatches: t.ScanMaxBatches, ScanTimeout: t.ScanTimeout}, q, nil
}

func Run(ctx context.Context, r *app.Runtime, restartInterval time.Duration) {
	if r.Service.ReadOnly {
		return
	}
	for ctx.Err() == nil {
		if err := r.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("task worker unavailable, retrying: %v", err)
		}
		timer := time.NewTimer(restartInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
