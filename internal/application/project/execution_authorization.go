package project

import (
	"context"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// 测试/受控执行可以注入范围与费用策略；正式工作台使用用户明确启动。
type Authorization func(context.Context, d.Project, d.Run) (int64, error)

// UserInitiatedExecution 对应用户在本地工作台的明确启动命令。
// 0 表示用户未设置应用层费用上限，不伪造巨大预算，费用仍逐次持久化。
func UserInitiatedExecution(context.Context, d.Project, d.Run) (int64, error) {
	return 0, nil
}

func (s *Service) permit(ctx context.Context, p d.Project, run d.Run) (int64, error) {
	if s.Authorize == nil {
		return 0, fault.New("EXECUTION_NOT_AUTHORIZED", "当前服务未开放任务执行")
	}
	n, err := s.Authorize(ctx, p, run)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fault.New("EXECUTION_NOT_AUTHORIZED", "执行策略的预算不能为负数")
	}
	return n, nil
}

func RunUsed(r *d.Run) int64 { p := d.Project{Runs: []d.Run{*r}}; return p.UsedMicroCNY() }
