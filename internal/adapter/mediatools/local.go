// Package mediatools runs the media command-line tools (FFmpeg, ffprobe, libwebp):
// either in-process on this machine (Local) or in the separate media service
// (Remote). Callers depend on Tools and never on where the tools run.
package mediatools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/imagethumbnail"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

// Tools 是缩略图、视频核对与示例视频转码的统一入口。
type Tools interface {
	ports.ImageThumbnailer
	ProbeVideo(ctx context.Context, raw []byte) (width, height int, seconds float64, err error)
	EncodeVideo(ctx context.Context, input []byte, width, height int, seconds float64) ([]byte, error)
}

// New 配置了媒体服务地址时走独立服务；未配置时（主机上直接运行后端）调用本机工具。
func New(settings appconfig.Media) Tools {
	if endpoint := strings.TrimRight(settings.Endpoint, "/"); endpoint != "" {
		return NewRemote(endpoint)
	}
	return Local{Settings: settings}
}

// MaxMediaBytes 与上传、任务结果的单文件上限一致。
const MaxMediaBytes = 128 << 20

// Local 在当前进程所在机器上调用媒体工具；媒体服务内部也使用它。
type Local struct{ Settings appconfig.Media }

func (l Local) Generate(ctx context.Context, raw []byte, options ports.ThumbnailOptions) (ports.ThumbnailImage, error) {
	return imagethumbnail.FFmpeg{Settings: l.Settings}.Generate(ctx, raw, options)
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

// ProbeVideo 用 ffprobe 读取完整 MP4 的实际宽高与时长。
func (l Local) ProbeVideo(ctx context.Context, raw []byte) (int, int, float64, error) {
	inspector, err := exec.LookPath(l.Settings.FFprobe)
	if err != nil {
		return 0, 0, 0, fault.New("TASK_MEDIA_INSPECTOR_UNAVAILABLE", "视频核对工具 ffprobe 不可用")
	}
	if len(raw) == 0 || len(raw) > MaxMediaBytes {
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
	c, cancel := withTimeout(ctx, l.Settings.ProbeTimeout)
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

// EncodeVideo 把输入视频按目标宽高与时长重新编码为 H.264 MP4（本地 Mock 视频使用）。
func (l Local) EncodeVideo(ctx context.Context, input []byte, width, height int, seconds float64) ([]byte, error) {
	if len(input) == 0 || len(input) > MaxMediaBytes || width < 1 || height < 1 || width > 7680 || height > 7680 || seconds <= 0 || seconds > 600 {
		return nil, fault.ErrInvalid
	}
	path := l.Settings.FFmpeg
	if path == "" {
		path = appconfig.Defaults().Media.FFmpeg
	}
	encoder, err := exec.LookPath(path)
	if err != nil {
		return nil, fault.New("MOCK_ENCODER_UNAVAILABLE", "本地 Mock 视频需要 ffmpeg 和 libx264 编码器")
	}
	dir, err := os.MkdirTemp("", "drama-mock-video-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	source, output := filepath.Join(dir, "source.mp4"), filepath.Join(dir, "output.mp4")
	if err = os.WriteFile(source, input, 0600); err != nil {
		return nil, err
	}
	c, cancel := context.WithTimeout(ctx, appconfig.MockVideoEncodeTimeout)
	defer cancel()
	filter := "fps=30,scale=" + strconv.Itoa(width) + ":" + strconv.Itoa(height) + ":flags=bilinear,setsar=1"
	cmd := exec.CommandContext(c, encoder, "-nostdin", "-v", "error", "-i", source,
		"-map", "0:v:0", "-an", "-vf", filter, "-t", strconv.FormatFloat(seconds, 'f', 0, 64),
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "28", "-pix_fmt", "yuv420p", "-threads", "2", "-movflags", "+faststart", output)
	if err = cmd.Run(); err != nil {
		if c.Err() != nil {
			return nil, c.Err()
		}
		return nil, fault.New("MOCK_ENCODING_FAILED", "本地 Mock 视频编码失败；请检查 ffmpeg 和 libx264")
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > MaxMediaBytes {
		return nil, fault.New("MOCK_ENCODING_FAILED", "本地 Mock 视频编码结果大小无效")
	}
	if err = c.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
