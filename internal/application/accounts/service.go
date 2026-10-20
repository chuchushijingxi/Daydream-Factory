// Package accounts owns identities and a single current session, never resource permissions.
package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"drama-agent/internal/application/ports"
	domain "drama-agent/internal/domain/accounts"
	"drama-agent/internal/domain/fault"
	"errors"
)

const InitialOperatorEmail = domain.InitialOperatorEmail
const InitialOperatorUsername = domain.InitialOperatorUsername

type User = domain.User
type AuthorizationCode = domain.AuthorizationCode
type Store interface {
	ports.SessionAuthenticator
	RegistrationRequired(context.Context) (bool, error)
	PutVerification(context.Context, string, string, string, string, time.Time) error
	ActivateVerification(context.Context, string, bool) error
	RegistrationStore
	Login(context.Context, LoginCredentials) (User, error)
	ResetPassword(context.Context, string, string, []byte) error
	CurrentUser(context.Context, string) (User, error)
	Profile(context.Context, string) (Profile, error)
	UpdateUsername(context.Context, string, string) error
	SetAvatar(context.Context, string, []byte, string) error
	Avatar(context.Context, string) ([]byte, error)
	ChangePassword(context.Context, string, string, []byte, []byte) error
	Logout(context.Context, string) error
	InitializeOperator(context.Context, string, []byte) (User, bool, error)
	ListUsers(context.Context, string, int, int) (ports.Page[User], error)
	SetUserEnabled(context.Context, string, bool) (User, error)
	SetRegistrationRequired(context.Context, bool) error
	CreateAuthorizationCode(context.Context, string, string, time.Time) (AuthorizationCode, error)
	ListAuthorizationCodes(context.Context, int, int) (ports.Page[AuthorizationCode], error)
	DisableAuthorizationCode(context.Context, string) error
}
type Mailer interface {
	SendCode(context.Context, string, string, string) error
}
type PasswordHasher interface{ Hash([]byte) ([]byte, error) }
type Service struct {
	Hasher     PasswordHasher
	Store      Store
	Thumbnails ports.ImageThumbnailer
	Mailer     Mailer
}

func Digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func RandomToken() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func Email(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, e := mail.ParseAddress(s)
	if e != nil || a.Address != s || len(s) > 254 || strings.ContainsAny(s, "\r\n\x00") {
		return "", fault.ErrInvalid
	}
	return s, nil
}
func (service *Service) password(s string) ([]byte, error) {
	if len(s) < 12 || len(s) > 72 {
		return nil, fault.New("INVALID_PASSWORD", "密码须为12至72字节")
	}
	return service.hashPassword(s)
}
func (service *Service) hashPassword(s string) ([]byte, error) {
	if service.Hasher == nil {
		return nil, fault.New("AUTH_UNAVAILABLE", "密码散列资源不可用")
	}
	return service.Hasher.Hash([]byte(s))
}
func (s *Service) Authenticate(ctx context.Context, token string) (ports.Actor, error) {
	return s.Store.Authenticate(ctx, token)
}
func (s *Service) SendCode(ctx context.Context, email, purpose string) error {
	email, e := Email(email)
	if e != nil {
		return e
	}
	if purpose != "register" && purpose != "login" && purpose != "reset" {
		return fault.ErrInvalid
	}
	if s.Mailer == nil {
		return fault.New("MAIL_UNAVAILABLE", "邮件服务尚未配置")
	}
	n, e := rand.Int(rand.Reader, big.NewInt(900000))
	if e != nil {
		return e
	}
	code := big.NewInt(100000).Add(n, big.NewInt(100000)).String()
	id := RandomToken()
	if e = s.Store.PutVerification(ctx, id, email, purpose, Digest(id+":"+code), time.Now().UTC().Add(10*time.Minute)); e != nil {
		return e
	}
	sent := s.Mailer.SendCode(ctx, email, purpose, code)
	if e = s.Store.ActivateVerification(context.WithoutCancel(ctx), id, sent == nil); e != nil {
		return e
	}
	if sent != nil {
		var unavailable *fault.Error
		if errors.As(sent, &unavailable) && (unavailable.Code == "MAIL_UNAVAILABLE" || unavailable.Code == "MAIL_DELIVERY_FAILED") {
			return sent
		}
		return fault.New("MAIL_DELIVERY_FAILED", "邮件提交失败，请稍后重试")
	}
	return nil
}
func (s *Service) Register(ctx context.Context, email, username, password, code, authorization string) (User, error) {
	email, e := Email(email)
	if e != nil {
		return User{}, e
	}
	if e = domain.AllowPublicRegistration(email); e != nil {
		return User{}, e
	}
	username, e = Username(username)
	if e != nil {
		return User{}, e
	}
	hash, e := s.password(password)
	if e != nil {
		return User{}, e
	}
	return Register(ctx, s.Store, Registration{ID: RandomToken(), Email: email, Username: username, Code: code, AuthorizationCode: authorization, PasswordHash: hash})
}

