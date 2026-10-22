package ports

import "context"

// WithoutSpaceRequest starts a fresh authorization boundary for long-running
// Agent/tool work. Context cancellation and unrelated values are preserved, but
// an inherited HTTP request-local member/status cache must never survive a tool
// step, including contexts produced with context.WithoutCancel.
func WithoutSpaceRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestedSpaceKey{}, (*spaceRequest)(nil))
}
