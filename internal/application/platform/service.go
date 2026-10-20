package platform

import (
	"context"
	"errors"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/logoimage"
	d "drama-agent/internal/domain/platform"
)

type Snapshot struct {
	Revision uint64 `json:"revision"`
	d.Settings
}
type View struct {
	Snapshot
	LogoURL string `json:"logoUrl"`
}
type Input = Snapshot
type LogoView struct {
	LogoID  string `json:"logoId"`
	LogoURL string `json:"logoUrl"`
}
type Store interface {
	PlatformSettings(context.Context) (Snapshot, error)
	CommitPlatformSettings(context.Context, uint64, d.Settings) (Snapshot, error)
	SavePlatformLogo(context.Context, string, []byte) error
	PlatformLogo(context.Context, string) ([]byte, error)
}
type Service struct {
	Store    Store
	ReadOnly bool
}

func unavailable() error {
	return fault.New("PLATFORM_SETTINGS_UNAVAILABLE", "平台设置暂不可用")
}
func logoURL(id string) string {
	if id == "" {
		return ""
	}
	return "/api/platform-logos/" + id
}
func (s *Service) Get(ctx context.Context) (View, error) {
	if s.Store == nil {
		return View{}, unavailable()
	}
	v, err := s.Store.PlatformSettings(ctx)
	if err != nil {
		return View{}, err
	}
	return View{Snapshot: v, LogoURL: logoURL(v.LogoID)}, nil
}
func (s *Service) authorizeWrite(actor ports.Actor) error {
	if err := actor.RequireOperator(); err != nil {
		return err
	}
	if s.ReadOnly {
		return fault.New("SERVICE_READ_ONLY", "当前服务只读")
	}
	if s.Store == nil {
		return unavailable()
	}
	return nil
}
func (s *Service) Save(ctx context.Context, actor ports.Actor, input Input) (View, error) {
	if err := s.authorizeWrite(actor); err != nil {
		return View{}, err
	}
	if input.Revision >= ports.MaxSafeInteger {
		return View{}, fault.New("PLATFORM_SETTINGS_INVALID", "平台配置版本超出安全整数范围")
	}
	v, err := d.Validate(input.Settings)
	if err != nil {
		return View{}, err
	}
	if v.LogoID != "" {
		if _, err = s.Store.PlatformLogo(ctx, v.LogoID); errors.Is(err, fault.ErrNotFound) {
			return View{}, fault.New("INVALID_PLATFORM_LOGO", "请先上传平台Logo，再保存设置")
		} else if err != nil {
			return View{}, err
		}
	}
	next, err := s.Store.CommitPlatformSettings(ctx, input.Revision, v)
	if err != nil {
		return View{}, err
	}
	return View{Snapshot: next, LogoURL: logoURL(next.LogoID)}, nil
}
func (s *Service) UploadLogo(ctx context.Context, actor ports.Actor, raw []byte) (LogoView, error) {
	if err := s.authorizeWrite(actor); err != nil {
		return LogoView{}, err
	}
	id, png, err := logoimage.Normalize(raw)
	if err != nil {
		return LogoView{}, fault.New("INVALID_PLATFORM_LOGO", "请选择不超过2MiB的PNG或JPEG图片，尺寸最大4096×4096")
	}
	if err = s.Store.SavePlatformLogo(ctx, id, png); err != nil {
		return LogoView{}, err
	}
	return LogoView{LogoID: id, LogoURL: logoURL(id)}, nil
}
func (s *Service) ReadLogo(ctx context.Context, id string) ([]byte, error) {
	if !logoimage.ValidID(id) {
		return nil, fault.ErrNotFound
	}
	if s.Store == nil {
		return nil, unavailable()
	}
	return s.Store.PlatformLogo(ctx, id)
}
