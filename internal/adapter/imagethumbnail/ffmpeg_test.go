package imagethumbnail

import (
	"bytes"
	"context"
	testconfig "drama-agent/internal/testsupport/config"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"drama-agent/internal/application/ports"
)

func requireTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe", "dwebp"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("local image tools unavailable")
		}
	}
}

func TestActualWebPBoundsTransparencyAndFirstFrame(t *testing.T) {
	requireTools(t)
	for _, size := range []struct{ w, h, tw, th int }{{640, 400, 320, 200}, {100, 600, 53, 320}, {20, 10, 20, 10}, {1, 1000, 1, 320}} {
		im := image.NewNRGBA(image.Rect(0, 0, size.w, size.h))
		for y := 0; y < size.h; y++ {
			for x := 0; x < size.w; x++ {
				im.SetNRGBA(x, y, color.NRGBA{R: 200, G: 30, B: 20, A: 120})
			}
		}
		var input bytes.Buffer
		if err := png.Encode(&input, im); err != nil {
			t.Fatal(err)
		}
		out, err := (FFmpeg{Settings: testconfig.Example().Media}).Generate(context.Background(), input.Bytes(), ports.ThumbnailOptions{MaxWidth: 320, MaxHeight: 320})
		if err != nil || out.Width != size.tw || out.Height != size.th || http.DetectContentType(out.Data) != "image/webp" {
			t.Fatal("actual thumbnail dimensions/format", size, out.Width, out.Height, err)
		}
		decoded := decodeWebP(t, out.Data)
		_, _, _, alpha := decoded.At(0, 0).RGBA()
		if alpha == 0 || alpha == 65535 {
			t.Fatal("thumbnail flattened transparency", size, alpha)
		}
		// The uploaded source may itself be WebP; verify native decoding before
		// resize also retains alpha, including the known thin-file decoder case.
		again, err := (FFmpeg{Settings: testconfig.Example().Media}).Generate(context.Background(), out.Data, ports.ThumbnailOptions{MaxWidth: 16, MaxHeight: 16})
		if err != nil || again.Width > 16 || again.Height > 16 {
			t.Fatal("WebP source resize failed", err)
		}
		_, _, _, alpha = decodeWebP(t, again.Data).At(0, 0).RGBA()
		if alpha == 0 || alpha == 65535 {
			t.Fatal("WebP input lost alpha", size, alpha)
		}
	}
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 640, 400)), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	jpegThumb, err := (FFmpeg{Settings: testconfig.Example().Media}).Generate(context.Background(), photo.Bytes(), ports.ThumbnailOptions{MaxWidth: 320, MaxHeight: 320})
	if err != nil || jpegThumb.Width != 320 || jpegThumb.Height != 200 {
		t.Fatal("JPEG source dimensions", err)
	}
	palette := color.Palette{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}}
	first := image.NewPaletted(image.Rect(0, 0, 20, 10), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	for i := range second.Pix {
		second.Pix[i] = 1
	}
	var input bytes.Buffer
	if err := gif.EncodeAll(&input, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	out, err := (FFmpeg{Settings: testconfig.Example().Media}).Generate(context.Background(), input.Bytes(), ports.ThumbnailOptions{MaxWidth: 320, MaxHeight: 320})
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ := decodeWebP(t, out.Data).At(0, 0).RGBA()
	if r <= b {
		t.Fatal("animated image did not use first frame")
	}
}

func decodeWebP(t *testing.T, raw []byte) image.Image {
	t.Helper()
	// Native libwebp also verifies alpha for one-pixel-wide files; this machine's
	// FFmpeg WebP decoder incorrectly returns fully transparent pixels for them.
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.webp"), filepath.Join(dir, "decoded.png")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("dwebp", "-quiet", input, "-o", output).Run(); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	return im
}

func TestNonImageCorruptionAndCancellationDoNotProduceThumbnail(t *testing.T) {
	requireTools(t)
	for _, raw := range [][]byte{[]byte("ordinary file"), []byte("\x89PNG\r\n\x1a\ncorrupt")} {
		if _, err := (FFmpeg{Settings: testconfig.Example().Media}).Generate(context.Background(), raw, ports.ThumbnailOptions{MaxWidth: 320, MaxHeight: 320}); err == nil {
			t.Fatal("non-image/corrupt image accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (FFmpeg{Settings: testconfig.Example().Media}).Generate(ctx, []byte("x"), ports.ThumbnailOptions{MaxWidth: 320, MaxHeight: 320}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored", err)
	}
}

func TestVideoThumbnailUsesActualFirstFrameIncludingOneSecondVideo(t *testing.T) {
	requireTools(t)
	filename := filepath.Join(t.TempDir(), "frames.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=640x360:d=0.5", "-f", "lavfi", "-i", "color=c=blue:s=640x360:d=0.5", "-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-y", filename)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("video fixture: %v %s", err, out)
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	thumb, err := (FFmpeg{Settings: testconfig.Example().Media}).Generate(context.Background(), raw, ports.ThumbnailOptions{MaxWidth: 320, MaxHeight: 320})
	if err != nil || thumb.Width != 320 || thumb.Height != 180 {
		t.Fatal("video cover dimensions", thumb.Width, thumb.Height, err)
	}
	r, _, b, _ := decodeWebP(t, thumb.Data).At(160, 90).RGBA()
	if r <= b {
		t.Fatal("cover did not capture the red first frame")
	}
}
