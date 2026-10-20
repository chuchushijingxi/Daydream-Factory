package ports

type MediaRequest struct {
	Resolution      string
	DurationSeconds float64
	ModelID         string
	Prompt          string
	AspectRatio     string
}

type MediaResult struct {
	Data            []byte
	ContentType     string
	Width           int
	Height          int
	DurationSeconds float64
}
