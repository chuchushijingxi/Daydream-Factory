package upload_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/upload"
	"drama-agent/internal/testsupport/uploadfiles"
)

type thumbnailFixture struct{}

func (thumbnailFixture) Generate(context.Context, []byte, ports.ThumbnailOptions) (ports.ThumbnailImage, error) {
	return ports.ThumbnailImage{Data: []byte("RIFF\x10\x00\x00\x00WEBPVP8 fictional"), Width: 10, Height: 5}, nil
}

type failObjects struct {
	ports.FileObjects
	part    string
	cleanup bool
	cancel  context.CancelFunc
}

func (o failObjects) PutFile(ctx context.Context, id, part, kind string, raw []byte) (d.Blob, error) {
	b, err := o.FileObjects.PutFile(ctx, id, part, kind, raw)
	if o.cancel != nil {
		o.cancel()
	}
	if part == o.part {
		return b, fault.New("FILE_STORAGE_UNAVAILABLE", "injected uncertain PUT")
	}
	return b, err
}
func (o failObjects) RemoveFile(ctx context.Context, id string, b d.Blob) error {
	if o.cleanup {
		return errors.New("injected cleanup failure")
	}
	return o.FileObjects.RemoveFile(ctx, id, b)
}

type failMetadata struct {
	ports.UploadedFileStore
	mode string
}

func (s failMetadata) CreateUploadedFile(ctx context.Context, f d.File) error {
	if s.mode == "adopted" {
		if err := s.UploadedFileStore.CreateUploadedFile(ctx, f); err != nil {
			return err
		}
	}
	return errors.New("injected metadata response failure")
}
func (s failMetadata) UploadedFile(ctx context.Context, id string) (d.File, error) {
	if s.mode == "unknown" {
		return d.File{}, errors.New("injected unreadable metadata")
	}
	return s.UploadedFileStore.UploadedFile(ctx, id)
}

func TestUploadPreservesAnyBytesAndConcurrentIdentity(t *testing.T) {
	s := uploadfiles.New()
	ctx := context.Background()
	for _, raw := range [][]byte{nil, []byte("whole source\r\n  \n"), {0, 0xff, 0x01, 0x10}} {
		f, err := s.Upload(ctx, app.Input{Filename: "unknown.custom", Data: raw})
		if err != nil {
			t.Fatal(err)
		}
		reopened := &app.Service{Store: s.Store, Objects: s.Objects}
		saved, read, err := reopened.ReadOriginal(ctx, f.ID)
		if err != nil || !reflect.DeepEqual(saved, f) || !bytes.Equal(read, raw) || f.Thumbnail != nil {
			t.Fatal("file restart readback lost metadata/bytes", err)
		}
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := s.Upload(ctx, app.Input{Filename: "same.bin", Data: []byte("same content")})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- f.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatal("same name/content collapsed identities")
		}
		seen[id] = true
	}
	if len(seen) != 8 {
		t.Fatal("concurrent upload lost a file")
	}
}

func TestUploadCompensatesOnlyUncommittedOwnObjects(t *testing.T) {
	for _, mode := range []string{"original", "thumbnail.webp", "rejected", "adopted", "unknown", "cleanup", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s := uploadfiles.New()
			mem := s.Store.(*uploadfiles.Memory)
			ctx := context.Background()
			prior, err := s.Upload(ctx, app.Input{Filename: "keep.txt", Data: []byte("preserve prior")})
			if err != nil {
				t.Fatal(err)
			}
			s.Thumbnails = thumbnailFixture{}
			options := &ports.ThumbnailOptions{MaxWidth: 20, MaxHeight: 20}
			want := 1
			switch mode {
			case "original", "thumbnail.webp":
				s.Objects = failObjects{FileObjects: s.Objects, part: mode}
			case "cleanup":
				s.Objects = failObjects{FileObjects: s.Objects, part: "thumbnail.webp", cleanup: true}
				want = 3
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				s.Objects = failObjects{FileObjects: s.Objects, cancel: cancel}
				options = nil
			default:
				s.Store = failMetadata{UploadedFileStore: s.Store, mode: mode}
				if mode != "rejected" {
					want = 3
				}
			}
			f, err := s.Upload(ctx, app.Input{Filename: "new.bin", Data: []byte("new bytes"), Thumbnail: options})
			if err == nil || len(mem.Objects) != want {
				t.Fatal("incorrect rollback/preservation", err, len(mem.Objects), want)
			}
			if mode == "cleanup" && !strings.Contains(err.Error(), "清理未确认") {
				t.Fatal("cleanup error swallowed", err)
			}
			if mode == "unknown" && !strings.Contains(err.Error(), "结果未知") {
				t.Fatal("unknown adoption hidden", err)
			}
			if mode == "adopted" {
				read, err := mem.UploadedFile(context.Background(), f.ID)
				if err != nil || read.ID != f.ID {
					t.Fatal("committed metadata deleted", err)
				}
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if _, raw, err := uploadfilesRead(mem, prior.ID); err != nil || string(raw) != "preserve prior" {
				t.Fatal("prior upload removed", err)
			}
		})
	}
}
func uploadfilesRead(mem *uploadfiles.Memory, id string) (d.File, []byte, error) {
	return (&app.Service{Store: mem, Objects: mem}).ReadOriginal(context.Background(), id)
}

func TestInvalidUploadWritesNothing(t *testing.T) {
	s := uploadfiles.New()
	mem := s.Store.(*uploadfiles.Memory)
	for _, in := range []app.Input{{Filename: "../"}, {Filename: "bad\nname"}, {Filename: "x", Data: make([]byte, d.MaxFileBytes+1)}, {Filename: "x", Data: []byte("x"), Thumbnail: &ports.ThumbnailOptions{MaxWidth: -1}}} {
		if _, err := s.Upload(context.Background(), in); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
	if len(mem.Files) != 0 || len(mem.Objects) != 0 {
		t.Fatal("invalid upload wrote data")
	}
}
