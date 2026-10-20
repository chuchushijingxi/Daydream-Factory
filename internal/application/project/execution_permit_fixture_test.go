package project

import (
	"context"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// Historical finite policies are test fixtures, not production configuration.
// ExecutionPermit 验证受控执行策略的范围；正式工作台不加载许可文件。
// 精确原文 hash 与启动 revision 防止新任务续用旧授权。
type ExecutionPermit struct {
	ProjectID      string           `json:"projectId"`
	SourceSHA256   string           `json:"sourceSHA256"`
	SourceVersion  uint64           `json:"sourceVersion"`
	Contract       string           `json:"contract"`
	InputRevision  uint64           `json:"inputRevision"`
	BudgetMicroCNY int64            `json:"budgetMicroCny"`
	Targets        []d.VisualTarget `json:"targets,omitempty"`
}

func AuthorizePermits(permits []ExecutionPermit) Authorization {
	return func(ctx context.Context, p d.Project, run d.Run) (int64, error) {
		for _, v := range permits {
			if v.ProjectID == p.ID && v.SourceSHA256 == p.ScriptSource.SHA256 && v.SourceVersion == p.ScriptSource.Version && v.Contract == run.Contract && v.InputRevision == run.InputRevision && v.BudgetMicroCNY > 0 && (run.Contract != d.VisualContract || sameTargets(v.Targets, run.Targets)) {
				return v.BudgetMicroCNY, nil
			}
		}
		return 0, fault.New("EXECUTION_NOT_AUTHORIZED", "未找到匹配项目、完整原文、任务与启动版本的本次有限许可")
	}
}

func sameTargets(a, b []d.VisualTarget) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	seen := map[d.VisualTarget]bool{}
	for _, t := range a {
		if seen[t] {
			return false
		}
		seen[t] = true
	}
	for _, t := range b {
		if !seen[t] {
			return false
		}
		delete(seen, t)
	}
	return len(seen) == 0
}
