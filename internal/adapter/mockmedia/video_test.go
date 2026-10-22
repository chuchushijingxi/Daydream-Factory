package mockmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/mediatools"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	td "drama-agent/internal/domain/task"
)

func TestVideoOptionsAllThirtyActualFilesAndFrozenRecovery(t *testing.T) {
	ctx := context.Background()
	provider := TaskProvider{Encoder: mediatools.Local{Settings: appconfig.Defaults().Media}}
	frozen, _ := json.Marshal(md.MockExecution{VideoOptions: true})
	b := td.Binding{Protocol: md.MockProtocol, RequestModelID: md.MockVideoModelID, ProviderConfig: frozen}
	file := td.SourceFile{ID: "media_fixture", Source: "mock-submission_fixture"}
	path := filepath.Join(t.TempDir(), "actual.mp4")
	for _, size := range []struct {
		id            string
		width, height int
	}{{"480p", 854, 480}, {"720p", 1280, 720}, {"1080p", 1920, 1080}} {
		for seconds := 1; seconds <= 10; seconds++ {
			t.Run(fmt.Sprintf("%s/%ds", size.id, seconds), func(t *testing.T) {
				input, _ := json.Marshal(TaskInput{Prompt: "按原提示词保留，Mock不生成剧情", Resolution: size.id, DurationSeconds: float64(seconds)})
				if _, err := provider.Prepare(ctx, b, input); err != nil {
					t.Fatal(err)
				}
				// A fresh provider has no process memory or current model config.
				out, err := (TaskProvider{Encoder: mediatools.Local{Settings: appconfig.Defaults().Media}}).Fetch(ctx, b, file, input)
				if err != nil || out.Width != size.width || out.Height != size.height || out.DurationSeconds != float64(seconds) {
					t.Fatal(out.Width, out.Height, out.DurationSeconds, err)
				}
				if err = os.WriteFile(path, out.Data, 0600); err != nil {
					t.Fatal(err)
				}
				raw, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-count_frames", "-show_entries", "stream=width,height,codec_name,pix_fmt,nb_read_frames:format=duration", "-of", "json", path).Output()
				if err != nil {
					t.Fatal(err)
				}
				var actual struct {
					Streams []struct {
						Width, Height int
						Codec         string `json:"codec_name"`
						Pixel         string `json:"pix_fmt"`
						Frames        string `json:"nb_read_frames"`
					}
					Format struct{ Duration string }
				}
				if json.Unmarshal(raw, &actual) != nil || len(actual.Streams) != 1 {
					t.Fatal(string(raw))
				}
				stream := actual.Streams[0]
				duration, err := strconv.ParseFloat(actual.Format.Duration, 64)
				if err != nil || duration != float64(seconds) || stream.Width != size.width || stream.Height != size.height || stream.Codec != "h264" || stream.Pixel != "yuv420p" || stream.Frames != strconv.Itoa(seconds*30) {
					t.Fatal(string(raw), err)
				}
				t.Logf("actual=%dx%d duration=%gs h264/yuv420p frames=%s bytes=%d", stream.Width, stream.Height, duration, stream.Frames, len(out.Data))
			})
		}
	}
	for _, oldConfig := range []json.RawMessage{nil, json.RawMessage(`{"delaySeconds":0,"failurePercent":0,"fail":false}`)} {
		b.ProviderConfig = oldConfig
		for _, oldOptions := range []TaskInput{{Prompt: "old"}, {Prompt: "old", Resolution: "1080p", DurationSeconds: md.MockVideoDurationSeconds}} {
			input, _ := json.Marshal(oldOptions)
			out, err := provider.Fetch(ctx, b, file, input)
			if err != nil || !bytes.Equal(out.Data, sampleVideo) || out.DurationSeconds != md.MockVideoDurationSeconds {
				t.Fatal("accepted sample task was reinterpreted", err)
			}
		}
	}
	b.ProviderConfig = frozen
	if _, err := provider.Prepare(ctx, b, json.RawMessage(`{"prompt":"new","aspectRatio":"","resolution":"1080p","durationSeconds":10.793333}`)); err == nil {
		t.Fatal("new task accepted old fractional sample duration")
	}
}

func TestVideoEncoderFailureAndCancellationCannotReturnSample(t *testing.T) {
	in := ports.MediaRequest{ModelID: md.MockVideoModelID, Resolution: "1080p", DurationSeconds: 10}
	_, err := (Generator{Encoder: mediatools.Local{Settings: appconfig.Media{FFmpeg: filepath.Join(t.TempDir(), "missing")}}}).Generate(context.Background(), in)
	var f *fault.Error
	if !errors.As(err, &f) || f.Code != "MOCK_ENCODER_UNAVAILABLE" {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	out, err := (Generator{Encoder: mediatools.Local{}}).Generate(ctx, in)
	if !errors.Is(err, context.DeadlineExceeded) || len(out.Data) != 0 {
		t.Fatal("cancelled encoder returned output", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary video files leaked", entries, err)
	}
	encoder := filepath.Join(t.TempDir(), "encoder")
	if err = os.WriteFile(encoder, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	out, err = (Generator{Encoder: mediatools.Local{Settings: appconfig.Media{FFmpeg: encoder}}}).Generate(context.Background(), in)
	if !errors.As(err, &f) || f.Code != "MOCK_ENCODING_FAILED" || len(out.Data) != 0 {
		t.Fatal("failed encoder returned fallback", err)
	}
}
