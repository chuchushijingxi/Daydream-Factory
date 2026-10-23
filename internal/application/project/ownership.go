package project

import (
	"context"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
)

// ForScope 只携带已认证用户与空间选择；授权在用例真正读取时执行。
// 后台继续使用原服务及资源持久空间，不依赖浏览器会话。
func (s *Service) ForScope(scope ports.SpaceScope) *Service {
	copy := *s
	copy.scope = &scope
	if s.Store == nil {
		copy.Store = deniedProjects{}
	} else {
		copy.Store = scopedProjects{ProjectStore: s.Store, service: &copy}
	}
	if files, ok := s.Files.(interface {
		ForFileScope(ports.SpaceScope) ports.FileReader
	}); ok {
		copy.Files = files.ForFileScope(scope)
	} else {
		copy.Files = nil
	}
	if s.MediaTasks != nil {
		copy.MediaTasks = s.MediaTasks.ForScope(scope)
	}
	if s.Models != nil {
		copy.Models = userModels{ModelProvider: s.Models}
	}
	return &copy
}

// forSpace 仅用于已持久化后台工作。用户入口必须经 ForScope。
func (s *Service) forSpace(space string) *Service {
	copy := *s
	copy.scope = nil
	copy.Store = trustedSpaceProjects{ProjectStore: s.Store, space: space}
	if files, ok := s.Files.(ports.ScopedFileReader); ok {
		copy.Files = files.ForFileSpace(space)
	} else {
		copy.Files = nil
	}
	if s.MediaTasks != nil {
		copy.MediaTasks = s.MediaTasks.ForSpace(space)
	}
	return &copy
}

func (s *Service) resolveSpace(ctx context.Context) (ports.SpaceScope, error) {
	if s.scope == nil {
		return ports.SpaceScope{}, nil
	}
	return ports.ResolveSpace(ctx, s.Resolver, *s.scope)
}
func (s *Service) requireProject(ctx context.Context, p d.Project, need spaces.Access) error {
	if s.scope == nil {
		return nil
	}
	_, err := ports.RequireSpaceAccess(ctx, s.Access, *s.scope, spaces.Resource{SpaceID: p.SpaceID, OwnerID: p.OwnerID, Kind: "project", ID: p.ID}, need)
	return err
}
func (s *Service) editingProject(ctx context.Context, id string) (d.Project, error) {
	p, err := s.Project(ctx, id)
	if err == nil {
		err = s.requireProject(ctx, p, spaces.Edit)
	}
	if err != nil {
		return d.Project{}, err
	}
	return p, nil
}

// scopedProjects 对按 ID 的操作先读取资源自身空间，再做统一权限判断。
// 列表和创建才使用当前选择空间；数据库写始终带资源空间及 revision。
type scopedProjects struct {
	ports.ProjectStore
	service *Service
}

func (s scopedProjects) Create(ctx context.Context, p d.Project) error {
	scope, err := s.service.resolveSpace(ctx)
	if err != nil {
		return err
	}
	if scope.ReadOnly {
		return fault.New("TEAM_READ_ONLY", "团队已只读")
	}
	if p.SpaceID != scope.SpaceID || p.OwnerID != scope.UserID || p.CreatorID != scope.UserID {
		return fault.ErrInvalid
	}
	if stores, ok := s.ProjectStore.(ports.ScopedProjects); ok {
		return stores.ForProjectSpace(scope.SpaceID).Create(ctx, p)
	}
	return fault.New("AUTH_REQUIRED", "项目空间存储不可用")
}
func (s scopedProjects) Snapshot(ctx context.Context, id string) (d.Project, error) {
	p, err := s.ProjectStore.Snapshot(ctx, id)
	if err == nil {
		err = s.service.requireProject(ctx, p, spaces.Read)
	}
	if err != nil {
		return d.Project{}, err
	}
	return p, nil
}
func (s scopedProjects) ReadProject(ctx context.Context, id string) (d.Project, error) {
	reader, ok := s.ProjectStore.(ports.ProjectReader)
	if !ok {
		return s.Snapshot(ctx, id)
	}
	p, err := reader.ReadProject(ctx, id)
	if err == nil {
		err = s.service.requireProject(ctx, p, spaces.Read)
	}
	if err != nil {
		return d.Project{}, err
	}
	return p, nil
}
func (s scopedProjects) MediaProject(ctx context.Context, id string) (d.Project, error) {
	reader, ok := s.ProjectStore.(ports.ProjectMediaReader)
	if !ok {
		return s.Snapshot(ctx, id)
	}
	p, err := reader.MediaProject(ctx, id)
	if err == nil {
		err = s.service.requireProject(ctx, p, spaces.Read)
	}
	if err != nil {
		return d.Project{}, err
	}
	return p, nil
}
func (s scopedProjects) Commit(ctx context.Context, p d.Project, revision uint64) error {
	// 每次写入重新读取当前负责人，转交或撤销共享不能沿用旧快照的权限。
	current, err := s.ProjectStore.Snapshot(ctx, p.ID)
	if err != nil {
		return err
	}
	if err = s.service.requireProject(ctx, current, spaces.Edit); err != nil {
		return err
	}
	if current.SpaceID != p.SpaceID {
		return fault.ErrConflict
	}
	if stores, ok := s.ProjectStore.(ports.ScopedProjects); ok {
		return stores.ForProjectSpace(p.SpaceID).Commit(ctx, p, revision)
	}
	return fault.New("AUTH_REQUIRED", "项目空间存储不可用")
}
func (s scopedProjects) ListSummaries(ctx context.Context) ([]ports.ProjectSummary, error) {
	return s.service.ListProjectsFiltered(ctx, ports.ListAll)
}

// trustedSpaceProjects 保留执行、故障与持久化装饰器；不能通过派生裸仓储绕过它们。
// 仅后台消费持久空间，正式仓储 Commit 仍执行 id/revision/space CAS。
type trustedSpaceProjects struct {
	ports.ProjectStore
	space string
}

func (s trustedSpaceProjects) Snapshot(ctx context.Context, id string) (d.Project, error) {
	if s.space == "" {
		return d.Project{}, fault.New("AUTH_REQUIRED", "缺少持久空间")
	}
	p, err := s.ProjectStore.Snapshot(ctx, id)
	if err == nil && p.SpaceID != s.space {
		return d.Project{}, fault.ErrNotFound
	}
	return p, err
}
func (s trustedSpaceProjects) Commit(ctx context.Context, p d.Project, revision uint64) error {
	if s.space == "" || p.SpaceID != s.space {
		return fault.ErrNotFound
	}
	return s.ProjectStore.Commit(ctx, p, revision)
}

type deniedProjects struct{ ports.ProjectStore }

func (deniedProjects) Create(context.Context, d.Project) error {
	return fault.New("AUTH_REQUIRED", "项目空间存储不可用")
}
func (deniedProjects) Snapshot(context.Context, string) (d.Project, error) {
	return d.Project{}, fault.New("AUTH_REQUIRED", "项目空间存储不可用")
}
func (deniedProjects) Commit(context.Context, d.Project, uint64) error {
	return fault.New("AUTH_REQUIRED", "项目空间存储不可用")
}
func (deniedProjects) ListSummaries(context.Context) ([]ports.ProjectSummary, error) {
	return nil, fault.New("AUTH_REQUIRED", "项目空间存储不可用")
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
