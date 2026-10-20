package notifications

import (
	"bytes"
	"context"
	"drama-agent/internal/application/ports"
	"image"
	"image/png"
	"reflect"
	"testing"
)

func TestNotificationImageReferencesOnlyOwnMessage(t *testing.T) {
	for _, destinations := range [][]string{{"/api/notifications/n/images/i"}, {"/api/notifications/n/images/i", "/api/notifications/n/images/i"}} {
		ids, e := imageReferences(destinations, "n")
		if e != nil || !reflect.DeepEqual(ids, []string{"i"}) {
			t.Fatal(destinations, ids, e)
		}
	}
	for _, destination := range []string{"https://example.com/private.png", "/api/files/file_a/original", "/api/notifications/other/images/i", "data:image/png;base64,AA", "//external/x", "/api/notifications/n/images/%69", "/api/projects/private/original", "/api/notifications/n/images/i?userId=other"} {
		if _, e := imageReferences([]string{destination}, "n"); e == nil {
			t.Fatal("unsafe image accepted", destination)
		}
	}
}

type imageStore struct {
	Store
	saved bool
}

func (s *imageStore) OperatorNotification(context.Context, string, string) (Message, error) {
	return Message{Status: "draft"}, nil
}
func (s *imageStore) SaveNotificationImage(_ context.Context, _, _ string, v Image) (Image, error) {
	s.saved = true
	return v, nil
}
func TestNotificationImageValidatesPixelsAndRemovesTrailingData(t *testing.T) {
	store := &imageStore{}
	service := &Service{Store: store}
	op := ports.Actor{UserID: "op", Role: ports.RoleOperator}
	var large bytes.Buffer
	png.Encode(&large, image.NewNRGBA(image.Rect(0, 0, 4097, 1)))
	for _, raw := range [][]byte{[]byte(`<svg onload="alert(1)"/>`), large.Bytes(), make([]byte, MaxImageBytes+1)} {
		if _, e := service.UploadImage(context.Background(), op, "n", raw); e == nil || store.saved {
			t.Fatal("invalid pixels accepted", e)
		}
	}
	var valid bytes.Buffer
	png.Encode(&valid, image.NewNRGBA(image.Rect(0, 0, 4, 2)))
	v, e := service.UploadImage(context.Background(), op, "n", append(valid.Bytes(), []byte("private trailing metadata")...))
	if e != nil || !store.saved || !bytes.Equal(v.Data, valid.Bytes()) || v.Width != 4 || v.Height != 2 || v.ContentType != "image/png" {
		t.Fatal("image not validated/reencoded", v, e)
	}
}

type emptyDraftStore struct {
	Store
	published bool
}

func (s *emptyDraftStore) OperatorNotification(context.Context, string, string) (Message, error) {
	return Message{Status: "draft"}, nil
}
func (s *emptyDraftStore) PublishNotification(context.Context, string, string, uint64) (Message, error) {
	s.published = true
	return Message{}, nil
}
func TestEmptyDraftCannotPublishOrAuthenticateThroughImageAndWatch(t *testing.T) {
	store := &emptyDraftStore{}
	service := &Service{Store: store}
	if _, e := service.Publish(context.Background(), ports.Actor{UserID: "op", Role: ports.RoleOperator}, "n", 1); e == nil || store.published {
		t.Fatal("empty draft published", e)
	}
	if _, e := service.Image(context.Background(), ports.Actor{}, "n", "i"); e == nil {
		t.Fatal("empty actor image")
	}
	if _, e := service.OperatorImage(context.Background(), ports.Actor{UserID: "ordinary", Role: ports.RoleUser}, "n", "i"); e == nil {
		t.Fatal("ordinary preview")
	}
	if _, _, e := service.Watch(context.Background(), ports.Actor{}); e == nil {
		t.Fatal("empty actor stream")
	}
}
