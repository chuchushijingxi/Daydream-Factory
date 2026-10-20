package modelconfig

import (
	"context"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/logoimage"
)

type LogoView struct {
	LogoID  string `json:"logoId"`
	LogoURL string `json:"logoUrl"`
}

func validLogoID(id string) bool { return logoimage.ValidID(id) }

func invalidLogo() error {
	return fault.New("INVALID_MODEL_LOGO", "请选择不超过2MB的PNG或JPEG图片（最大4096×4096），或恢复默认Logo")
}

// 上传生成不可变本地资源；只有后续配置 CAS 保存才绑定到模型。
// 取消或恢复默认不修改其他模型共享的图片。
func (c *Service) uploadLogo(ctx context.Context, raw []byte) (LogoView, error) {
	if c.ReadOnly {
		return LogoView{}, fault.New("SERVICE_READ_ONLY", "当前服务只读")
	}
	if c.Logos == nil {
		return LogoView{}, Unavailable()
	}
	id, png, e := logoimage.Normalize(raw)
	if e != nil {
		return LogoView{}, invalidLogo()
	}
	if e = c.Logos.SaveModelLogo(ctx, id, png); e != nil {
		return LogoView{}, e
	}
	return LogoView{LogoID: id, LogoURL: "/api/model-logos/" + id}, nil
}

func (c *Service) readLogo(ctx context.Context, id string) ([]byte, error) {
	if !validLogoID(id) {
		return nil, fault.ErrNotFound
	}
	if c.Logos == nil {
		return nil, Unavailable()
	}
	return c.Logos.ModelLogo(ctx, id)
}
