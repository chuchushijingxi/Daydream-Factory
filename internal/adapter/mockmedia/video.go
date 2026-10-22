package mockmedia

import (
	"context"

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
	if g.Encoder == nil {
		return ports.MediaResult{}, fault.New("MOCK_ENCODER_UNAVAILABLE", "本地 Mock 视频需要 ffmpeg 和 libx264 编码器")
	}
	raw, err := g.Encoder.EncodeVideo(ctx, sampleVideo, width, height, options.DurationSeconds)
	if err != nil {
		return ports.MediaResult{}, err
	}
	return ports.MediaResult{Data: raw, ContentType: "video/mp4", Width: width, Height: height, DurationSeconds: options.DurationSeconds}, nil
}
