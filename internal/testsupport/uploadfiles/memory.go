// Package uploadfiles provides isolated file ports for free HTTP/application checks.
package uploadfiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/upload"
)

type Memory struct {
	mu      sync.Mutex
	Files   map[string]d.File
	Objects map[string][]byte
}

func New() *app.Service {
	m := &Memory{Files: map[string]d.File{}, Objects: map[string][]byte{}}
	return &app.Service{Store: m, Objects: m}
}

func (m *Memory) CreateUploadedFile(ctx context.Context, f d.File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.Files[f.ID]; exists {
		return fault.ErrConflict
	}
	raw, _ := json.Marshal(f)
	var copy d.File
	_ = json.Unmarshal(raw, &copy)
	m.Files[f.ID] = copy
	return nil
}
func (m *Memory) UploadedFile(ctx context.Context, id string) (d.File, error) {
	if err := ctx.Err(); err != nil {
		return d.File{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.Files[id]
	if !ok {
		return f, fault.ErrNotFound
	}
	raw, _ := json.Marshal(f)
	var copy d.File
	_ = json.Unmarshal(raw, &copy)
	return copy, nil
}
func (m *Memory) PutFile(ctx context.Context, id, part, kind string, raw []byte) (d.Blob, error) {
	sum := sha256.Sum256(raw)
	b := d.Blob{Bucket: "fixture", ObjectKey: d.ObjectKey(id, part), SizeBytes: int64(len(raw)), ContentType: kind, SHA256: hex.EncodeToString(sum[:])}
	if err := ctx.Err(); err != nil {
		return b, err
	}
	if !b.Valid(id) {
		return d.Blob{}, fault.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Objects[b.ObjectKey] = append([]byte(nil), raw...)
	return b, nil
}
func (m *Memory) ReadFile(ctx context.Context, id string, b d.Blob) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !b.Valid(id) || b.Bucket != "fixture" {
		return nil, fault.ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.Objects[b.ObjectKey]
	if !ok {
		return nil, fault.ErrNotFound
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != b.SizeBytes || hex.EncodeToString(sum[:]) != b.SHA256 {
		return nil, fault.New("FILE_CORRUPT", "fixture checksum mismatch")
	}
	return append([]byte(nil), raw...), nil
}
func (m *Memory) RemoveFile(ctx context.Context, id string, b d.Blob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !b.Valid(id) {
		return fault.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Objects, b.ObjectKey)
	return nil
}

func (m *Memory) DetectContentType(raw []byte) string { return http.DetectContentType(raw) }

type ownedFiles struct {
	m     *Memory
	owner string
}

func (m *Memory) ForUploadedFileOwner(owner string) ports.UploadedFileStore {
	return ownedFiles{m, owner}
}
func (s ownedFiles) CreateUploadedFile(ctx context.Context, f d.File) error {
	if s.owner == "" || f.OwnerID != s.owner {
		return fault.ErrInvalid
	}
	return s.m.CreateUploadedFile(ctx, f)
}
func (s ownedFiles) UploadedFile(ctx context.Context, id string) (d.File, error) {
	f, e := s.m.UploadedFile(ctx, id)
	if e == nil && (s.owner == "" || f.OwnerID != s.owner) {
		return d.File{}, fault.ErrNotFound
	}
	return f, e
}
