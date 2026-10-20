// Package imagethumbnail creates a first-frame WebP preview from local image bytes.
// Based on spark-playlet's FFmpeg/libwebp approach; no remote URL fetch or storage.
package imagethumbnail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

type FFmpeg struct{ Settings appconfig.Media }

func (f FFmpeg) Generate(ctx context.Context, raw []byte, options ports.ThumbnailOptions) (ports.ThumbnailImage, error) {
	var out ports.ThumbnailImage
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if options.MaxWidth < 1 || options.MaxHeight < 1 || options.MaxWidth > 4096 || options.MaxHeight > 4096 {
		return out, fault.ErrInvalid
	}
	kind := http.DetectContentType(raw)
	switch kind {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp", "image/x-icon", "video/mp4":
	default:
		return out, fault.New("THUMBNAIL_UNSUPPORTED", "缩略图支持 JPEG、PNG、GIF、WebP、BMP、ICO 图片及 MP4 视频；其他文件可直接上传原件")
	}
	encoder, err := exec.LookPath(f.Settings.FFmpeg)
	if err != nil {
		return out, unavailable()
	}
	probe, err := exec.LookPath(f.Settings.FFprobe)
	if err != nil {
		return out, unavailable()
	}
	ctx, cancel := context.WithTimeout(ctx, f.Settings.ThumbnailTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "drama-thumbnail-*")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(dir)
	input, output := filepath.Join(dir, "input"), filepath.Join(dir, "thumbnail.webp")
	if err = os.WriteFile(input, raw, 0600); err != nil {
		return out, err
	}
	width, height, err := dimensions(ctx, probe, input)
	if err != nil {
		return out, err
	}
	if width < 1 || height < 1 || width > 32768 || height > 32768 || int64(width)*int64(height) > 40_000_000 {
		return out, fault.New("INVALID_IMAGE", "缩略图源图片尺寸超出处理上限（4000万像素、单边32768）")
	}
	if kind == "image/webp" {
		// 本机 FFmpeg 解码器对单像素宽的 WebP 会丢失 alpha；
		// 该格式先用原生 libwebp 解码，再进入共享缩放管线。
		decoder, err := exec.LookPath(f.Settings.DWebP)
		if err != nil {
			return out, fault.New("THUMBNAIL_UNAVAILABLE", "WebP 输入缩略图需要本机 dwebp")
		}
		decoded := filepath.Join(dir, "decoded.png")
		if err = exec.CommandContext(ctx, decoder, "-quiet", input, "-o", decoded).Run(); err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			return out, fault.New("INVALID_IMAGE", "无法解码静态 WebP 图片")
		}
		input = decoded
	}
	// iw/ih refer to the oriented frame. Bound both sides without upscaling,
	// and keep a positive dimension even for very thin images.
	factor := fmt.Sprintf("min(1,min(%d/iw,%d/ih))", options.MaxWidth, options.MaxHeight)
	scale := "scale=w='max(1,floor(iw*" + factor + "))':h='max(1,floor(ih*" + factor + "))':flags=lanczos"
	codec, err := webpEncoder(ctx, encoder)
	if err != nil {
		return out, err
	}
	target, codecOptions := output, []string{"-c:v", codec, "-quality", "82"}
	var cwebp string
	if codec == "" {
		cwebp, err = exec.LookPath(f.Settings.CWebP)
		if err != nil {
			return out, unavailable()
		}
		target, codecOptions = filepath.Join(dir, "scaled.png"), []string{"-c:v", "png"}
	}
	args := []string{"-v", "error", "-nostdin", "-protocol_whitelist", "file,pipe", "-i", input, "-vf", scale, "-frames:v", "1"}
	args = append(args, codecOptions...)
	args = append(args, "-threads", "1", "-y", target)
	if err = exec.CommandContext(ctx, encoder, args...).Run(); err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, fault.New("THUMBNAIL_FAILED", "FFmpeg 图片缩略图处理失败")
	}
	if cwebp != "" {
		if err = exec.CommandContext(ctx, cwebp, "-quiet", "-q", "82", target, "-o", output).Run(); err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			return out, fault.New("THUMBNAIL_FAILED", "cwebp 缩略图编码失败")
		}
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() <= 0 || info.Size() > 128<<20 {
		return out, fault.New("THUMBNAIL_FAILED", "缩略图文件无效")
	}
	out.Data, err = os.ReadFile(output)
	if err != nil {
		return out, err
	}
	out.Width, out.Height, err = dimensions(ctx, probe, output)
	if err != nil {
		return out, err
	}
	if out.Width > options.MaxWidth || out.Height > options.MaxHeight || http.DetectContentType(out.Data) != "image/webp" {
		return out, fault.New("THUMBNAIL_FAILED", "缩略图格式或实际尺寸核对失败")
	}
	return out, nil
}

func unavailable() error {
	return fault.New("THUMBNAIL_UNAVAILABLE", "缩略图需要本机 ffmpeg、ffprobe，以及 FFmpeg WebP 编码器或 cwebp")
}

func webpEncoder(ctx context.Context, ffmpeg string) (string, error) {
	raw, err := exec.CommandContext(ctx, ffmpeg, "-v", "quiet", "-encoders").Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", unavailable()
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && (fields[1] == "libwebp" || fields[1] == "webp") {
			return fields[1], nil
		}
	}
	return "", nil
}

func dimensions(ctx context.Context, probe, filename string) (int, int, error) {
	cmd := exec.CommandContext(ctx, probe, "-v", "error", "-protocol_whitelist", "file,pipe", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "json", filename)
	raw, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return 0, 0, ctx.Err()
		}
		return 0, 0, fault.New("INVALID_IMAGE", "无法解码图片")
	}
	var result struct{ Streams []struct{ Width, Height int } }
	if json.Unmarshal(raw, &result) != nil || len(result.Streams) != 1 || result.Streams[0].Width < 1 || result.Streams[0].Height < 1 {
		return 0, 0, fault.New("INVALID_IMAGE", "图片尺寸无效")
	}
	return result.Streams[0].Width, result.Streams[0].Height, nil
}
