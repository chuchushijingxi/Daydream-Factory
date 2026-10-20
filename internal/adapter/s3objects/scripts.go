// Package s3objects implements the standard S3 object protocol, including MinIO.
// Vendor-specific credentials and endpoints stay outside application/domain.
package s3objects

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client   *minio.Client
	bucket   string
	region   string
	settings appconfig.Storage
}

var _ ports.OriginalScripts = (*Store)(nil)

func New(cfg appconfig.Storage) (*Store, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || (u.Path != "" && u.Path != "/") || u.Fragment != "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("invalid S3 configuration")
	}
	lookup := minio.BucketLookupDNS
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	options := cfg
	transport.ResponseHeaderTimeout = options.HeaderTimeout
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken), Secure: u.Scheme == "https", Region: cfg.Region, BucketLookup: lookup, Transport: transport})
	if err != nil {
		return nil, fmt.Errorf("invalid S3 configuration")
	}
	return &Store{client: client, bucket: cfg.Bucket, region: cfg.Region, settings: options}, nil
}

// EnsureBucket 只使用标准 S3 API；默认 ACL 即私有。
func (s *Store) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return unavailable()
	}
	if !exists {
		if err = s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region}); err != nil {
			return unavailable()
		}
	}
	return nil
}

func unavailable() error {
	return fault.New("SCRIPT_STORAGE_UNAVAILABLE", "S3 原剧本存储不可用")
}

func (s *Store) valid(projectID string, file d.OriginalFile) bool {
	parts := strings.Split(file.ObjectKey, "/")
	if len(parts) != 5 || parts[0] != "projects" || parts[1] != projectID || parts[2] != "scripts" || !strings.HasPrefix(parts[4], "original_") || strings.ContainsAny(file.ObjectKey, "\\") {
		return false
	}
	version, err := strconv.ParseUint(parts[3], 10, 64)
	return err == nil && version > 0 && projectID != "" && projectID != "." && projectID != ".." && !strings.ContainsAny(projectID, "/\\") && file.Bucket == s.bucket && file.SizeBytes > 0 && file.SizeBytes <= d.MaxScriptBytes
}

func (s *Store) Read(ctx context.Context, projectID string, file d.OriginalFile) ([]byte, error) {
	ctx, cancel := s.withTimeout(ctx, s.settings.ReadTimeout)
	defer cancel()
	if !s.valid(projectID, file) {
		return nil, fault.ErrNotFound
	}
	object, err := s.client.GetObject(ctx, file.Bucket, file.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, unavailable()
	}
	defer object.Close()
	raw, err := io.ReadAll(io.LimitReader(object, d.MaxScriptBytes+1))
	if err != nil {
		return nil, unavailable()
	}
	if int64(len(raw)) != file.SizeBytes {
		return nil, fault.New("SCRIPT_FILE_CORRUPT", "原剧本文件长度不符")
	}
	return raw, nil
}

func (s *Store) withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}
