package ports

import "context"

// 带 Owner 的存储由资源模块创建；空归属一律失败。
type OwnedProjects interface{ ForProjectOwner(string) ProjectStore }
type OwnedFiles interface {
	ForUploadedFileOwner(string) UploadedFileStore
}
type OwnedFileReader interface{ ForFileOwner(string) FileReader }
type OwnedTasks interface{ ForTaskOwner(string) TaskStore }

type TaskOwners interface {
	TaskOwner(context.Context, string) (string, error)
}
type OwnedGeneratedFiles interface{ ForGeneratedOwner(string) GeneratedFiles }
