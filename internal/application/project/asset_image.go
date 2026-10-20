package project

import (
	"context"
	"strings"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	u "drama-agent/internal/domain/upload"
)

func (s *Service) SaveImage(ctx context.Context, id string, revision uint64, target d.VisualTarget, fileID string) (d.Project, error) {
	if err := ctx.Err(); err != nil {
		return d.Project{}, err
	}
	p, err := s.Project(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Revision != revision {
		return p, fault.ErrConflict
	}
	change := func(p *d.Project) error {
		if err := p.ValidateImageTarget(target); err != nil {
			return err
		}
		if s.Files == nil {
			return fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
		}
		file, err := s.Files.File(ctx, fileID)
		if err != nil {
			return err
		}
		// 上传元数据经真实 S3 字节与缩略图核对后才发布且不可变；
		// 缺少真实缩略图的声明图片直接失败。
		if !publishedUploadImage(fileID, file) {
			return fault.New("INVALID_ASSET_IMAGE", "请上传带缩略图的图片文件")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return p.SetImage(target, fileID)
	}
	if err = change(&p); err != nil {
		return p, err
	}
	return s.commit(ctx, p, revision)
}

// publishedUploadImage 复核已发布上传的回执：原文件与缩略图必须是同一 bucket 内
// 经字节、哈希与对象键核对的真实对象，且缩略图为非零尺寸的 WebP；
// 缺少真实缩略图的声明图片不能进入资产目录。
func publishedUploadImage(fileID string, file u.File) bool {
	if file.ID != fileID || !strings.HasPrefix(file.Original.ContentType, "image/") || file.Thumbnail == nil {
		return false
	}
	if !file.Original.Valid(fileID) || file.Original.ObjectKey != u.ObjectKey(fileID, "original") || file.Original.SizeBytes == 0 {
		return false
	}
	thumb := file.Thumbnail.Blob
	if !thumb.Valid(fileID) || thumb.ObjectKey != u.ObjectKey(fileID, "thumbnail.webp") || thumb.ContentType != "image/webp" || thumb.SizeBytes == 0 {
		return false
	}
	return thumb.Bucket == file.Original.Bucket && file.Thumbnail.Width >= 1 && file.Thumbnail.Height >= 1
}
