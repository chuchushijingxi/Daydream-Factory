package ports

import (
	"context"
	d "drama-agent/internal/domain/project"
)

// OriginalScripts 读取保留的历史剧本对象；新上传一律走独立上传模块，
// 既有坐标不做迁移。
type OriginalScripts interface {
	Read(context.Context, string, d.OriginalFile) ([]byte, error)
}
