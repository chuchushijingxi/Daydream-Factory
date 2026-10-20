package modelconfig

// Current official API Key catalog, verified 2026-10-14. Image identifiers are
// model_name values; the upgraded video API identifies models in endpoint paths.
// These are configuration/preference entries, not generation adapters.
const KlingBaseURL = "https://api-singapore.klingai.com"

const KlingProtocol = "kling-media"

const KlingImage3ModelID = "kling-v3"

const KlingImageOmniModelID = "kling-v3-omni"

const KlingVideo3ModelID = "kling-3.0"

const KlingVideoOmniModelID = "kling-3.0-omni"

const KlingVideoTurboModelID = "kling-3.0-turbo"

func IsKlingModel(id, category string) bool { return klingModelName(id, category) != "" }

func klingModelName(id, category string) string {
	if category == "image" {
		switch id {
		case KlingImage3ModelID:
			return "Kling Image 3.0"
		case KlingImageOmniModelID:
			return "Kling Image 3.0 Omni"
		}
	}
	if category == "video" {
		switch id {
		case KlingVideo3ModelID:
			return "Kling Video 3.0"
		case KlingVideoOmniModelID:
			return "Kling Video 3.0 Omni"
		case KlingVideoTurboModelID:
			return "Kling Video 3.0 Turbo"
		}
	}
	return ""
}

func klingPreset() ModelConnection {
	media := func(id, category string) ConfiguredModel {
		return ConfiguredModel{ID: id, ModelID: id, Category: category}
	}
	return ModelConnection{ID: "kling", Name: "可灵 AI", Kind: "official", Protocol: KlingProtocol, BaseURL: KlingBaseURL, Models: []ConfiguredModel{
		media(KlingImage3ModelID, "image"), media(KlingImageOmniModelID, "image"),
		media(KlingVideo3ModelID, "video"), media(KlingVideoOmniModelID, "video"), media(KlingVideoTurboModelID, "video"),
	}}
}
