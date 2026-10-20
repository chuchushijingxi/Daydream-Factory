package accounts

import (
	"bytes"
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

type Profile struct {
	User      User    `json:"user"`
	AvatarURL *string `json:"avatarUrl"`
}

const MaxAvatarBytes = 5 << 20

func (s *Service) Profile(ctx context.Context, actor ports.Actor) (Profile, error) {
	if err := actor.Require(); err != nil {
		return Profile{}, err
	}
	return s.Store.Profile(ctx, actor.UserID)
}

func (s *Service) UpdateProfile(ctx context.Context, actor ports.Actor, username string) (Profile, error) {
	if err := actor.Require(); err != nil {
		return Profile{}, err
	}
	username, err := Username(username)
	if err != nil {
		return Profile{}, err
	}
	if err := s.Store.UpdateUsername(ctx, actor.UserID, username); err != nil {
		return Profile{}, err
	}
	return s.Profile(ctx, actor)
}

func (s *Service) SetAvatar(ctx context.Context, actor ports.Actor, raw []byte) (Profile, error) {
	if err := actor.Require(); err != nil {
		return Profile{}, err
	}
	if len(raw) == 0 || len(raw) > MaxAvatarBytes {
		return Profile{}, fault.New("INVALID_AVATAR", "请选择5MB以内的JPEG、PNG或WebP图片")
	}
	switch avatarImageType(raw) {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return Profile{}, fault.New("INVALID_AVATAR", "头像仅支持JPEG、PNG或WebP图片")
	}
	if s.Thumbnails == nil {
		return Profile{}, fault.New("THUMBNAIL_UNAVAILABLE", "头像处理资源不可用")
	}
	image, err := s.Thumbnails.Generate(ctx, raw, ports.ThumbnailOptions{MaxWidth: 256, MaxHeight: 256})
	if err != nil {
		return Profile{}, err
	}
	if len(image.Data) == 0 || len(image.Data) > 512<<10 || image.Width < 1 || image.Height < 1 || image.Width > 256 || image.Height > 256 || avatarImageType(image.Data) != "image/webp" {
		return Profile{}, fault.New("INVALID_AVATAR", "头像处理结果无效")
	}
	if err = s.Store.SetAvatar(ctx, actor.UserID, image.Data, Digest(string(image.Data))); err != nil {
		return Profile{}, err
	}
	return s.Profile(ctx, actor)
}

func (s *Service) RemoveAvatar(ctx context.Context, actor ports.Actor) (Profile, error) {
	if err := actor.Require(); err != nil {
		return Profile{}, err
	}
	if err := s.Store.SetAvatar(ctx, actor.UserID, nil, ""); err != nil {
		return Profile{}, err
	}
	return s.Profile(ctx, actor)
}

func (s *Service) Avatar(ctx context.Context, actor ports.Actor) ([]byte, error) {
	if err := actor.Require(); err != nil {
		return nil, err
	}
	return s.Store.Avatar(ctx, actor.UserID)
}

func (s *Service) ChangePassword(ctx context.Context, actor ports.Actor, token, current, password string) error {
	if err := actor.Require(); err != nil {
		return err
	}
	id, _, _, err := SessionParts(token)
	if err != nil || id != actor.UserID {
		return fault.New("AUTH_REQUIRED", "会话身份不一致")
	}
	if len(current) == 0 || len(current) > 72 {
		return fault.New("CURRENT_PASSWORD_INVALID", "原密码不正确")
	}
	hash, err := s.password(password)
	if err != nil {
		return err
	}
	return s.Store.ChangePassword(ctx, actor.UserID, token, []byte(current), hash)
}

// Username 只是展示名，不是认证身份或唯一键。
func Username(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 40 || strings.ContainsFunc(value, unicode.IsControl) {
		return "", fault.New("INVALID_USERNAME", "用户名须为1至40个字符，不能包含控制字符")
	}
	return value, nil
}

// 签名只选择受支持的格式；持久化前仍由共用图片适配器做真实解码、尺寸限制与重编码。
func avatarImageType(raw []byte) string {
	if bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")) {
		return "image/png"
	}
	if bytes.HasPrefix(raw, []byte{0xff, 0xd8, 0xff}) {
		return "image/jpeg"
	}
	if len(raw) >= 12 && bytes.Equal(raw[:4], []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")) {
		return "image/webp"
	}
	return ""
}
