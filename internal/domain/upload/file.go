// Package upload owns immutable uploaded files, independently of projects and tasks.
package upload

import (
	"encoding/hex"
	"time"
)

const MaxFileBytes int64 = 128 << 20

type Blob struct {
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"objectKey"`
	SizeBytes   int64  `json:"sizeBytes"`
	ContentType string `json:"contentType"`
	SHA256      string `json:"sha256"`
}

type Thumbnail struct {
	Blob   Blob `json:"blob"`
	Width  int  `json:"width"`
	Height int  `json:"height"`
}

type File struct {
	SpaceID   string     `json:"spaceId"`
	CreatorID string     `json:"creatorId"`
	OwnerID   string     `json:"ownerId,omitempty"`
	ID        string     `json:"id"`
	Filename  string     `json:"filename"`
	Original  Blob       `json:"original"`
	Thumbnail *Thumbnail `json:"thumbnail,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

func ValidID(id string) bool {
	if len(id) != 37 || id[:5] != "file_" {
		return false
	}
	_, err := hex.DecodeString(id[5:])
	return err == nil
}

func ObjectKey(id, part string) string { return "uploads/" + id + "/" + part }

func (b Blob) Valid(id string) bool {
	if !ValidID(id) || b.Bucket == "" || b.ContentType == "" || b.SizeBytes < 0 || b.SizeBytes > MaxFileBytes || len(b.SHA256) != 64 {
		return false
	}
	_, err := hex.DecodeString(b.SHA256)
	return err == nil && (b.ObjectKey == ObjectKey(id, "original") || b.ObjectKey == ObjectKey(id, "thumbnail.webp"))
}
