package ports

import (
	"context"
	"time"

	d "drama-agent/internal/domain/project"
)

// ProjectMediaReader 仅供媒体只读投影复用领域校验，不含原件和调用大正文。
// 写用例必须继续读完整 Snapshot，不能将本投影用于 Commit。
type ProjectMediaReader interface {
	MediaProject(context.Context, string) (d.Project, error)
}

// ProjectSummary 只投影列表需要的字段，避免读取原文与全部调用证据。
type ProjectSummary struct {
	ListMetadata
	SpaceID    string             `json:"spaceId"`
	OwnerID    string             `json:"ownerId"`
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	Filename   string             `json:"filename"`
	UploadedAt *time.Time         `json:"uploadedAt"`
	Revision   uint64             `json:"revision"`
	LatestRun  *ProjectRunSummary `json:"latestRun"`
	// 当前角色主图的只读引用；公开访问地址由 HTTP 适配器沿上传合同生成。
	PreviewImageFileIDs []string `json:"previewImageFileIds"`
}

// 首页只需真实任务的身份/阶段/状态，不加载剧本、响应或调用费用。
type ProjectRunSummary struct {
	ID       string `json:"id"`
	Contract string `json:"contract"`
	Status   string `json:"status"`
	Stage    string `json:"stage"`
}

// ProjectModelReference 是模型配置模块检查引用所需的最小项目读模型。
// 删除项目后仍保留历史引用，不能借清理解除模型身份保护。
type ProjectModelReference struct {
	HasSettings bool                `json:"hasSettings"`
	LLMModel    string              `json:"llmModel"`
	ImageModel  string              `json:"imageModel"`
	VideoModel  string              `json:"videoModel"`
	Runs        []RunModelReference `json:"runs"`
}
type RunModelReference struct {
	Model  string `json:"model"`
	Status string `json:"status"`
}

type ModelReferences interface {
	ModelReferences(context.Context) ([]ProjectModelReference, error)
}

type ProjectQueries interface {
	ListSummaries(context.Context) ([]ProjectSummary, error)
	RunningProjectIDs(context.Context) ([]string, error)
	ModelReferences
}

// VisibleProjects is the user-facing SQL-filtered projection. Internal recovery
// and model-reference queries retain ProjectQueries and do not impersonate users.
type VisibleProjects interface {
	ListVisibleSummaries(context.Context, SpaceScope, ListFilter) ([]ProjectSummary, error)
}
