package main

import (
	"context"
	"log"
	"time"

	"drama-agent/internal/adapter/appconfig"
)

// 团队提醒和周期发放沿同一生命周期运行，幂等事实由空间事务持久化。
func runTeamSchedule(ctx context.Context, schedule func(context.Context, time.Time, int) (int, error)) {
	ticker := time.NewTicker(appconfig.CreditRecoveryInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if _, err := schedule(ctx, time.Now().UTC(), 100); err != nil && ctx.Err() == nil {
			log.Printf("team schedule failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
