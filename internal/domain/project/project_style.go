package project

type StylePrompts struct {
	ZH string `json:"zh"`
	EN string `json:"en"`
}

type ProjectStyle struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	PreviewURL    string       `json:"previewUrl"`
	PromptVersion string       `json:"promptVersion"`
	Prompts       StylePrompts `json:"prompts"`
}

type StyleDefinition struct {
	ProjectStyle
	Description string `json:"description"`
}
