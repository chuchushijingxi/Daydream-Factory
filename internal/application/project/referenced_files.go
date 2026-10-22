package project

import (
	"context"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	u "drama-agent/internal/domain/upload"
)

// referencedFile只相信服务器读回的项目事实；调用者声明一个容器ID不能扩大文件权限。
func referencedFile(p d.Project, fileID string) bool {
	if fileID == "" {
		return false
	}
	if p.SourceFileID == fileID {
		return true
	}
	for _, asset := range p.Assets {
		if asset.ImageFileID == fileID {
			return true
		}
		for _, look := range asset.Looks {
			if look.ImageFileID == fileID {
				return true
			}
		}
	}
	return false
}
func (s *Service) referencedFileProject(ctx context.Context, id, fileID string) (d.Project, error) {
	p, err := s.ReadProject(ctx, id)
	if err != nil {
		return d.Project{}, err
	}
	if !referencedFile(p, fileID) {
		return d.Project{}, fault.ErrNotFound
	}
	return p, nil
}
func (s *Service) containerFiles() (ports.ContainerFileReader, error) {
	files, ok := s.Files.(ports.ContainerFileReader)
	if !ok {
		return nil, fault.New("FILE_STORAGE_UNAVAILABLE", "项目引用文件读取尚未组装")
	}
	return files, nil
}
func (s *Service) ReferencedFile(ctx context.Context, id, fileID string) (u.File, error) {
	p, err := s.referencedFileProject(ctx, id, fileID)
	if err != nil {
		return u.File{}, err
	}
	files, err := s.containerFiles()
	if err != nil {
		return u.File{}, err
	}
	file, err := files.ContainerFile(ctx, p.SpaceID, fileID)
	if err == nil && (file.ID != fileID || file.SpaceID != p.SpaceID) {
		return u.File{}, fault.ErrNotFound
	}
	return file, err
}
func (s *Service) ReferencedFileBytes(ctx context.Context, id, fileID string, thumbnail bool) (u.File, []byte, error) {
	p, err := s.referencedFileProject(ctx, id, fileID)
	if err != nil {
		return u.File{}, nil, err
	}
	files, err := s.containerFiles()
	if err != nil {
		return u.File{}, nil, err
	}
	file, raw, err := files.ReadContainerFile(ctx, p.SpaceID, fileID, thumbnail)
	if err == nil && (file.ID != fileID || file.SpaceID != p.SpaceID) {
		return u.File{}, nil, fault.ErrNotFound
	}
	return file, raw, err
}
