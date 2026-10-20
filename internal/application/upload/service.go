package upload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"path"
	"strings"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/upload"
)

type Service struct {
	owner              *string
	Store              ports.UploadedFileStore
	Objects            ports.FileObjects
	Thumbnails         ports.ImageThumbnailer
	CommitCheckTimeout time.Duration
	CleanupTimeout     time.Duration
}

type Input struct {
	Filename, ContentType string
	Data                  []byte
	Thumbnail             *ports.ThumbnailOptions
}

func (s *Service) Upload(ctx context.Context, in Input) (d.File, error) {
	return s.save(ctx, identity.New("file"), in, false)
}

func (s *Service) ImportGenerated(ctx context.Context, key string, m ports.MediaResult) (d.File, error) {
	sum := sha256.Sum256([]byte(key))
	id := "file_" + hex.EncodeToString(sum[:16])
	existing, err := s.File(ctx, id)
	if err == nil {
		hash := sha256.Sum256(m.Data)
		if existing.Original.SHA256 != hex.EncodeToString(hash[:]) || existing.Thumbnail == nil {
			return d.File{}, fault.ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return d.File{}, err
	}
	return s.save(ctx, id, Input{Filename: "generated-image", ContentType: m.ContentType, Data: m.Data, Thumbnail: &ports.ThumbnailOptions{}}, true)
}

func (s *Service) save(ctx context.Context, id string, in Input, deterministic bool) (d.File, error) {
	var file d.File
	if s.owner != nil && *s.owner == "" {
		return file, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	if err := ctx.Err(); err != nil {
		return file, err
	}
	if int64(len(in.Data)) > d.MaxFileBytes {
		return file, fault.New("REQUEST_TOO_LARGE", "上传文件上限 128 MiB")
	}
	filename := path.Base(strings.ReplaceAll(in.Filename, "\\", "/"))
	if filename == "" || filename == "." || filename == ".." || filename == "/" || len(filename) > 1024 || strings.ContainsAny(filename, "\x00\r\n") {
		return file, fault.ErrInvalid
	}
	if s == nil || s.Store == nil || s.Objects == nil {
		return file, fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
	}
	// 嗅探已知格式；声明的 MIME 仅用于无法识别的二进制格式。
	kind := s.Objects.DetectContentType(in.Data)
	if kind == "application/octet-stream" && in.ContentType != "" {
		if declared, _, err := mime.ParseMediaType(in.ContentType); err == nil {
			kind = declared
		}
	}
	var thumb ports.ThumbnailImage
	if in.Thumbnail != nil {
		options := *in.Thumbnail
		if options.MaxWidth == 0 {
			options.MaxWidth = 320
		}
		if options.MaxHeight == 0 {
			options.MaxHeight = 320
		}
		if options.MaxWidth < 1 || options.MaxHeight < 1 || options.MaxWidth > 4096 || options.MaxHeight > 4096 {
			return file, fault.ErrInvalid
		}
		if s.Thumbnails == nil {
			return file, fault.New("THUMBNAIL_UNAVAILABLE", "图片缩略图工具不可用")
		}
		var err error
		thumb, err = s.Thumbnails.Generate(ctx, in.Data, options)
		if err != nil {
			return file, err
		}
		if len(thumb.Data) == 0 || s.Objects.DetectContentType(thumb.Data) != "image/webp" || thumb.Width < 1 || thumb.Height < 1 || thumb.Width > options.MaxWidth || thumb.Height > options.MaxHeight {
			return file, fault.New("THUMBNAIL_FAILED", "缩略图输出核对失败")
		}
	}
	file = d.File{ID: id, Filename: filename, CreatedAt: time.Now().UTC()}
	if s.owner != nil {
		file.OwnerID = *s.owner
	}
	var err error
	file.Original, err = s.Objects.PutFile(ctx, file.ID, "original", kind, in.Data)
	if err != nil {
		if deterministic {
			return file, err
		}
		return file, s.rollback(ctx, file, err)
	}
	if in.Thumbnail != nil {
		file.Thumbnail = &d.Thumbnail{Width: thumb.Width, Height: thumb.Height}
		file.Thumbnail.Blob, err = s.Objects.PutFile(ctx, file.ID, "thumbnail.webp", "image/webp", thumb.Data)
		if err != nil {
			if deterministic {
				return file, err
			}
			return file, s.rollback(ctx, file, err)
		}
	}
	if err = ctx.Err(); err != nil {
		if deterministic {
			return file, err
		}
		return file, s.rollback(ctx, file, err)
	}
	if err = s.Store.CreateUploadedFile(ctx, file); err != nil {
		if deterministic {
			existing, checkErr := s.File(ctx, id)
			if checkErr == nil && existing.Original.SHA256 == file.Original.SHA256 && existing.Thumbnail != nil {
				return existing, nil
			}
			return file, err
		}
		// PG 响应丢失不等于 INSERT 失败；已提交或结果未知都保留对象，
		// 只补偿确认缺失的记录。
		check, cancel := s.afterCancel(ctx, s.CommitCheckTimeout)
		defer cancel()
		_, readErr := s.Store.UploadedFile(check, file.ID)
		if errors.Is(readErr, fault.ErrNotFound) {
			if deterministic {
				return file, err
			}
			return file, s.rollback(ctx, file, err)
		}
		if readErr != nil {
			return file, errors.Join(err, fmt.Errorf("文件提交结果未知，保留对象: %w", readErr))
		}
		return file, err
	}
	return file, nil
}

func (s *Service) rollback(ctx context.Context, file d.File, cause error) error {
	cleanup, cancel := s.afterCancel(ctx, s.CleanupTimeout)
	defer cancel()
	for _, blob := range []d.Blob{file.Original, thumbnailBlob(file)} {
		if blob.ObjectKey == "" {
			continue
		}
		if err := s.Objects.RemoveFile(cleanup, file.ID, blob); err != nil {
			cause = errors.Join(cause, fmt.Errorf("本次文件对象清理未确认: %w", err))
		}
	}
	return cause
}

func thumbnailBlob(file d.File) d.Blob {
	if file.Thumbnail != nil {
		return file.Thumbnail.Blob
	}
	return d.Blob{}
}

func (s *Service) File(ctx context.Context, id string) (d.File, error) {
	if !d.ValidID(id) {
		return d.File{}, fault.ErrNotFound
	}
	if s == nil || s.Store == nil {
		return d.File{}, fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
	}
	return s.Store.UploadedFile(ctx, id)
}

func (s *Service) ReadOriginal(ctx context.Context, id string) (d.File, []byte, error) {
	return s.Read(ctx, id, false)
}

func (s *Service) Read(ctx context.Context, id string, thumbnail bool) (d.File, []byte, error) {
	file, err := s.File(ctx, id)
	if err != nil {
		return file, nil, err
	}
	blob := file.Original
	if thumbnail {
		if file.Thumbnail == nil {
			return file, nil, fault.ErrNotFound
		}
		blob = file.Thumbnail.Blob
	}
	if s.Objects == nil {
		return file, nil, fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
	}
	raw, err := s.Objects.ReadFile(ctx, id, blob)
	return file, raw, err
}

func (s *Service) afterCancel(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(context.WithoutCancel(ctx), timeout)
	}
	return context.WithCancel(context.WithoutCancel(ctx))
}

func (s *Service) ForActor(a ports.Actor) *Service {
	owner := ""
	if a.Require() == nil {
		owner = a.UserID
	}
	return s.owned(owner)
}
func (s *Service) ForFileOwner(owner string) ports.FileReader { return s.owned(owner) }
func (s *Service) owned(owner string) *Service {
	if s == nil {
		s = &Service{}
	}
	copy := *s
	copy.owner = &owner
	if store, ok := s.Store.(ports.OwnedFiles); ok {
		copy.Store = store.ForUploadedFileOwner(owner)
	} else {
		copy.Store = deniedFiles{}
	}
	return &copy
}

type deniedFiles struct{}

func (deniedFiles) CreateUploadedFile(context.Context, d.File) error {
	return fault.New("AUTH_REQUIRED", "文件归属存储不可用")
}
func (deniedFiles) UploadedFile(context.Context, string) (d.File, error) {
	return d.File{}, fault.New("AUTH_REQUIRED", "文件归属存储不可用")
}

func (s *Service) ForGeneratedOwner(owner string) ports.GeneratedFiles { return s.owned(owner) }
