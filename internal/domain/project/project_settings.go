package project

type ProjectSettings struct {
	AspectRatio string `json:"aspectRatio"`
	LLMModel    string `json:"llmModel"`
	ImageModel  string `json:"imageModel"`
	VideoModel  string `json:"videoModel"`
}
