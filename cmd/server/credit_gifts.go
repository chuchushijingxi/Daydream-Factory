package main

import (
	"context"
	"log"
	"time"

	"drama-agent/internal/adapter/appconfig"
)

// 生命周期仅执行到期回收；登录后的领取仍由原受保护工作台命令触发。
func runDailyGiftExpiration(ctx context.Context, expire func(context.Context) error) {
	ticker := time.NewTicker(appconfig.CreditRecoveryInterval)
	defer ticker.Stop()
	for {
		if err := expire(ctx); err != nil && ctx.Err() == nil {
			log.Printf("daily credit expiration failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
