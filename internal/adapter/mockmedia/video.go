package mockmedia

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
)

// Re-encode the local sample, rather than label the same file with new metadata.
// This is an adapter implementation detail; it adds no task or outbound provider.
func (g Generator) video(ctx context.Context, options md.MediaOptions) (ports.MediaResult, error) {
	width, height := 1920, 1080
	switch options.Resolution {
	case "480p":
		width, height = 854, 480
	case "720p":
		width, height = 1280, 720
	}
	path := g.Settings.FFmpeg
	if path == "" {
		path = appconfig.Defaults().Media.FFmpeg
	}
	encoder, err := exec.LookPath(path)
	if err != nil {
		return ports.MediaResult{}, fault.New("MOCK_ENCODER_UNAVAILABLE", "本地 Mock 视频需要 ffmpeg 和 libx264 编码器")
	}
	dir, err := os.MkdirTemp("", "drama-mock-video-*")
	if err != nil {
		return ports.MediaResult{}, err
	}
	defer os.RemoveAll(dir)
	input, output := filepath.Join(dir, "source.mp4"), filepath.Join(dir, "output.mp4")
	if err = os.WriteFile(input, sampleVideo, 0600); err != nil {
		return ports.MediaResult{}, err
	}
	c, cancel := context.WithTimeout(ctx, appconfig.MockVideoEncodeTimeout)
	defer cancel()
	filter := "fps=30,scale=" + strconv.Itoa(width) + ":" + strconv.Itoa(height) + ":flags=bilinear,setsar=1"
	cmd := exec.CommandContext(c, encoder, "-nostdin", "-v", "error", "-i", input,
		"-map", "0:v:0", "-an", "-vf", filter, "-t", strconv.FormatFloat(options.DurationSeconds, 'f', 0, 64),
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "28", "-pix_fmt", "yuv420p", "-threads", "2", "-movflags", "+faststart", output)
	if err = cmd.Run(); err != nil {
		if c.Err() != nil {
			return ports.MediaResult{}, c.Err()
		}
		return ports.MediaResult{}, fault.New("MOCK_ENCODING_FAILED", "本地 Mock 视频编码失败；请检查 ffmpeg 和 libx264")
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		return ports.MediaResult{}, err
	}
	if len(raw) == 0 || len(raw) > 128<<20 {
		return ports.MediaResult{}, fault.New("MOCK_ENCODING_FAILED", "本地 Mock 视频编码结果大小无效")
	}
	if err = c.Err(); err != nil {
		return ports.MediaResult{}, err
	}
	return ports.MediaResult{Data: raw, ContentType: "video/mp4", Width: width, Height: height, DurationSeconds: options.DurationSeconds}, nil
}
