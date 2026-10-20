package notifications

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"image"
	"image/jpeg"
	"image/png"
	"net/url"
	"strings"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
)

const MaxImageBytes = 5 << 20

type Image struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	PreviewURL  string `json:"previewUrl"`
	ContentType string `json:"contentType"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	SizeBytes   int    `json:"sizeBytes"`
	Data        []byte `json:"-"`
	Digest      string `json:"-"`
}

func invalidImage() error {
	return fault.New("NOTIFICATION_IMAGE_INVALID", "请选择5MiB以内、长宽不超过4096的PNG/JPEG；正文图片只能引用本通知上传的图片")
}

func (s *Service) imageReferences(body, messageID string) ([]string, error) {
	if s.Markdown == nil {
		return nil, fault.New("NOTIFICATION_UNAVAILABLE", "通知Markdown解析资源不可用")
	}
	return imageReferences(s.Markdown.ImageDestinations(body), messageID)
}

func imageReferences(destinations []string, messageID string) ([]string, error) {
	ids := []string{}
	seen := map[string]bool{}
	for _, destination := range destinations {
		value := html.UnescapeString(destination)
		u, e := url.Parse(value)
		prefix := "/api/notifications/" + messageID + "/images/"
		if e != nil || u.Scheme != "" || u.Host != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, prefix) {
			return nil, invalidImage()
		}
		id := strings.TrimPrefix(u.Path, prefix)
		if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\\x00\r\n\t") {
			return nil, invalidImage()
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *Service) UploadImage(ctx context.Context, a ports.Actor, messageID string, raw []byte) (Image, error) {
	if e := a.RequireOperator(); e != nil {
		return Image{}, e
	}
	m, e := s.Store.OperatorNotification(ctx, a.UserID, messageID)
	if e != nil {
		return Image{}, e
	}
	if m.Status != "draft" {
		return Image{}, fault.New("NOTIFICATION_CONFLICT", "只有草稿可上传图片")
	}
	if len(raw) == 0 || len(raw) > MaxImageBytes {
		return Image{}, invalidImage()
	}
	config, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || (format != "png" && format != "jpeg") || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		return Image{}, invalidImage()
	}
	decoded, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return Image{}, invalidImage()
	}
	var out bytes.Buffer
	// Re-encode actual pixels: do not publish embedded metadata or trailing data.
	contentType := "image/png"
	if format == "jpeg" {
		contentType = "image/jpeg"
		e = jpeg.Encode(&out, decoded, &jpeg.Options{Quality: 90})
	} else {
		e = png.Encode(&out, decoded)
	}
	if e != nil {
		return Image{}, e
	}
	if out.Len() > MaxImageBytes {
		return Image{}, invalidImage()
	}
	sum := sha256.Sum256(out.Bytes())
	v := Image{ID: identity.New("noticeimg"), ContentType: contentType, Width: config.Width, Height: config.Height, SizeBytes: out.Len(), Data: out.Bytes(), Digest: hex.EncodeToString(sum[:])}
	return s.Store.SaveNotificationImage(ctx, a.UserID, messageID, v)
}

func (s *Service) Image(ctx context.Context, a ports.Actor, messageID, imageID string) (Image, error) {
	if e := a.Require(); e != nil {
		return Image{}, e
	}
	return s.Store.NotificationImage(ctx, a.UserID, messageID, imageID)
}

func (s *Service) OperatorImage(ctx context.Context, a ports.Actor, messageID, imageID string) (Image, error) {
	if e := a.RequireOperator(); e != nil {
		return Image{}, e
	}
	return s.Store.OperatorNotificationImage(ctx, a.UserID, messageID, imageID)
}
