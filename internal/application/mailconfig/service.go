// Package mailconfig owns the single runtime SMTP configuration, never mail bodies.
package mailconfig

import (
	"context"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"unicode"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

const MaxRevision uint64 = ports.MaxSafeInteger

type Settings struct {
	Enabled  bool   `json:"enabled"`
	Address  string `json:"address"`
	Username string `json:"username"`
	From     string `json:"from"`
	TLS      string `json:"tls"`
}
type Snapshot struct {
	Revision uint64
	Settings
	PasswordCipher []byte `json:"-"`
}
type View struct {
	Revision uint64 `json:"revision"`
	Settings
	PasswordConfigured bool `json:"passwordConfigured"`
}
type Input struct {
	Revision uint64 `json:"revision"`
	Settings
	Password      string `json:"password,omitempty"`
	ClearPassword bool   `json:"clearPassword,omitempty"`
}
type Delivery struct {
	Settings
	Password string `json:"-"`
}
type Store interface {
	MailConfigSnapshot(context.Context) (Snapshot, error)
	CommitMailConfig(context.Context, uint64, Snapshot) error
}
type Sender interface {
	DeliverCode(context.Context, Delivery, string, string, string) error
}
type Service struct {
	Store  Store
	Cipher ports.CredentialCipher
	Sender Sender
}

func invalid() error {
	return fault.New("MAIL_CONFIGURATION_INVALID", "邮件配置无效，请核对连接、发送邮箱、TLS和密码设置")
}
func Validate(value Settings, hasPassword bool) (Settings, error) {
	value.Address = strings.TrimSpace(value.Address)
	value.Username = strings.TrimSpace(value.Username)
	value.From = strings.TrimSpace(value.From)
	value.TLS = strings.ToLower(strings.TrimSpace(value.TLS))
	for _, field := range []string{value.Address, value.Username, value.From} {
		if strings.ContainsFunc(field, unicode.IsControl) {
			return Settings{}, invalid()
		}
	}
	if len(value.Address) > 512 || len(value.Username) > 320 || len(value.From) > 254 || (value.TLS != "tls" && value.TLS != "starttls") {
		return Settings{}, invalid()
	}
	if value.Address != "" {
		host, port, err := net.SplitHostPort(value.Address)
		n, pe := strconv.Atoi(port)
		if err != nil || pe != nil || host == "" || n < 1 || n > 65535 || strings.ContainsAny(host, "/\\ @") {
			return Settings{}, invalid()
		}
	}
	if value.From != "" {
		address, err := mail.ParseAddress(value.From)
		if err != nil || address.Address != value.From {
			return Settings{}, invalid()
		}
	}
	if value.Enabled && (value.Address == "" || value.Username == "" || value.From == "" || !hasPassword) {
		return Settings{}, invalid()
	}
	return value, nil
}
func view(value Snapshot) View {
	return View{Revision: value.Revision, Settings: value.Settings, PasswordConfigured: len(value.PasswordCipher) > 0}
}
func (s *Service) Get(ctx context.Context, actor ports.Actor) (View, error) {
	if err := actor.RequireOperator(); err != nil {
		return View{}, err
	}
	value, err := s.Store.MailConfigSnapshot(ctx)
	if err != nil {
		return View{}, err
	}
	return view(value), nil
}
func (s *Service) Save(ctx context.Context, actor ports.Actor, input Input) (View, error) {
	if err := actor.RequireOperator(); err != nil {
		return View{}, err
	}
	if input.Revision >= MaxRevision || len(input.Password) > 4096 || strings.ContainsRune(input.Password, '\x00') || (input.ClearPassword && input.Password != "") {
		return View{}, invalid()
	}
	old, err := s.Store.MailConfigSnapshot(ctx)
	if err != nil {
		return View{}, err
	}
	if old.Revision != input.Revision {
		return View{}, fault.New("MAIL_CONFIGURATION_CONFLICT", "邮件配置已被修改，请重新读取后再保存")
	}
	next := Snapshot{Revision: old.Revision + 1, Settings: input.Settings, PasswordCipher: old.PasswordCipher}
	if input.ClearPassword {
		next.PasswordCipher = nil
	}
	if input.Password != "" {
		if s.Cipher == nil {
			return View{}, fault.New("MAIL_CONFIGURATION_UNAVAILABLE", "邮件凭证加密资源不可用")
		}
		next.PasswordCipher, err = s.Cipher.Seal(input.Password)
		if err != nil {
			return View{}, fault.New("MAIL_CONFIGURATION_UNAVAILABLE", "邮件凭证加密失败")
		}
	}
	next.Settings, err = Validate(next.Settings, len(next.PasswordCipher) > 0)
	if err != nil {
		return View{}, err
	}
	if err = s.Store.CommitMailConfig(ctx, old.Revision, next); err != nil {
		return View{}, err
	}
	return view(next), nil
}
func (s *Service) SendCode(ctx context.Context, to, purpose, code string) error {
	unavailable := func(stage string, cause error) error {
		return submissionFailure(stage, cause, "MAIL_UNAVAILABLE", "邮件服务暂不可用，请稍后重试或联系运营")
	}
	value, err := s.Store.MailConfigSnapshot(ctx)
	if err != nil {
		return unavailable("configuration-read", err)
	}
	if !value.Enabled {
		return unavailable("disabled", nil)
	}
	if len(value.PasswordCipher) == 0 || s.Cipher == nil || s.Sender == nil {
		return unavailable("resources", nil)
	}
	settings, err := Validate(value.Settings, true)
	if err != nil {
		return unavailable("configuration-invalid", err)
	}
	password, err := s.Cipher.Open(value.PasswordCipher)
	if err != nil || password == "" {
		return unavailable("decrypt", err)
	}
	if err = s.Sender.DeliverCode(ctx, Delivery{Settings: settings, Password: password}, to, purpose, code); err != nil {
		return submissionFailure("delivery", err, "MAIL_DELIVERY_FAILED", "邮件提交失败，请稍后重试")
	}
	return nil
}

// ImportLegacy 是来自私有部署文件的受控一次性迁移；绝不覆盖已独立配置的数据库连接。
func (s *Service) ImportLegacy(ctx context.Context, settings Settings, password string) (View, error) {
	settings, err := Validate(settings, password != "")
	if err != nil || password == "" || len(password) > 4096 {
		return View{}, invalid()
	}
	if s.Cipher == nil {
		return View{}, fault.New("MAIL_CONFIGURATION_UNAVAILABLE", "邮件凭证加密资源不可用")
	}
	current, err := s.Store.MailConfigSnapshot(ctx)
	if err != nil {
		return View{}, err
	}
	if current.Revision == 0 && len(current.PasswordCipher) == 0 {
		secret, err := s.Cipher.Seal(password)
		if err != nil {
			return View{}, fault.New("MAIL_CONFIGURATION_UNAVAILABLE", "邮件凭证加密失败")
		}
		if err = s.Store.CommitMailConfig(ctx, 0, Snapshot{Revision: 1, Settings: settings, PasswordCipher: secret}); err != nil {
			return View{}, err
		}
		current, err = s.Store.MailConfigSnapshot(ctx)
		if err != nil {
			return View{}, err
		}
	}
	actual, err := s.Cipher.Open(current.PasswordCipher)
	if err != nil || actual != password || current.Settings != settings {
		return View{}, fault.New("MAIL_CONFIGURATION_CONFLICT", "已有邮件配置与迁移输入不一致，未覆盖或移除原文件")
	}
	return view(current), nil
}
