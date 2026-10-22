package ports

import (
	"context"
	"time"
)

// ProjectEditor 是Redis维护的短期占用，SessionID只由已认证传输层导出，不接受客户端自行声明。
type ProjectEditor struct {
	UserID    string
	SessionID string
	ExpiresAt time.Time
}

// ProjectEditing 与后台执行锁独立；续租仅延长仍匹配user/session的占用，不重新抢占。
type ProjectEditing interface {
	AcquireProjectEditing(context.Context, string, string, string) (ProjectEditor, error)
	RenewProjectEditing(context.Context, string, string, string) (ProjectEditor, error)
	ReleaseProjectEditing(context.Context, string, string, string) error
	CheckProjectEditing(context.Context, string, string, string) error
}

type ProjectEditorNames interface {
	ProjectEditorName(context.Context, string) (string, error)
}
