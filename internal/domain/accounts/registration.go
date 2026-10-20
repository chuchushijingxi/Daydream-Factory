package accounts

import (
	"time"

	"drama-agent/internal/domain/fault"
)

// 引导账号不绑定可投递的个人邮箱；普通用户仍使用已验证邮箱登录。
const InitialOperatorEmail = "daydream@localhost.invalid"
const InitialOperatorUsername = "Daydream"

func AllowPublicRegistration(email string) error {
	if email == InitialOperatorEmail {
		return fault.New("REGISTRATION_FORBIDDEN", "该身份不可公开注册")
	}
	return nil
}

type AuthorizationCode struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	Disabled  bool       `json:"disabled"`
	UsedBy    *string    `json:"usedBy"`
	UsedAt    *time.Time `json:"usedAt"`
}

func (c AuthorizationCode) RequireAvailable(at time.Time) error {
	if c.Disabled || c.UsedBy != nil || !at.Before(c.ExpiresAt) {
		return fault.New("AUTHORIZATION_CODE_INVALID", "注册授权码无效")
	}
	return nil
}
