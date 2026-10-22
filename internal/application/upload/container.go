package upload

import (
	"context"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/upload"
)

// ContainerFile is an internal capability, called after the owning application
// verified a readable real container and its exact reference. A supplied project
// ID is never accepted here as proof, and the file must belong to that space.
func (s *Service) ContainerFile(ctx context.Context, spaceID, fileID string) (d.File, error) {
	if spaceID == "" || !d.ValidID(fileID) {
		return d.File{}, fault.ErrNotFound
	}
	if s == nil || s.Store == nil {
		return d.File{}, fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
	}
	file, err := s.Store.UploadedFile(ctx, fileID)
	if err != nil {
		return d.File{}, err
	}
	if file.SpaceID != spaceID {
		return d.File{}, fault.ErrNotFound
	}
	return file, nil
}
func (s *Service) ReadContainerFile(ctx context.Context, spaceID, fileID string, thumbnail bool) (d.File, []byte, error) {
	file, err := s.ContainerFile(ctx, spaceID, fileID)
	if err != nil {
		return d.File{}, nil, err
	}
	return s.readFile(ctx, file, thumbnail)
}
