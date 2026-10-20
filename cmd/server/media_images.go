package main

import (
	"context"
	"log"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/project"
)

// 与积分恢复同周期复核补空位；补图本身由媒体任务异步推进，这里只做采用复核。
func runImageFills(ctx context.Context, service *project.Service) {
	tick := time.NewTicker(appconfig.CreditRecoveryInterval)
	defer tick.Stop()
	for {
		if err := service.RecoverImageFills(ctx); err != nil && ctx.Err() == nil {
			log.Printf("image fill recovery: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
