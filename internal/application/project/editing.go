package project

import (
	"context"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
)

type EditingView struct {
	UserID    string     `json:"userId"`
	Username  string     `json:"username"`
	ExpiresAt *time.Time `json:"expiresAt"`
	Owned     bool       `json:"owned"`
}

// WithEditingSession只接受已认证HTTP会话的稳定摘要；它不授予内容权限。
func (s *Service) WithEditingSession(sessionID string) *Service {
	copy := *s
	copy.editingSession = sessionID
	if scoped, ok := copy.Store.(scopedProjects); ok {
		scoped.service = &copy
		copy.Store = scoped
	}
	return &copy
}
func (s *Service) editingIdentity() (string, string, error) {
	if s.scope == nil || s.scope.UserID == "" || s.editingSession == "" {
		return "", "", fault.New("AUTH_REQUIRED", "缺少已认证编辑会话")
	}
	if s.Editing == nil {
		return "", "", fault.New("RUNTIME_UNAVAILABLE", "项目编辑协调尚未组装")
	}
	return s.scope.UserID, s.editingSession, nil
}
func (s *Service) requireEditing(ctx context.Context, p d.Project) error {
	if s.scope == nil {
		return nil
	} // 已持久化worker内部写入不占用页面编辑权。
	user, session, err := s.editingIdentity()
	if err != nil {
		return err
	}
	return s.Editing.CheckProjectEditing(ctx, p.ID, user, session)
}
func (s *Service) editingResource(ctx context.Context, id string) (d.Project, error) {
	p, err := s.Project(ctx, id)
	if err == nil {
		err = s.requireProject(ctx, p, spaces.Edit)
	}
	return p, err
}
func (s *Service) editingView(ctx context.Context, owner ports.ProjectEditor) (EditingView, error) {
	out := EditingView{UserID: owner.UserID, Owned: owner.UserID != "" && owner.UserID == s.scope.UserID && owner.SessionID == s.editingSession}
	if owner.UserID == "" {
		return out, nil
	}
	out.ExpiresAt = &owner.ExpiresAt
	if s.EditorNames == nil {
		return out, fault.New("RUNTIME_UNAVAILABLE", "项目占用者查询尚未组装")
	}
	name, err := s.EditorNames.ProjectEditorName(ctx, owner.UserID)
	out.Username = name
	return out, err
}
func (s *Service) AcquireEditing(ctx context.Context, id string) (EditingView, error) {
	if _, err := s.editingResource(ctx, id); err != nil {
		return EditingView{}, err
	}
	user, session, err := s.editingIdentity()
	if err != nil {
		return EditingView{}, err
	}
	owner, err := s.Editing.AcquireProjectEditing(ctx, id, user, session)
	if err != nil {
		return EditingView{}, err
	}
	return s.editingView(ctx, owner)
}
func (s *Service) RenewEditing(ctx context.Context, id string) (EditingView, error) {
	if _, err := s.editingResource(ctx, id); err != nil {
		return EditingView{}, err
	}
	user, session, err := s.editingIdentity()
	if err != nil {
		return EditingView{}, err
	}
	owner, err := s.Editing.RenewProjectEditing(ctx, id, user, session)
	if err != nil {
		return EditingView{}, err
	}
	return s.editingView(ctx, owner)
}
func (s *Service) ReleaseEditing(ctx context.Context, id string) error {
	// 释放只删除匹配当前认证user/session的租约，不返回资源信息；撤销共享后也应能离页释放。
	// 不用项目读取权限阻挡此清理，更不能由任意客户端session释放别人的占用。
	user, session, err := s.editingIdentity()
	if err != nil {
		return err
	}
	return s.Editing.ReleaseProjectEditing(ctx, id, user, session)
}
