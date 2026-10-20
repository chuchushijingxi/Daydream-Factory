package modelconfig

import (
	"math"
	"slices"
)

// MediaProfile 是程序维护的官方接口能力，与运营积分定价无关；中转站自定义 ID 不继承官方能力。
type MediaProfile struct {
	SupplierPrice     string            `json:"supplierPrice"`
	Resolutions       []MediaResolution `json:"resolutions"`
	DefaultResolution string            `json:"defaultResolution"`
	Durations         []float64         `json:"durations,omitempty"`
	DefaultDuration   float64           `json:"defaultDuration,omitempty"`
	AspectRatios      []string          `json:"aspectRatios"`
	Note              string            `json:"note"`
	Sources           []string          `json:"sources"`
	CheckedAt         string            `json:"checkedAt"`
	Retired           bool              `json:"retired"`
}
type MediaResolution struct {
	ID     string            `json:"id"`
	Label  string            `json:"label"`
	Pixels map[string]string `json:"pixels,omitempty"`
}
type MediaOptions struct {
	Resolution      string  `json:"resolution"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
}

func (p *MediaProfile) Supports(in MediaOptions) bool {
	if p == nil || p.Retired || math.IsNaN(in.DurationSeconds) || math.IsInf(in.DurationSeconds, 0) {
		return false
	}
	return slices.ContainsFunc(p.Resolutions, func(r MediaResolution) bool { return r.ID == in.Resolution }) && (len(p.Durations) == 0 && in.DurationSeconds == 0 || slices.Contains(p.Durations, in.DurationSeconds))
}
func (p *MediaProfile) Defaults() MediaOptions {
	if p == nil {
		return MediaOptions{}
	}
	return MediaOptions{Resolution: p.DefaultResolution, DurationSeconds: p.DefaultDuration}
}
func (p *MediaProfile) HasResolution(id string) bool {
	return p != nil && slices.ContainsFunc(p.Resolutions, func(r MediaResolution) bool { return r.ID == id })
}
func seconds(min, max int) []float64 {
	out := []float64{}
	for n := min; n <= max; n++ {
		out = append(out, float64(n))
	}
	return out
}
func resolutions(ids ...string) []MediaResolution {
	out := []MediaResolution{}
	for _, id := range ids {
		out = append(out, MediaResolution{ID: id, Label: id})
	}
	return out
}

func OfficialMediaProfile(connection, model string) *MediaProfile {
	p := &MediaProfile{CheckedAt: "2026-10-17", AspectRatios: []string{"1:1", "4:3", "3:4", "16:9", "9:16", "3:2", "2:3", "21:9"}}
	switch connection {
	case "openai":
		if model != "gpt-image-2" && model != "gpt-image-2.5-sunburst" && model != "gpt-image-2.5-flare" {
			return nil
		}
		// 这些是官方支持的显式尺寸，不是虚构的 4096 方形「4K」。
		p.Resolutions = resolutions("1024x1024", "1536x1024", "1024x1536", "2048x2048", "2048x1152", "1152x2048", "3840x2160", "2160x3840")
		p.DefaultResolution = "1024x1024"
		p.Note = "支持自定义宽×高：16 的倍数，单边≤3840，总像素 655360–8294400，宽高比 1:3–3:1；高于 3686400 像素为实验能力。此处列常用固定尺寸。成本取决于像素、质量及输入/输出 token，不能仅按分辨率推算。"
		p.Sources = []string{"https://developers.openai.com/api/reference/resources/images/methods/generate", "https://developers.openai.com/api/docs/guides/image-generation", "https://platform.openai.com/pricing"}
	case "google":
		p.Sources = []string{"https://ai.google.dev/gemini-api/docs/image-generation", "https://ai.google.dev/gemini-api/docs/pricing"}
		p.DefaultResolution = "1K"
		p.Resolutions = resolutions("1K", "2K", "4K")
		switch model {
		case "gemini-nano-banana-2.1":
			p.Note = "1K / 2K / 4K；Google 规格表与价格页的 4K token 数存在差异，供应商费用须以实际用量核对。"
		case "gemini-3-pro-image":
			p.Note = "1K / 2K 同一输出价，4K 另一档；另计输入、文本与思考 token。"
		case "gemini-3.1-flash-image":
			p.Resolutions = resolutions("512", "1K", "2K", "4K")
			p.Note = "512px / 1K / 2K / 4K，分档输出价格；另计输入与文本 token。"
		case "gemini-3.1-flash-lite-image":
			p.Resolutions = resolutions("1K")
			p.Note = "仅支持 1K。"
		case "gemini-2.5-flash-image":
			p.Resolutions = resolutions("1K")
			p.Retired = true
			p.Note = "官方已于 2026-10-02 关闭；保留历史身份，请改选当前型号。"
		default:
			return nil
		}
	case "volcengine":
		p.Sources = []string{"https://docs.volcengine.com/docs/ark/image-generation-api?lang=zh", "https://docs.volcengine.com/docs/ark/create-video-generation-task-api?lang=zh", "https://docs.volcengine.com/docs/ark/model-pricing?lang=zh"}
		switch model {
		case "doubao-seedream-5-0-pro-260628", "doubao-seedream-5-0-flash-260915":
			p.Resolutions = resolutions("1K", "1.5K", "2K")
			p.DefaultResolution = "2K"
			p.Note = "图片生成档位仅 1K / 1.5K / 2K；也支持显式像素，总像素 921600–4624220，宽高比 1:16–16:1。Pro 在 261 万像素上下分价；Flash 统一按张。"
		case SeedreamLiteModelID:
			p.Resolutions = resolutions("2K", "3K", "4K")
			p.DefaultResolution = "2K"
			p.Note = "支持 2K / 3K / 4K，默认 2K；显式尺寸总像素 3686400–16777216，宽高比 1:16–16:1。官方已于 2026-09-24 停止新购，2026-11-24 14:00（UTC+8）关停；已有开通仍以厂商实际权限为准。"
			p.Sources = []string{"https://docs.volcengine.com/docs/ark/seedream-4-0-5-0?lang=zh", "https://docs.volcengine.com/docs/ark/image-generation-api?lang=zh", "https://docs.volcengine.com/docs/ark/model-pricing?lang=zh", "https://docs.volcengine.com/docs/ark/model-deprecation-notice?lang=zh"}
		case "doubao-seedance-2-5-260628", "doubao-seedance-2-0-260128", "doubao-seedance-2-0-fast-260128", "doubao-seedance-2-0-mini-260615":
			p.Resolutions = resolutions("480p", "720p")
			p.DefaultResolution = "720p"
			p.DefaultDuration = 5
			p.Durations = seconds(4, 15)
			p.AspectRatios = []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}
			if model == "doubao-seedance-2-5-260628" {
				p.Resolutions = append(p.Resolutions, resolutions("1080p")...)
				p.Durations = seconds(4, 30)
			}
			if model == "doubao-seedance-2-0-260128" {
				p.Resolutions = append(p.Resolutions, resolutions("1080p", "4k")...)
			}
			p.Note = "明确时长按整数秒；官方另支持智能时长 -1。视频编辑有单独输入/时长限制。供应商按真实视频 token 计费，受输入视频、帧率、像素及最低用量影响，每秒示例价不是固定账单价。"
		default:
			return nil
		}
	case "alibaba":
		p.Sources = []string{"https://help.aliyun.com/zh/model-studio/wan-image-generation-and-editing-api-reference", "https://help.aliyun.com/zh/model-studio/video-generate-edit-model", "https://help.aliyun.com/zh/model-studio/model-pricing"}
		switch model {
		case "wan2.7-image", "wan2.7-image-pro":
			p.Resolutions = resolutions("1K", "2K")
			p.DefaultResolution = "2K"
			if model == "wan2.7-image-pro" {
				p.Resolutions = append(p.Resolutions, resolutions("4K")...)
			}
			p.Note = "按成功输出张数统一计费；Pro 的 4K 仅限无图片输入、非组图的文生图。支持显式像素与宽高比 1:8–8:1。档位模式无参考图时输出正方形。"
		case "happyhorse-1.1-t2v", "happyhorse-1.1-i2v", "happyhorse-1.1-r2v", "happyhorse-1.0-video-edit":
			p.Resolutions = resolutions("480p", "720p", "1080p")
			p.DefaultResolution = "1080p"
			p.Durations = seconds(3, 15)
			p.DefaultDuration = 5
			p.AspectRatios = []string{"16:9", "9:16", "1:1", "4:3", "3:4", "4:5", "5:4", "9:21", "21:9"}
			p.Note = "北京地域按分辨率收取输出视频每秒费用。"
			if model == "happyhorse-1.0-video-edit" {
				p.Resolutions = resolutions("720p", "1080p")
				p.Note = "编辑输出时长跟随输入视频（3–15 秒），不能任意指定；输入和输出时长均计费。"
				p.Durations = nil
				p.DefaultDuration = 0
			}
		default:
			return nil
		}
	case "kling":
		p.Sources = []string{"https://kling.ai/document-api/guides/capability-map/video", "https://kling.ai/document-api/pricing/base/image", "https://kling.ai/document-api/pricing/base/video"}
		switch model {
		case "kling-v3", "kling-v3-omni":
			p.Resolutions = resolutions("1K", "2K")
			p.DefaultResolution = "1K"
			if model == "kling-v3-omni" {
				p.Resolutions = append(p.Resolutions, resolutions("4K")...)
			}
			p.Note = "3.0 及 Omni 的 1K / 2K 同价；仅 Omni 支持 4K，按另一档计费。"
		case "kling-3.0", "kling-3.0-omni", "kling-3.0-turbo":
			p.Resolutions = resolutions("720p", "1080p")
			if model != "kling-3.0-turbo" {
				p.Resolutions = append(p.Resolutions, resolutions("4k")...)
			}
			p.DefaultResolution = "720p"
			p.DefaultDuration = 5
			p.Durations = seconds(3, 15)
			p.AspectRatios = []string{"16:9", "9:16", "1:1"}
			p.Note = "按分辨率与秒数计费；原生音频、视频输入会影响官方单价。Turbo 不支持 4K。"
		default:
			return nil
		}
	case "mock":
		p.Sources = []string{}
		p.Note = "本地测试，供应商成本为 0；用户积分由运营设置。"
		switch model {
		case MockImageModelID:
			p.Resolutions = resolutions("1K", "2K", "4K")
			p.DefaultResolution = "1K"
		case MockVideoModelID:
			p.Resolutions = resolutions("480p", "720p", "1080p")
			p.DefaultResolution = "1080p"
			p.AspectRatios = []string{"16:9"}
			p.Durations = seconds(1, 10)
			p.DefaultDuration = 10
			p.Note += "视频固定 16:9，按所选分辨率和 1–10 整数秒实际编码，按所选秒数计用户积分。"
		default:
			return nil
		}
	default:
		return nil
	}
	for i, r := range p.Resolutions {
		switch r.ID {
		case "720p":
			p.Resolutions[i].Pixels = map[string]string{"16:9": "1280×720", "9:16": "720×1280"}
		case "1080p":
			p.Resolutions[i].Pixels = map[string]string{"16:9": "1920×1080", "9:16": "1080×1920"}
		case "4k":
			p.Resolutions[i].Pixels = map[string]string{"16:9": "3840×2160", "9:16": "2160×3840"}
		case "480p":
			if connection == "volcengine" {
				if model == "doubao-seedance-2-5-260628" {
					p.Resolutions[i].Pixels = map[string]string{"16:9": "854×480", "9:16": "480×854"}
				} else {
					p.Resolutions[i].Pixels = map[string]string{"16:9": "864×496", "9:16": "496×864"}
				}
			}
		}
		if connection == "mock" {
			switch r.ID {
			case "480p":
				p.Resolutions[i].Pixels = map[string]string{"16:9": "854×480"}
			case "720p":
				p.Resolutions[i].Pixels = map[string]string{"16:9": "1280×720"}
			case "1080p":
				p.Resolutions[i].Pixels = map[string]string{"16:9": "1920×1080"}
			}
		}
	}
	p.SupplierPrice = mediaSupplierPrice(model)
	return p
}

// mediaSupplierPrice 仅作官方牌价展示，不能替代供应商用量回执，也不替代运营独立的积分定价。
func mediaSupplierPrice(model string) string {
	switch model {
	case "gpt-image-2":
		return "Standard 输出 $30/百万图像 token，输入另计；1024×1024 输出示例：low $0.006、medium $0.053、high $0.211/张。"
	case "gpt-image-2.5-sunburst", "gpt-image-2.5-flare":
		return "Standard 图像输入/缓存/输出 $8/$2/$30 每百万 token，文本输入/缓存 $5/$1.25 每百万 token；像素与质量共同决定输出 token。"
	case "gemini-nano-banana-2.1":
		return "Standard 输出：1K $0.0336、2K $0.0504、4K 约 $0.113/张；输入 $1.50/百万 token，文本/思考输出 $7.50/百万 token。"
	case "gemini-3-pro-image":
		return "Standard 输出：1K/2K $0.134、4K $0.24/张；输入 $2、文本/思考输出 $12、图片输出 $120 每百万 token。"
	case "gemini-3.1-flash-image":
		return "Standard 输出：512px $0.045、1K $0.067、2K $0.101、4K $0.151/张；输入 $0.50、文本/思考 $3、图片输出 $60 每百万 token。"
	case "gemini-3.1-flash-lite-image":
		return "Standard 1K 输出 $0.0336/张；输入 $0.25、文本/思考输出 $1.50、图片输出 $30 每百万 token。"
	case "gemini-2.5-flash-image":
		return "已关闭。历史 Standard 1K 输出 $0.039/张，输入 $0.30/百万 token。"
	case "doubao-seedream-5-0-pro-260628":
		return "单图输出：≤261 万像素 ¥0.30/张，超过 ¥0.60/张；输入首图免费，第 2 张起 ¥0.02/张。图层拆分输出单价另为 ¥0.15/¥0.30。"
	case "doubao-seedream-5-0-flash-260915":
		return "输出 ¥0.12/张，输入免费，各支持档位同价。"
	case SeedreamLiteModelID:
		return "输出 ¥0.22/张，输入免费，各支持档位同价；已停止新购，2026-11-24 14:00（UTC+8）关停。"
	case "doubao-seedance-2-5-260628":
		return "¥/百万视频 token（无/有输入视频）：480p/720p 70/42；1080p 77/46。16:9、24fps、无输入视频示例约 ¥0.67/1.51/3.74 每输出秒。"
	case "doubao-seedance-2-0-260128":
		return "¥/百万视频 token（无/有输入视频）：480p/720p 46/28；1080p 51/31；4K 26/16。16:9、无输入视频示例约 ¥0.46/0.99/2.48/5.05 每输出秒。"
	case "doubao-seedance-2-0-fast-260128":
		return "刊例 ¥37/22 每百万视频 token（无/有输入视频）；480p/720p 无输入视频示例约 ¥0.37/0.80 每输出秒。8 月至 10 月 7 日促销已结束。"
	case "doubao-seedance-2-0-mini-260615":
		return "刊例 ¥23/14 每百万视频 token（无/有输入视频）；480p/720p 无输入视频示例约 ¥0.23/0.50 每输出秒。8 月至 10 月 7 日促销已结束。"
	case "wan2.7-image":
		return "北京 ¥0.20/输出张，各支持分辨率同价；新加坡 ¥0.224826/张。"
	case "wan2.7-image-pro":
		return "北京 ¥0.50/输出张，各支持分辨率同价；新加坡 ¥0.562065/张。"
	case "happyhorse-1.1-t2v", "happyhorse-1.1-i2v", "happyhorse-1.1-r2v":
		return "北京输出 480p ¥0.45/秒、720p ¥0.90/秒、1080p ¥1.20/秒；输入不收费。"
	case "happyhorse-1.0-video-edit":
		return "北京刊例：720p ¥0.90/秒、1080p ¥1.60/秒，输入和输出时长均计费；页面标有限时八折，期限未明确，不写死促销。"
	case "kling-v3":
		return "1K/2K 均为 $0.028/张（8 图片 Units）。"
	case "kling-v3-omni":
		return "1K/2K $0.028/张（8 图片 Units），4K $0.056/张（16 图片 Units）。"
	case "kling-3.0":
		return "无音频：720p/1080p/4K $0.084/$0.112/$0.42 每秒；有音频、无音色控制 $0.126/$0.168/$0.42 每秒。"
	case "kling-3.0-omni":
		return "720p/1080p/4K 每秒：无参考视频无音频 $0.084/$0.112/$0.42；无参考视频有音频 $0.112/$0.14/$0.42；有参考视频无音频 $0.126/$0.168/$0.42。"
	case "kling-3.0-turbo":
		return "原生音频：720p $0.112/秒、1080p $0.14/秒。"
	case MockImageModelID, MockVideoModelID:
		return "供应商成本 0，用户积分另按运营设置。"
	}
	return ""
}
