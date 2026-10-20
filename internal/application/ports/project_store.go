package ports

import (
	"context"

	d "drama-agent/internal/domain/project"
)

// ProjectStore 是项目聚合的唯一持久化端口。Create 分配初始记录，
// Snapshot 返回独立快照，Commit 以 expected revision 原子更新完整聚合。
type ProjectStore interface {
	ProjectQueries
	Create(context.Context, d.Project) error
	Snapshot(context.Context, string) (d.Project, error)
	Commit(context.Context, d.Project, uint64) error
	// 项目锁跨进程防重；释放锁不释放费用或未知预留。
	Acquire(context.Context, string) (lease Lease, acquired bool, err error)
}

// ProjectReader 供项目/分镜只读视图取现行事实及调用计量，不运输原件和调用正文。
// 写入与任务恢复仍必须使用完整 Snapshot。
type ProjectReader interface {
	ReadProject(context.Context, string) (d.Project, error)
}
