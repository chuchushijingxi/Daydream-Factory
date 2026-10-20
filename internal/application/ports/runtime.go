package ports

import (
	"context"
	"time"
)

// Lease 失租会取消 Context；Release 只释放自己的所有权，绝不释放费用预留。
type Lease struct {
	Context context.Context
	Check   func(context.Context) error
	Release func()
}

type Coordinator interface {
	Lock(context.Context, string, bool) (Lease, bool, error)
}

type SessionRegistry interface {
	SessionAuthenticator
	PutSession(context.Context, string, string, string, Role, time.Time) (bool, error)
	DeleteSession(context.Context, string) (bool, error)
	DeleteSessionDigest(context.Context, string, string, string) (bool, error)
	DeleteUserSessions(context.Context, string) ([]string, error)
	SetSessionsDisabled(context.Context, string, bool) error
}

type RateLimiter interface {
	Allow(context.Context, string, int, time.Duration) (bool, error)
	ResetLimit(context.Context, string) error
}

type ReadCache interface {
	ReadCached(context.Context, string, time.Duration, func(context.Context) ([]byte, error)) ([]byte, error)
	Invalidate(context.Context, ...string) error
}

type UpdateBus interface {
	Watch(context.Context, string) (<-chan struct{}, func(), error)
	Publish(context.Context, string) error
}

type TaskSchedule interface {
	ScheduleTask(context.Context, string, uint64, *time.Time) error
	ScheduledTasks(context.Context, *TaskCursor, int) ([]TaskDue, error)
	ScheduleReady(context.Context) (bool, error)
	MarkScheduleReady(context.Context) error
}

// RuntimeState 仅供资源组装及持久化适配器使用；各应用用例消费上面的窄端口。
type RuntimeState interface {
	Coordinator
	SessionRegistry
	RateLimiter
	ReadCache
	UpdateBus
	TaskSchedule
}
