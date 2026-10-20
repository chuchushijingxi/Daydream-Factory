package modelconfig

// Bailian's Beijing endpoint remains supported alongside workspace-specific
// hosts. Wan Image and HappyHorse share its regional API Key. No paid calls
// are made by configuration or by saving project media preferences.
const AlibabaBaseURL = "https://dashscope.aliyuncs.com/api/v1"

const AlibabaProtocol = "dashscope-media"

const WanImageModelID = "wan2.7-image"

const WanImageProModelID = "wan2.7-image-pro"

const HappyHorseTextModelID = "happyhorse-1.1-t2v"

const HappyHorseImageModelID = "happyhorse-1.1-i2v"

const HappyHorseReferenceModelID = "happyhorse-1.1-r2v"

const HappyHorseEditModelID = "happyhorse-1.0-video-edit"

func IsAlibabaModel(id string) bool { return alibabaModelName(id) != "" }

func alibabaModelName(id string) string {
	return map[string]string{
		WanImageModelID: "Wan Image 2.7", WanImageProModelID: "Wan Image 2.7 Pro",
		HappyHorseTextModelID: "HappyHorse 1.1 文生视频", HappyHorseImageModelID: "HappyHorse 1.1 图生视频",
		HappyHorseReferenceModelID: "HappyHorse 1.1 参考生视频", HappyHorseEditModelID: "HappyHorse 1.0 视频编辑",
	}[id]
}

func alibabaPreset() ModelConnection {
	media := func(id, category string) ConfiguredModel {
		return ConfiguredModel{ID: id, ModelID: id, Category: category}
	}
	return ModelConnection{ID: "alibaba", Name: "阿里云百炼", Kind: "official", Protocol: AlibabaProtocol, BaseURL: AlibabaBaseURL, Models: []ConfiguredModel{
		media(WanImageModelID, "image"), media(WanImageProModelID, "image"),
		media(HappyHorseTextModelID, "video"), media(HappyHorseImageModelID, "video"), media(HappyHorseReferenceModelID, "video"), media(HappyHorseEditModelID, "video"),
	}}
}
