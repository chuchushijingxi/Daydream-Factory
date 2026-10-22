// Package mockmedia adapts spark-playlet's local JPEG and embedded MP4 mocks.
// It makes no network requests and never substitutes an external placeholder.
package mockmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"image"
	"image/color"
	"image/jpeg"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
)

//go:embed assets/sample.mp4
var sampleVideo []byte

// VideoEncoder 把示例视频重新编码为请求的分辨率与时长；由本机 ffmpeg 或独立媒体服务实现。
type VideoEncoder interface {
	EncodeVideo(ctx context.Context, input []byte, width, height int, seconds float64) ([]byte, error)
}

type Generator struct{ Encoder VideoEncoder }

func (g Generator) Generate(ctx context.Context, in ports.MediaRequest) (ports.MediaResult, error) {
	if err := ctx.Err(); err != nil {
		return ports.MediaResult{}, err
	}

	profile := md.OfficialMediaProfile("mock", in.ModelID)
	options := md.MediaOptions{Resolution: in.Resolution, DurationSeconds: in.DurationSeconds}
	if options.Resolution == "" && options.DurationSeconds == 0 && profile != nil {
		options = profile.Defaults()
	}
	if !profile.Supports(options) {
		return ports.MediaResult{}, fault.ErrInvalid
	}
	switch in.ModelID {
	case md.MockVideoModelID:
		if in.AspectRatio != "" && in.AspectRatio != "16:9" {
			return ports.MediaResult{}, fault.ErrInvalid
		}
		return g.video(ctx, options)
	case md.MockImageModelID:
		width, height, ok := imageSize(in.AspectRatio)
		if !ok {
			return ports.MediaResult{}, fault.ErrInvalid
		}
		if options.Resolution == "2K" {
			width *= 2
			height *= 2
		}
		if options.Resolution == "4K" {
			width *= 4
			height *= 4
		}
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		seed := sha256.Sum256([]byte(in.Prompt))
		for y := 0; y < height; y++ {
			if err := ctx.Err(); err != nil {
				return ports.MediaResult{}, err
			}
			for x := 0; x < width; x++ {
				shade := uint8(((x / 64) + (y / 64)) % 2 * 24)
				img.SetRGBA(x, y, color.RGBA{R: 48 + seed[0]/2 + shade, G: 48 + seed[1]/2 + shade, B: 48 + seed[2]/2 + shade, A: 255})
			}
		}
		var data bytes.Buffer
		if err := jpeg.Encode(&data, img, &jpeg.Options{Quality: 85}); err != nil {
			return ports.MediaResult{}, err
		}
		if err := ctx.Err(); err != nil {
			return ports.MediaResult{}, err
		}
		return ports.MediaResult{Data: data.Bytes(), ContentType: "image/jpeg", Width: width, Height: height}, nil
	default:
		return ports.MediaResult{}, fault.New("MOCK_MODEL_REQUIRED", "此测试入口只支持内置 Mock 图片和视频模型")
	}
}

func imageSize(ratio string) (int, int, bool) {
	switch ratio {
	case "", "4:3":
		return 1024, 768, true
	case "1:1":
		return 1024, 1024, true
	case "2:3":
		return 768, 1152, true
	case "3:2":
		return 1152, 768, true
	case "3:4":
		return 768, 1024, true
	case "9:16":
		return 720, 1280, true
	case "16:9":
		return 1280, 720, true
	case "21:9":
		return 1344, 576, true
	default:
		return 0, 0, false
	}
}
