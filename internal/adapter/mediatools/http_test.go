package mediatools

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

// webp is a minimal RIFF/WEBP header; http.DetectContentType only needs the prefix.
var webp = append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{0}, 24)...)

type fakeTools struct {
	err      error
	received []byte
}

func (f *fakeTools) Generate(_ context.Context, raw []byte, o ports.ThumbnailOptions) (ports.ThumbnailImage, error) {
	f.received = raw
	if f.err != nil {
		return ports.ThumbnailImage{}, f.err
	}
	return ports.ThumbnailImage{Data: webp, Width: o.MaxWidth / 2, Height: o.MaxHeight / 2}, nil
}
func (f *fakeTools) ProbeVideo(_ context.Context, raw []byte) (int, int, float64, error) {
	f.received = raw
	if f.err != nil {
		return 0, 0, 0, f.err
	}
	return 1280, 720, 4.5, nil
}
func (f *fakeTools) EncodeVideo(_ context.Context, input []byte, w, h int, s float64) ([]byte, error) {
	f.received = input
	if f.err != nil {
		return nil, f.err
	}
	return []byte("mp4:" + string(rune('0'+w%10)) + string(rune('0'+h%10))), nil
}

func TestRemoteRoundTripsThroughTheMediaServiceHandler(t *testing.T) {
	tools := &fakeTools{}
	server := httptest.NewServer(Handler(tools))
	defer server.Close()
	remote := New(appconfig.Media{Endpoint: server.URL + "/"})
	if _, ok := remote.(*Remote); !ok {
		t.Fatal("configured endpoint must select the media service")
	}
	image, err := remote.Generate(context.Background(), []byte("png-bytes"), ports.ThumbnailOptions{MaxWidth: 400, MaxHeight: 300})
	if err != nil || !bytes.Equal(image.Data, webp) || image.Width != 200 || image.Height != 150 || string(tools.received) != "png-bytes" {
		t.Fatal("thumbnail", image.Width, image.Height, err)
	}
	w, h, seconds, err := remote.ProbeVideo(context.Background(), []byte("mp4-bytes"))
	if err != nil || w != 1280 || h != 720 || seconds != 4.5 {
		t.Fatal("probe", w, h, seconds, err)
	}
	out, err := remote.EncodeVideo(context.Background(), []byte("sample"), 854, 480, 3)
	if err != nil || string(out) != "mp4:40" || string(tools.received) != "sample" {
		t.Fatal("encode", string(out), err)
	}
}

func TestRemoteRestoresServiceErrorsAsTheSameFaults(t *testing.T) {
	tools := &fakeTools{err: fault.New("MOCK_ENCODER_UNAVAILABLE", "本地 Mock 视频需要 ffmpeg 和 libx264 编码器")}
	server := httptest.NewServer(Handler(tools))
	defer server.Close()
	remote := NewRemote(server.URL)
	_, err := remote.EncodeVideo(context.Background(), []byte("sample"), 1920, 1080, 10)
	var f *fault.Error
	if !errors.As(err, &f) || f.Code != "MOCK_ENCODER_UNAVAILABLE" || f.Message == "" {
		t.Fatal("business code lost across the service boundary", err)
	}
	tools.err = fault.ErrInvalid
	if _, err = remote.Generate(context.Background(), []byte("x"), ports.ThumbnailOptions{MaxWidth: 1, MaxHeight: 1}); !errors.Is(err, fault.ErrInvalid) {
		t.Fatal("invalid input must stay the shared sentinel", err)
	}
	tools.err = errors.New("unexpected")
	if _, _, _, err = remote.ProbeVideo(context.Background(), []byte("x")); !errors.As(err, &f) || f.Code != "MEDIA_FAILED" {
		t.Fatal("internal failures must not leak details", err)
	}
}

func TestRemoteReportsAnUnreachableServiceAndHonoursCancellation(t *testing.T) {
	server := httptest.NewServer(Handler(&fakeTools{}))
	endpoint := server.URL
	server.Close()
	remote := NewRemote(endpoint)
	_, err := remote.Generate(context.Background(), []byte("x"), ports.ThumbnailOptions{MaxWidth: 10, MaxHeight: 10})
	var f *fault.Error
	if !errors.As(err, &f) || f.Code != "MEDIA_UNAVAILABLE" {
		t.Fatal("unreachable service", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = remote.EncodeVideo(ctx, []byte("x"), 1, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled request", err)
	}
	if _, ok := New(appconfig.Media{}).(Local); !ok {
		t.Fatal("no endpoint must run the tools on this host")
	}
}
