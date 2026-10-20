package project

type OutlineInput struct {
	Protagonist string `json:"protagonist"`
	Genre       string `json:"genre"`
	Summary     string `json:"summary"`
	Background  string `json:"background"`
	Logline     string `json:"logline"`
}

type Outline struct {
	OutlineInput
	SourceVersion uint64 `json:"sourceVersion"`
	ActionRef
}
