package modelconfig

const OpenAIBaseURL = "https://api.openai.com/v1"

const OpenAIImageProtocol = "openai-image"

const GPTImage2ModelID = "gpt-image-2"

const GPTImageSunburstModelID = "gpt-image-2.5-sunburst"

const GPTImageFlareModelID = "gpt-image-2.5-flare"

func IsOpenAIModel(id string) bool { return openaiModelName(id) != "" }

func openaiModelName(id string) string {
	return map[string]string{GPTImage2ModelID: "GPT Image 2", GPTImageSunburstModelID: "GPT Image 2.5 Sunburst", GPTImageFlareModelID: "GPT Image 2.5 Flare"}[id]
}

func openaiPreset() ModelConnection {
	c := ModelConnection{ID: "openai", Name: "OpenAI", Kind: "official", Protocol: OpenAIImageProtocol, BaseURL: OpenAIBaseURL}
	for _, id := range []string{GPTImage2ModelID, GPTImageSunburstModelID, GPTImageFlareModelID} {
		c.Models = append(c.Models, ConfiguredModel{ID: id, ModelID: id, Category: "image"})
	}
	return c
}
