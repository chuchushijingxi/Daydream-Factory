package ports

import (
	"context"
	d "drama-agent/internal/domain/upload"
	"time"
)

type UploadedFileStore interface {
	CreateUploadedFile(context.Context, d.File) error
	UploadedFile(context.Context, string) (d.File, error)
}

// PutFile 在 PUT 结果不明时也返回本次分配的新坐标，
// 保证补偿只可能删除本次上传自己的对象。
type FileObjects interface {
	DetectContentType([]byte) string
	PutFile(context.Context, string, string, string, []byte) (d.Blob, error)
	ReadFile(context.Context, string, d.Blob) ([]byte, error)
	RemoveFile(context.Context, string, d.Blob) error
}

type ThumbnailOptions struct {
	MaxWidth  int `json:"maxWidth"`
	MaxHeight int `json:"maxHeight"`
}

type ThumbnailImage struct {
	Data          []byte
	Width, Height int
}

type ImageThumbnailer interface {
	Generate(context.Context, []byte, ThumbnailOptions) (ThumbnailImage, error)
}

// FileReader 是面向业务的端口：调用者使用稳定 ID，绝不接触 S3 键。
type FileReader interface {
	File(context.Context, string) (d.File, error)
	ReadOriginal(context.Context, string) (d.File, []byte, error)
}

// GeneratedFiles publishes immutable generated images with stable identity and a thumbnail.
// Repeated saves of the same task/output reuse the same verified file.
type GeneratedFiles interface {
	ImportGenerated(context.Context, string, MediaResult) (d.File, error)
}

// ContainerFileReader is consumed only after the project/canvas application has
// authorized a real container and verified this exact file reference. It never
// grants visibility in selectors; implementations must enforce the same space.
type ContainerFileReader interface {
	ContainerFile(context.Context, string, string) (d.File, error)
	ReadContainerFile(context.Context, string, string, bool) (d.File, []byte, error)
}

type UploadedFilePosition struct {
	At     time.Time `json:"at"`
	ID     string    `json:"id"`
	Filter string    `json:"filter"`
}
type UploadedFileQuery struct {
	Filter ListFilter
	Search string
	After  *UploadedFilePosition
	Limit  int
}

// UploadedFileItem excludes storage coordinates and includes only selector data.
type UploadedFileItem struct {
	ListMetadata
	ID           string    `json:"id"`
	SpaceID      string    `json:"spaceId"`
	OwnerID      string    `json:"ownerId"`
	Filename     string    `json:"filename"`
	ContentType  string    `json:"contentType"`
	SizeBytes    int64     `json:"sizeBytes"`
	HasThumbnail bool      `json:"hasThumbnail"`
	CreatedAt    time.Time `json:"createdAt"`
}
type UploadedFileQueries interface {
	ListUploadedFiles(context.Context, SpaceScope, UploadedFileQuery) ([]UploadedFileItem, error)
}