type LoginCredentials struct {
	Email         string
	Password      []byte
	Code          string
	Audience      string
	SessionDigest string
	ExpiresAt     time.Time
}

type LoginResult struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

func (s *Service) Login(ctx context.Context, email, password, code, audience string) (LoginResult, error) {
	// 仅引导账号有固定的密码登录别名，显示名仍不作为普通用户的登录身份。
	if password != "" && code == "" && strings.EqualFold(strings.TrimSpace(email), InitialOperatorUsername) {
		email = InitialOperatorEmail
	}
	email, e := Email(email)
	if e != nil {
		return LoginResult{}, fault.New("INVALID_CREDENTIALS", "账号或凭证不正确")
	}
	if !ValidAudience(audience) {
		return LoginResult{}, fault.ErrInvalid
	}
	if (password == "") == (code == "") {
		return LoginResult{}, fault.ErrInvalid
	}
	secret := RandomToken()
	until := time.Now().UTC().Add(7 * 24 * time.Hour)
	user, e := s.Store.Login(ctx, LoginCredentials{Email: email, Password: []byte(password), Code: code, Audience: audience, SessionDigest: Digest(audience + ":" + secret), ExpiresAt: until})
	if e != nil {
		return LoginResult{}, e
	}
	return LoginResult{user, user.ID + "." + audience + "." + secret, until}, nil
}
func (s *Service) ResetPassword(ctx context.Context, email, code, password string) error {
	email, e := Email(email)
	if e != nil {
		return e
	}
	hash, e := s.password(password)
	if e != nil {
		return e
	}
	return s.Store.ResetPassword(ctx, email, code, hash)
}
func (s *Service) Current(ctx context.Context, a ports.Actor) (User, error) {
	if e := a.Require(); e != nil {
		return User{}, e
	}
	return s.Store.CurrentUser(ctx, a.UserID)
}
func (s *Service) InitializeOperator(ctx context.Context, password string) (User, bool, error) {
	// 这个受控的非 HTTP 入口接受另行提供的引导密钥；公开注册与找回仍用各自的密码下限。
	if len(password) < 6 || len(password) > 72 {
		return User{}, false, fault.New("INVALID_PASSWORD", "初始化密码须为6至72字节")
	}
	hash, e := s.hashPassword(password)
	if e != nil {
		return User{}, false, e
	}
	return s.Store.InitializeOperator(ctx, RandomToken(), hash)
}
func (s *Service) Users(ctx context.Context, a ports.Actor, q string, page, size int) (ports.Page[User], error) {
	if e := a.RequireOperator(); e != nil {
		return ports.Page[User]{}, e
	}
	if e := ports.CheckPage(page, size); e != nil {
		return ports.Page[User]{}, e
	}
	return s.Store.ListUsers(ctx, q, page, size)
}
func (s *Service) SetEnabled(ctx context.Context, a ports.Actor, id string, enabled bool) (User, error) {
	if e := a.RequireOperator(); e != nil {
		return User{}, e
	}
	if id == a.UserID && !enabled {
		return User{}, fault.New("SELF_DISABLE_FORBIDDEN", "不能禁用当前运营账号")
	}
	return s.Store.SetUserEnabled(ctx, id, enabled)
}
func (s *Service) SetRegistration(ctx context.Context, a ports.Actor, required bool) error {
	if e := a.RequireOperator(); e != nil {
		return e
	}
	return s.Store.SetRegistrationRequired(ctx, required)
}
func (s *Service) CreateCode(ctx context.Context, a ports.Actor, expires time.Time) (AuthorizationCode, string, error) {
	if e := a.RequireOperator(); e != nil {
		return AuthorizationCode{}, "", e
	}
	if !expires.After(time.Now()) || expires.After(time.Now().Add(365*24*time.Hour)) {
		return AuthorizationCode{}, "", fault.ErrInvalid
	}
	raw := RandomToken()
	v, e := s.Store.CreateAuthorizationCode(ctx, RandomToken(), Digest(raw), expires)
	return v, raw, e
}
func (s *Service) Codes(ctx context.Context, a ports.Actor, page, size int) (ports.Page[AuthorizationCode], error) {
	if e := a.RequireOperator(); e != nil {
		return ports.Page[AuthorizationCode]{}, e
	}
	if e := ports.CheckPage(page, size); e != nil {
		return ports.Page[AuthorizationCode]{}, e
	}
	return s.Store.ListAuthorizationCodes(ctx, page, size)
}
func (s *Service) DisableCode(ctx context.Context, a ports.Actor, id string) error {
	if e := a.RequireOperator(); e != nil {
		return e
	}
	return s.Store.DisableAuthorizationCode(ctx, id)
}

func (s *Service) Logout(ctx context.Context, a ports.Actor, token string) error {
	if e := a.Require(); e != nil {
		return e
	}
	id, _, _, err := SessionParts(token)
	if err != nil || id != a.UserID {
		return fault.New("AUTH_REQUIRED", "会话身份不一致")
	}
	return s.Store.Logout(ctx, token)
}
