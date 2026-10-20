package ports

import (
	"context"
	d "drama-agent/internal/domain/task"
	"encoding/json"
	"time"
)

type TaskCursor struct {
	At time.Time
	ID string
}
type TaskStore interface {
	CreateTask(context.Context, d.Task) (d.Task, error)
	Task(context.Context, string) (d.Task, error)
	TaskInput(context.Context, string) (d.Snapshot, error)
	TaskByCommand(context.Context, string, string) (d.Task, error)
	ListTasks(context.Context, string, string, *TaskCursor, int) ([]d.Task, error)
	ClaimTask(context.Context, string, string, time.Duration) (d.Task, bool, error)
	CommitTask(context.Context, d.Task, uint64, string, bool) (d.Task, error)
	RenewTask(context.Context, string, string, time.Duration) error
	CancelTask(context.Context, string, string, uint64) (d.Task, error)
	AppendTaskEvidence(context.Context, string, TaskEvidence) (d.Task, error)
	AcquireTaskScan(context.Context) (TaskScan, bool, error)
	DueTasks(context.Context, *TaskCursor, int) ([]TaskDue, error)
}
type TaskDue struct {
	ID string
	At time.Time
}

// TaskInputs 按同一 scope 一次取回多个任务的不可变调用方快照，
// 供列表投影避免逐条往返；内容对任务模块保持不透明，由业务调用方解释。
type TaskInputs interface {
	TaskInputs(context.Context, string, []string) (map[string]d.Snapshot, error)
}

// Check校验当前Redis扫描租约仍有效；不授予任务执行权。
type TaskScan struct {
	Release func()
	Check   func(context.Context) error
}
type TaskEvidence struct {
	SubmissionID string
	Receipt      *d.Receipt
	Observation  *d.Observation
}
type TaskResolver interface {
	ResolveTask(context.Context, string, string) (d.Binding, error)
}

// TaskSubmissionPermit confirms an already persisted caller prerequisite before
// the existing outbound gate. The kernel does not interpret callerContext or
// know accounts/credits/projects. Errors are retryable; a rejection is terminal.
type TaskSubmissionPermit interface {
	ConfirmTaskSubmission(context.Context, d.Task, d.Snapshot) (*TaskRejection, error)
}
type TaskRejection struct{ Code, Message string }
type TaskProvider interface {
	Prepare(context.Context, d.Binding, json.RawMessage) (json.RawMessage, error)
	Submit(context.Context, d.Binding, string, json.RawMessage) (d.Receipt, error)
	Query(context.Context, d.Binding, d.Remote) (d.Receipt, error)
	Fetch(context.Context, d.Binding, d.SourceFile, json.RawMessage) (MediaResult, error)
}
type TaskQueue interface {
	PublishTasks(context.Context, []string) error
	ConsumeTasks(context.Context, func(context.Context, string) error) error
}

type TaskCapacity interface {
	AcquireTaskCapacity(context.Context, d.Binding, string, int) (Lease, bool, error)
}
type TaskResultStore interface {
	SaveTaskOutput(context.Context, string, string, MediaResult) (d.Output, error)
	ReadTaskOutput(context.Context, string, d.Output) ([]byte, error)
	SaveTaskRaw(context.Context, string, string, []byte) (d.Blob, error)
	ReadTaskRaw(context.Context, string, d.Blob) ([]byte, error)
}

// TaskOutputThumbnails derives a cached still from an already saved output.
// It never submits a provider request or changes the task or its billing.
type TaskOutputThumbnails interface {
	ReadTaskThumbnail(context.Context, string, d.Output) ([]byte, error)
}
