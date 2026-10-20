package localembedding

import (
	"context"
	"testing"
	"time"
)

func TestEmbedContextDerivesFromParent(t *testing.T) {
	// 生产配置带超时；派生上下文不能立即完成，否则命令根本不会启动。
	ctx, cancel := embedContext(context.Background(), 2*time.Minute)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout context is already done: %v", ctx.Err())
	default:
	}
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	derived, cancelDerived := embedContext(parent, time.Second)
	defer cancelDerived()
	select {
	case <-derived.Done():
	default:
		t.Fatal("parent cancellation must propagate to derived context")
	}
	noTimeout, cancelNoTimeout := embedContext(context.Background(), 0)
	defer cancelNoTimeout()
	select {
	case <-noTimeout.Done():
		t.Fatalf("no-timeout context is already done: %v", noTimeout.Err())
	default:
	}
}
