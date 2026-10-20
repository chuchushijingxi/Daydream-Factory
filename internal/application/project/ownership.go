package project

import (
	"context"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// ForActor is the explicit user-use-case entry. Workers keep the original service
// and use persisted owners, independently of the browser session's lifetime.
func (s *Service) ForActor(a ports.Actor) *Service {
	owner := ""
	if a.Require() == nil {
		owner = a.UserID
	}
	copy := s.forOwner(owner)
	if s.Models != nil {
		copy.Models = userModels{ModelProvider: s.Models}
	}
	return copy
}

// 后台仅用已持久化的owner约束相同资源端口，不构造浏览器会话或用户Actor。
func (s *Service) forOwner(owner string) *Service {
	copy := *s
	copy.owner = owner
	if store, ok := s.Store.(ports.OwnedProjects); ok {
		copy.Store = store.ForProjectOwner(owner)
	} else {
		copy.Store = deniedProjects{ProjectStore: s.Store}
	}
	if files, ok := s.Files.(ports.OwnedFileReader); ok {
		copy.Files = files.ForFileOwner(owner)
	} else {
		copy.Files = nil
	}
	if s.MediaTasks != nil {
		copy.MediaTasks = s.MediaTasks.ForOwner(owner)
	}
	return &copy
}

type deniedProjects struct{ ports.ProjectStore }

func (deniedProjects) Create(context.Context, d.Project) error {
	return fault.New("AUTH_REQUIRED", "项目归属存储不可用")
}
func (deniedProjects) Snapshot(context.Context, string) (d.Project, error) {
	return d.Project{}, fault.New("AUTH_REQUIRED", "项目归属存储不可用")
}
func (deniedProjects) Commit(context.Context, d.Project, uint64) error {
	return fault.New("AUTH_REQUIRED", "项目归属存储不可用")
}
func (deniedProjects) ListSummaries(context.Context) ([]ports.ProjectSummary, error) {
	return nil, fault.New("AUTH_REQUIRED", "项目归属存储不可用")
}

type userModels struct{ ports.ModelProvider }

func (m userModels) Options(ctx context.Context) (ports.ModelOptions, error) {
	if provider, ok := m.ModelProvider.(interface {
		UserOptions(context.Context) (ports.ModelOptions, error)
	}); ok {
		return provider.UserOptions(ctx)
	}
	return m.ModelProvider.Options(ctx)
}
