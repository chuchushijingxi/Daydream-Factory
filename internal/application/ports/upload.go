package ports

import (
	"context"
	d "drama-agent/internal/domain/upload"
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
