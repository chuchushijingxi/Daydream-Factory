package ports

import "context"

// 空间派生存储限制读写范围；用户是否可访问具体资源仍须经 SpaceAccess 核验。
type ScopedProjects interface{ ForProjectSpace(string) ProjectStore }
type ScopedFiles interface {
	ForUploadedFileSpace(string) UploadedFileStore
}
type ScopedFileReader interface{ ForFileSpace(string) FileReader }
type ScopedTasks interface{ ForTaskSpace(string) TaskStore }

// 幂等命令键含发起人，团队中的不同用户不能回读彼此同编号的命令。
type ScopeTasks interface{ ForTaskScope(SpaceScope) TaskStore }

type TaskSpaces interface {
	TaskSpace(context.Context, string) (spaceID, ownerID, creatorID string, err error)
}
type ScopedGeneratedFiles interface {
	ForGeneratedResource(spaceID, ownerID, creatorID string) GeneratedFiles
}
