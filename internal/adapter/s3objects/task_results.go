package s3objects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/task"
	"github.com/minio/minio-go/v7"
)

type TaskResults struct {
	ReadOnly     bool
	Thumbnails   ports.ImageThumbnailer
	Owners       ports.TaskOwners
	Files        ports.GeneratedFiles
	Objects      *Store
	ProbePath    string
	ProbeTimeout time.Duration
}

var _ ports.TaskResultStore = (*TaskResults)(nil)

func component(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func (s *TaskResults) SaveTaskRaw(ctx context.Context, id, key string, raw []byte) (d.Blob, error) {
	return s.put(ctx, id, key, raw, "application/json")
}
func (s *TaskResults) put(ctx context.Context, id, key string, raw []byte, contentType string) (d.Blob, error) {
	if !component(id) || !component(key) || len(raw) == 0 || len(raw) > 128<<20 {
		return d.Blob{}, fault.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	blob := d.Blob{Bucket: s.Objects.bucket, ObjectKey: "tasks/" + id + "/" + key + "/" + hash, SizeBytes: int64(len(raw)), SHA256: hash}
	c, cancel := s.Objects.withTimeout(ctx, s.Objects.settings.TaskWriteTimeout)
	defer cancel()
	saved, err := s.Objects.client.PutObject(c, blob.Bucket, blob.ObjectKey, bytes.NewReader(raw), int64(len(raw)), minio.PutObjectOptions{ContentType: contentType, DisableMultipart: true, SendContentMd5: true, DisableContentSha256: true})
	if err != nil || saved.Size != int64(len(raw)) {
		return d.Blob{}, fault.New("TASK_STORAGE_FAILED", "任务对象保存未确认")
	}
	body, err := s.ReadTaskRaw(c, id, blob)
	if err != nil || !bytes.Equal(raw, body) {
		return d.Blob{}, fault.New("TASK_STORAGE_FAILED", "任务对象读回核对失败")
	}
	return blob, nil
}
func (s *TaskResults) ReadTaskRaw(ctx context.Context, id string, b d.Blob) ([]byte, error) {
	ctx, cancel := s.Objects.withTimeout(ctx, s.Objects.settings.ReadTimeout)
	defer cancel()
	parts := strings.Split(b.ObjectKey, "/")
	if !component(id) || len(parts) != 4 || parts[0] != "tasks" || parts[1] != id || !component(parts[2]) || parts[3] != b.SHA256 || len(b.SHA256) != 64 || b.Bucket != s.Objects.bucket || b.SizeBytes <= 0 || b.SizeBytes > 128<<20 {
		return nil, fault.ErrNotFound
	}
	obj, err := s.Objects.client.GetObject(ctx, b.Bucket, b.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	raw, err := io.ReadAll(io.LimitReader(obj, b.SizeBytes+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != b.SizeBytes || hex.EncodeToString(sum[:]) != b.SHA256 {
		return nil, fault.New("TASK_STORAGE_FAILED", "任务文件长度或散列不符")
	}
	return raw, nil
}
func (s *TaskResults) ReadTaskOutput(ctx context.Context, id string, out d.Output) ([]byte, error) {
	return s.ReadTaskRaw(ctx, id, out.Blob)
}
func (s *TaskResults) SaveTaskOutput(ctx context.Context, id, mediaID string, m ports.MediaResult) (d.Output, error) {
	out := d.Output{ID: mediaID, ContentType: m.ContentType}
	switch m.ContentType {
	case "image/jpeg", "image/png":
		config, format, err := image.DecodeConfig(bytes.NewReader(m.Data))
		if err != nil || (format != "jpeg" && format != "png") || (m.ContentType == "image/jpeg" && format != "jpeg") || (m.ContentType == "image/png" && format != "png") || int64(config.Width)*int64(config.Height) > 64<<20 {
			return out, fault.New("TASK_RESULT_INVALID", "图片文件格式无效")
		}
		if _, _, err = image.Decode(bytes.NewReader(m.Data)); err != nil {
			return out, fault.New("TASK_RESULT_INVALID", "图片字节不完整")
		}
		out.Width = config.Width
		out.Height = config.Height
	case "video/mp4":
		width, height, duration, err := s.probeVideo(ctx, m.Data)
		if err != nil {
			return out, err
		}
		out.Width = width
		out.Height = height
		out.DurationSeconds = duration
	default:
		return out, fault.New("TASK_RESULT_INVALID", "不支持的媒体结果格式")
	}
	if out.Width <= 0 || out.Height <= 0 || (m.Width > 0 && m.Width != out.Width) || (m.Height > 0 && m.Height != out.Height) {
		return out, fault.New("TASK_RESULT_INVALID", "实际媒体尺寸与回执不符")
	}
	var err error
	out.Blob, err = s.put(ctx, id, mediaID, m.Data, m.ContentType)
	if err == nil && strings.HasPrefix(m.ContentType, "image/") && s.Files != nil {
		files := s.Files
		if s.Owners != nil {
			owner, e := s.Owners.TaskOwner(ctx, id)
			if e != nil {
				return out, e
			}
			if owner != "" {
				owned, ok := files.(ports.OwnedGeneratedFiles)
				if !ok {
					return out, fault.ErrInvalid
				}
				files = owned.ForGeneratedOwner(owner)
			}
		}
		file, e := files.ImportGenerated(ctx, id+":"+mediaID, m)
		if e != nil {
			return out, e
		}
		out.FileID = file.ID
	}
	return out, err
}
func (s *TaskResults) probeVideo(ctx context.Context, raw []byte) (int, int, float64, error) {
	inspector, err := exec.LookPath(s.ProbePath)
	if err != nil {
		return 0, 0, 0, fault.New("TASK_MEDIA_INSPECTOR_UNAVAILABLE", "视频核对工具 ffprobe 不可用")
	}
	if len(raw) == 0 || len(raw) > 128<<20 {
		return 0, 0, 0, fault.New("TASK_RESULT_INVALID", "视频文件大小无效")
	}
	f, err := os.CreateTemp("", "drama-task-video-*.mp4")
	if err != nil {
		return 0, 0, 0, err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return 0, 0, 0, err
	}
	if err = f.Close(); err != nil {
		return 0, 0, 0, err
	}
	c, cancel := s.Objects.withTimeout(ctx, s.ProbeTimeout)
	defer cancel()
	body, err := exec.CommandContext(c, inspector, "-v", "error", "-show_entries", "stream=width,height,codec_type:format=duration", "-of", "json", name).Output()
	if err != nil {
		return 0, 0, 0, fault.New("TASK_RESULT_INVALID", "无法核对实际视频；需要完整MP4")
	}
	var p struct {
		Streams []struct {
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Kind   string `json:"codec_type"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(body, &p) != nil {
		return 0, 0, 0, fmt.Errorf("invalid video probe response")
	}
	seconds, err := strconv.ParseFloat(p.Format.Duration, 64)
	if err != nil || seconds <= 0 || math.IsInf(seconds, 0) || math.IsNaN(seconds) {
		return 0, 0, 0, fault.New("TASK_RESULT_INVALID", "视频实际时长无效")
	}
	for _, stream := range p.Streams {
		if stream.Kind == "video" && stream.Width > 0 && stream.Height > 0 {
			return stream.Width, stream.Height, seconds, nil
		}
	}
	return 0, 0, 0, fault.New("TASK_RESULT_INVALID", "视频缺少有效视频流")
}
