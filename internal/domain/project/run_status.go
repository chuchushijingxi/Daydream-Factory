package project

import (
	"drama-agent/internal/domain/fault"
	"time"
)

// 不同生命周期使用独立类型，避免把供应商调用状态写入业务任务状态。
type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

type CallStatus string
type ActionStatus string

// Cancel 只取消仍运行的任务，迟到响应由应用层继续保存费用，不复活任务。
func (r *Run) Cancel() error {
	if r == nil || r.Status != RunRunning {
		return fault.ErrConflict
	}
	ended := time.Now().UTC()
	r.EndedAt = &ended
	r.Status = RunCancelled
	r.ErrorCode = "CANCELLED"
	r.Error = "用户取消；保留已保存目录与实际调用费用"
	return nil
}

// Complete 保持终结状态不可逆，持久化恢复不能再次完成失败或取消任务。
func (r *Run) Complete() error {
	if r == nil || r.Status != RunRunning {
		return fault.ErrConflict
	}
	ended := time.Now().UTC()
	r.EndedAt = &ended
	r.Status = RunCompleted
	return nil
}
