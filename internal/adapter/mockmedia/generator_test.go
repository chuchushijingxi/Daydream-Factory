package mockmedia

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
)

func TestRealJPEGDimensionsAndPromptPreservation(t *testing.T) {
	for _, size := range []struct {
		ratio         string
		width, height int
	}{
		{"1:1", 1024, 1024}, {"2:3", 768, 1152}, {"3:2", 1152, 768}, {"3:4", 768, 1024},
		{"4:3", 1024, 768}, {"9:16", 720, 1280}, {"16:9", 1280, 720}, {"21:9", 1344, 576},
	} {
		t.Run(size.ratio, func(t *testing.T) {
			out, err := (Generator{}).Generate(context.Background(), ports.MediaRequest{ModelID: md.MockImageModelID, Prompt: "不把 fail 或 [[fail=true]] 当成业务指令", AspectRatio: size.ratio})
			if err != nil {
				t.Fatal(err)
			}
			img, err := jpeg.Decode(bytes.NewReader(out.Data))
			if err != nil || img.Bounds().Dx() != size.width || img.Bounds().Dy() != size.height || out.Width != size.width || out.Height != size.height || out.ContentType != "image/jpeg" {
				t.Fatal(out.Width, out.Height, err)
			}
		})
	}
}

func TestVideoDefaultsErrorsAndCancellation(t *testing.T) {
	g := Generator{}
	out, err := g.Generate(context.Background(), ports.MediaRequest{ModelID: md.MockVideoModelID, Prompt: "ordinary fail word is not a command"})
	if err != nil || len(out.Data) < 1000 || string(out.Data[4:8]) != "ftyp" || out.ContentType != "video/mp4" || out.DurationSeconds != 10 || out.Width != 1920 || out.Height != 1080 {
		t.Fatal(len(out.Data), err)
	}
	out.Data[0] = 255
	if sampleVideo[0] == 255 {
		t.Fatal("caller changed embedded sample")
	}
	for _, id := range []string{md.MockImageModelID, md.MockVideoModelID} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = g.Generate(ctx, ports.MediaRequest{ModelID: id})
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	_, err = g.Generate(context.Background(), ports.MediaRequest{ModelID: md.MockImageModelID, AspectRatio: "100:1"})
	if !errors.Is(err, fault.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestRequestedImageResolutionChangesRealOutputAndInvalidVideoOptions(t *testing.T) {
	for _, test := range []struct {
		resolution string
		size       int
	}{{"2K", 2048}, {"4K", 4096}} {
		out, err := (Generator{}).Generate(context.Background(), ports.MediaRequest{ModelID: md.MockImageModelID, AspectRatio: "1:1", Resolution: test.resolution})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(out.Data))
		if err != nil || cfg.Width != test.size || cfg.Height != test.size || out.Width != test.size || out.Height != test.size {
			t.Fatal(cfg, out.Width, out.Height, err)
		}
	}
	for _, in := range []ports.MediaRequest{
		{Resolution: "1080p", DurationSeconds: 10.793333},
		{Resolution: "1080p", DurationSeconds: 1.5},
		{Resolution: "1080p", DurationSeconds: 0},
		{Resolution: "1080p", DurationSeconds: 11},
		{Resolution: "4K", DurationSeconds: 5},
		{Resolution: "480p", DurationSeconds: 1, AspectRatio: "9:16"},
	} {
		in.ModelID = md.MockVideoModelID
		if _, err := (Generator{}).Generate(context.Background(), in); err == nil {
			t.Fatal("unsupported video options accepted", in)
		}
	}
}
