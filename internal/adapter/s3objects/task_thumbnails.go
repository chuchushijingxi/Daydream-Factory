package s3objects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/task"
	"github.com/minio/minio-go/v7"
)

var _ ports.TaskOutputThumbnails = (*TaskResults)(nil)

// The source hash makes this an immutable derivative of the saved video.
// Cache misses read verified private bytes; no external URL is accepted.
func (s *TaskResults) ReadTaskThumbnail(ctx context.Context, id string, out d.Output) ([]byte, error) {
	if !component(id) || !component(out.ID) || out.ContentType != "video/mp4" || len(out.Blob.SHA256) != 64 {
		return nil, fault.ErrNotFound
	}
	if _, err := hex.DecodeString(out.Blob.SHA256); err != nil {
		return nil, fault.ErrNotFound
	}
	key := "tasks/" + id + "/thumbnail_" + out.ID + "/" + out.Blob.SHA256
	if body, err := s.readThumbnailCache(ctx, key); err == nil {
		return body, nil
	} else if !errors.Is(err, fault.ErrNotFound) {
		return nil, err
	}
	if s.ReadOnly {
		return nil, fault.New("SERVICE_READ_ONLY", "当前服务只读，视频封面尚未缓存")
	}
	if s.Thumbnails == nil {
		return nil, fault.New("THUMBNAIL_UNAVAILABLE", "视频截帧工具不可用")
	}
	raw, err := s.ReadTaskOutput(ctx, id, out)
	if err != nil {
		return nil, err
	}
	thumb, err := s.Thumbnails.Generate(ctx, raw, ports.ThumbnailOptions{MaxWidth: 320, MaxHeight: 320})
	if err != nil {
		return nil, err
	}
	if len(thumb.Data) == 0 || len(thumb.Data) >= 1<<20 || thumb.Width < 1 || thumb.Height < 1 || thumb.Width > 320 || thumb.Height > 320 || http.DetectContentType(thumb.Data) != "image/webp" {
		return nil, fault.New("THUMBNAIL_FAILED", "视频截帧结果无效")
	}
	sum := sha256.Sum256(thumb.Data)
	w, finish := s.Objects.withTimeout(ctx, s.Objects.settings.TaskWriteTimeout)
	defer finish()
	saved, err := s.Objects.client.PutObject(w, s.Objects.bucket, key, bytes.NewReader(thumb.Data), int64(len(thumb.Data)), minio.PutObjectOptions{ContentType: "image/webp", UserMetadata: map[string]string{"sha256": hex.EncodeToString(sum[:])}, DisableMultipart: true, SendContentMd5: true, DisableContentSha256: true})
	if err != nil {
		return nil, err
	}
	if saved.Size != int64(len(thumb.Data)) {
		return nil, fault.New("TASK_STORAGE_FAILED", "视频封面保存未确认")
	}
	verified, err := s.readThumbnailCache(ctx, key)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(verified, thumb.Data) {
		return nil, fault.New("TASK_STORAGE_FAILED", "视频封面读回核对失败")
	}
	return verified, nil
}

func (s *TaskResults) readThumbnailCache(ctx context.Context, key string) ([]byte, error) {
	c, cancel := s.Objects.withTimeout(ctx, s.Objects.settings.ReadTimeout)
	defer cancel()
	obj, err := s.Objects.client.GetObject(c, s.Objects.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	info, err := obj.Stat()
	if err != nil {
		code := minio.ToErrorResponse(err).Code
		if code == "NoSuchKey" || code == "NoSuchObject" {
			return nil, fault.ErrNotFound
		}
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(obj, 1<<20))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if len(body) == 0 || len(body) >= 1<<20 || http.DetectContentType(body) != "image/webp" || info.Metadata.Get("X-Amz-Meta-Sha256") != hex.EncodeToString(sum[:]) {
		return nil, fault.New("TASK_STORAGE_FAILED", "视频封面缓存核对失败")
	}
	return body, nil
}
