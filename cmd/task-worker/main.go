// task-worker consumes frozen provider jobs without business recovery or an HTTP server.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/imagethumbnail"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/redisstate"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/adapter/taskruntime"
	"drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
)

func main() {
	configFile := flag.String("config", "config/app.local.yaml", "private runtime YAML file")
	flag.Parse()
	cfg, err := appconfig.Load(*configFile)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	initCtx, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer cancel()
	store, err := postgres.Open(initCtx, cfg.Database.URL, cfg.Database)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	runtimeState, err := redisstate.Open(initCtx, cfg.Redis)
	if err != nil {
		log.Fatal(err)
	}
	defer runtimeState.Close()
	store.UseRuntime(runtimeState)
	// API owns schema migration; workers never migrate business tables.
	objects, err := s3objects.New(cfg.Storage)
	if err != nil {
		log.Fatal("cannot initialize task S3")
	}
	cancel()
	files := &uploadapp.Service{Store: store, Objects: objects, Thumbnails: imagethumbnail.FFmpeg{Settings: cfg.Media}, CommitCheckTimeout: cfg.Uploads.CommitCheckTimeout, CleanupTimeout: cfg.Uploads.CleanupTimeout}
	runtime, queue, err := taskruntime.New(cfg, store, objects, nil, runtimeState, files)
	if err != nil {
		log.Fatal("cannot initialize task resources")
	}
	defer queue.Close()
	go store.RunRuntimeEvents(ctx)
	runtime.Service.Permit = &project.MediaCreditPermit{Ledger: store, Tasks: runtime.Service.Store}
	// 调用方的结算和补空位协调独立于通用任务执行及浏览器会话。
	// 补图复核项目、原任务与发布文件，必须保留和API相同的归属适配器。
	go func() {
		s := &project.Service{Store: store, Credits: store, Files: files, MediaTasks: runtime.Service}
		tick := time.NewTicker(appconfig.CreditRecoveryInterval)
		defer tick.Stop()
		for {
			if e := s.RecoverMediaCredits(ctx); e != nil && ctx.Err() == nil {
				log.Printf("media credit recovery: %v", e)
			}
			if e := s.RecoverImageFills(ctx); e != nil && ctx.Err() == nil {
				log.Printf("image fill recovery: %v", e)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	taskruntime.Run(ctx, runtime, cfg.Tasks.RestartInterval)
}
