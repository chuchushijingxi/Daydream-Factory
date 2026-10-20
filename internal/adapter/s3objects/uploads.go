package s3objects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/upload"
	"github.com/minio/minio-go/v7"
)

func fileUnavailable() error {
	return fault.New("FILE_STORAGE_UNAVAILABLE", "S3 文件存储不可用")
}

func (s *Store) PutFile(ctx context.Context, id, part, kind string, raw []byte) (d.Blob, error) {
	sum := sha256.Sum256(raw)
	blob := d.Blob{Bucket: s.bucket, ObjectKey: d.ObjectKey(id, part), SizeBytes: int64(len(raw)), ContentType: kind, SHA256: hex.EncodeToString(sum[:])}
	if !blob.Valid(id) {
		return d.Blob{}, fault.ErrInvalid
	}
	ctx, cancel := s.withTimeout(ctx, s.settings.UploadTimeout)
	defer cancel()
	saved, err := s.client.PutObject(ctx, blob.Bucket, blob.ObjectKey, bytes.NewReader(raw), blob.SizeBytes, minio.PutObjectOptions{ContentType: kind, DisableMultipart: true, SendContentMd5: true, DisableContentSha256: true})
	if err != nil || saved.Size != blob.SizeBytes {
		return blob, fileUnavailable()
	}
	if _, err = s.ReadFile(ctx, id, blob); err != nil {
		return blob, err
	}
	return blob, nil
}

func (s *Store) ReadFile(ctx context.Context, id string, blob d.Blob) ([]byte, error) {
	if !blob.Valid(id) || blob.Bucket != s.bucket {
		return nil, fault.ErrNotFound
	}
	ctx, cancel := s.withTimeout(ctx, s.settings.ReadTimeout)
	defer cancel()
	object, err := s.client.GetObject(ctx, blob.Bucket, blob.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fileUnavailable()
	}
	defer object.Close()
	raw, err := io.ReadAll(io.LimitReader(object, blob.SizeBytes+1))
	if err != nil {
		return nil, fileUnavailable()
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != blob.SizeBytes || hex.EncodeToString(sum[:]) != blob.SHA256 {
		return nil, fault.New("FILE_CORRUPT", "文件长度或 SHA256 校验失败")
	}
	return raw, nil
}

func (s *Store) RemoveFile(ctx context.Context, id string, blob d.Blob) error {
	if !blob.Valid(id) || blob.Bucket != s.bucket {
		return fault.ErrNotFound
	}
	if err := s.client.RemoveObject(ctx, blob.Bucket, blob.ObjectKey, minio.RemoveObjectOptions{}); err != nil {
		return fileUnavailable()
	}
	return nil
}

// DetectContentType 把传输层 MIME 识别留在文件对象端口之后。
func (s *Store) DetectContentType(raw []byte) string { return http.DetectContentType(raw) }
