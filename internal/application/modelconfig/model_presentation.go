package modelconfig

import md "drama-agent/internal/domain/modelconfig"

// DefaultLogoURL 由当前模型目录投影默认图标，展示配置不影响实际请求路由。
func DefaultLogoURL(m md.ConfiguredModel) string {
	if md.IsMockModel(m.ModelID, m.Category) {
		return "/demo/assets/providers/mock.svg"
	}
	if md.IsDeepSeekModel(m.ModelID) {
		return "/demo/assets/providers/deepseek.svg"
	}
	if md.IsVolcengineModel(m.ModelID) {
		return "/demo/assets/providers/volcengine.svg"
	}
	if md.IsKlingModel(m.ModelID, m.Category) {
		return "/demo/assets/providers/kling.svg"
	}
	if md.IsAlibabaModel(m.ModelID) {
		return "/demo/assets/providers/alibaba.svg"
	}
	if md.IsGoogleModel(m.ModelID) {
		return "/demo/assets/providers/google.svg"
	}
	if md.IsOpenAIModel(m.ModelID) {
		return "/demo/assets/providers/openai.svg"
	}
	return ""
}

// LogoURL 优先使用用户覆盖，稳定实例 ID 和实际模型 ID 保持独立。
func LogoURL(m md.ConfiguredModel) string {
	if m.LogoID != "" {
		return "/api/model-logos/" + m.LogoID
	}
	return DefaultLogoURL(m)
}
