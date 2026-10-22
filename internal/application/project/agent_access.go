package project

import (
	"context"
	"errors"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
)

const permissionChangedCode = "PROJECT_PERMISSION_CHANGED"

// Agent身份冻结在受理计费依据，转交负责人不能把运行中的Agent变成接收人。
// 只在内容工具/新模型读取边界检查；外发回执与费用始终能按原事实持久化、结算。
func (s *Service) checkAgentAccess(ctx context.Context, p d.Project) error {
	if p.SpaceID == "" {
		return nil
	} // 无持久空间的纯内存协议测试不具有成员关系。
	if s.agentUserID == "" || s.Access == nil {
		return fault.New(permissionChangedCode, "因权限变更停止")
	}
	permission, _, err := s.Access.Access(ports.WithoutSpaceRequest(ctx), ports.Actor{UserID: s.agentUserID, Role: ports.RoleUser}, spaces.Resource{SpaceID: p.SpaceID, OwnerID: p.OwnerID, Kind: spaces.ProjectResource, ID: p.ID})
	if err != nil {
		var f *fault.Error
		if errors.Is(err, fault.ErrNotFound) || errors.As(err, &f) && (f.Code == "ACCOUNT_DISABLED" || f.Code == "ROLE_FORBIDDEN" || f.Code == "TEAM_READ_ONLY") {
			return fault.New(permissionChangedCode, "因权限变更停止")
		}
		return err
	}
	if permission < spaces.Edit {
		return fault.New(permissionChangedCode, "因权限变更停止")
	}
	return nil
}
func (s *Service) stopAgentForPermission(ctx context.Context, id string, err error) error {
	var f *fault.Error
	if !errors.As(err, &f) || f.Code != permissionChangedCode {
		return err
	}
	saveCtx, cancel := s.receiptContext(ctx)
	defer cancel()
	p, readErr := s.Store.Snapshot(saveCtx, id)
	if readErr != nil {
		return readErr
	}
	if r := p.LatestRun(); r != nil {
		if saveErr := s.fail(saveCtx, id, r.ID, permissionChangedCode, "因权限变更停止"); saveErr != nil {
			return saveErr
		}
	}
	return err
}

type agentProjects struct {
	ports.ProjectStore
	service *Service
}

func (s agentProjects) Snapshot(ctx context.Context, id string) (d.Project, error) {
	p, err := s.ProjectStore.Snapshot(ctx, id)
	if err == nil {
		err = s.service.checkAgentAccess(ctx, p)
	}
	if err != nil {
		return d.Project{}, err
	}
	return p, nil
}
func (s agentProjects) Commit(ctx context.Context, p d.Project, revision uint64) error {
	current, err := s.ProjectStore.Snapshot(ctx, p.ID)
	if err != nil {
		return err
	}
	if err = s.service.checkAgentAccess(ctx, current); err != nil {
		return err
	}
	return s.ProjectStore.Commit(ctx, p, revision)
}
func (s *Service) agentTools() *Service {
	copy := *s
	copy.Store = agentProjects{ProjectStore: s.Store, service: s}
	return &copy
}
