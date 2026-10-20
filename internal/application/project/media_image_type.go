package project

import (
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// 构图只追加到本次运输输入，不改写资产正文。
func characterImageComposition(p *d.Project, target MediaTarget, imageType string) (string, error) {
	if imageType == "" {
		return "", nil
	}
	asset := p.Asset(target.AssetID)
	if target.EpisodeID != "" || target.ShotID != "" || asset == nil || asset.Kind != "character" {
		return "", fault.New("IMAGE_TYPE_INVALID", "生图类型仅适用于角色及其附加形象")
	}
	var layout string
	switch imageType {
	case "no_preset":
		return "", nil
	case "closeup":
		layout = "角色特写设定图：正面头肩胸像，完整展示头发、脸部、脖子、双肩与上胸衣服，突出五官、发型与上半身细节。"
	case "main_view":
		layout = "角色主视图设定图：全身正面自然站立，从头顶到脚底完整可见，双肩平行画面，展示全身比例、服装和整体造型。"
	case "closeup_main":
		layout = "角色特写 + 主视图设定图：同一张图片左侧为正面头肩胸像，右侧为全身正面自然站立主视图；全身从头顶到脚底完整可见。"
	case "three_view":
		layout = "角色三视图设定图：同一张图片横向排列全身正面、纯侧面90度、背面180度三视图，头顶到脚底完整可见，等高、同一比例和脚底基线，侧面与背面不得回头。"
	case "closeup_three_view":
		layout = "角色特写 + 三视图设定图：同一张图片横向排列四列，从左到右为正面头肩胸像、全身正面、全身纯侧面90度、全身背面180度；三张全身头顶到脚底完整可见，等高、同一比例和脚底基线，侧面与背面不得回头。"
	default:
		return "", fault.New("IMAGE_TYPE_INVALID", "不支持的生图类型")
	}
	return "画面类型：" + layout + "所有视图为同一角色、同一服装、发型、体型和配色，保持自然中性设定照姿态；纯净背景，不添加文字、编号、边框、水印或logo。仅输出一张图片，画布比例遵循本次生成参数。", nil
}

func mediaImageAspectRatio(p *d.Project, target MediaTarget, imageType, requested string) (string, error) {
	if target.AssetID == "" {
		if requested != "" {
			return "", fault.New("IMAGE_ASPECT_RATIO_INVALID", "视频不接受图片比例")
		}
		return "", nil
	}
	// 组合与多视图需要横向排版，不能沿竖屏项目默认值挤压角色。
	if imageType == "closeup_main" || imageType == "three_view" || imageType == "closeup_three_view" {
		if requested != "" && requested != "16:9" {
			return "", fault.New("IMAGE_ASPECT_RATIO_INVALID", "组合图和三视图使用16:9比例")
		}
		return "16:9", nil
	}
	if requested != "" {
		return requested, nil
	}
	if p.ProjectSettings != nil && p.ProjectSettings.AspectRatio != "" {
		return p.ProjectSettings.AspectRatio, nil
	}
	return "1:1", nil
}
