package payment

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 一次最多领取50项，全部立即执行；每项45秒的总期限短于1分钟租期。
// 渠道及存储端口必须遵守Context，领取失败或超时只保留持久化的待处理状态，
// 下个实例在租期后接管。不会让排在批尾的订单等到租期失效才开始外发。
func runClaimed[T any](ctx context.Context, items []T, run func(context.Context, T) error) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []error
	for _, item := range items {
		wg.Go(func() {
			operationCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			if err := run(operationCtx, item); err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(failures...)
}
