package ports

import "context"

// MediaImageFills 只定位已请求补空位且当前仍空的成功任务，不返回正文或第二份状态。
type MediaImageFill struct{ OwnerID, ProjectID, TaskID string }
type MediaImageFills interface {
	PendingImageFills(context.Context, string, int) ([]MediaImageFill, error)
}
