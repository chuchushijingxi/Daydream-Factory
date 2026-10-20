package ports

import (
	"context"
	"drama-agent/internal/domain/accounts"
	"drama-agent/internal/domain/fault"
)

type Role = accounts.Role

const (
	RoleUser     = accounts.RoleUser
	RoleOperator = accounts.RoleOperator
)

// Actor 是适配器认证后提供的可信值，绝不是请求 DTO。
type Actor struct {
	UserID string
	Role   Role
}

func (a Actor) Require() error {
	if a.UserID == "" || (a.Role != RoleUser && a.Role != RoleOperator) {
		return fault.New("AUTH_REQUIRED", "请先登录")
	}
	return nil
}
func (a Actor) RequireOperator() error {
	if err := a.Require(); err != nil {
		return err
	}
	if a.Role != RoleOperator {
		return fault.New("ROLE_FORBIDDEN", "需要运营权限")
	}
	return nil
}

type SessionAuthenticator interface {
	Authenticate(context.Context, string) (Actor, error)
}
