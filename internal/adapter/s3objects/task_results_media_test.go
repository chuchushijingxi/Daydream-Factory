package s3objects

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProbeVideoUsesActualLocalMediaAndCleansTemporaryFiles(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("local media tools unavailable")
		}
	}
	fixture := filepath.Join(t.TempDir(), "actual.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=96x54:r=30:d=1",
		"-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-threads", "1", "-movflags", "+faststart", fixture)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create actual media fixture: %v: %s", err, output)
	}
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	// probeVideo only uses the Store timeout helper; no S3 client or endpoint is needed.
	results := TaskResults{Objects: &Store{}, ProbePath: "ffprobe", ProbeTimeout: 2 * time.Second}
	width, height, duration, err := results.probeVideo(context.Background(), raw)
	if err != nil || width != 96 || height != 54 || duration != 1 {
		t.Fatalf("actual probe = %dx%d, %gs, %v", width, height, duration, err)
	}
	for _, invalid := range [][]byte{nil, []byte("not a video"), raw[:32]} {
		if _, _, _, err := results.probeVideo(context.Background(), invalid); err == nil {
			t.Fatal("invalid or truncated media was accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := results.probeVideo(ctx, raw); err == nil {
		t.Fatal("cancelled inspection was accepted")
	}
	entries, err := os.ReadDir(temporary)
	if err != nil || len(entries) != 0 {
		t.Fatal("media inspection left temporary files", entries, err)
	}
}
