package project

// Asset 是项目内的资产实体；稳定 ID 表示身份，名称不能作为身份或合并依据。
type Asset struct {
	ID          string     `json:"id,omitempty"`
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	Aliases     []string   `json:"aliases,omitempty"`
	Description string     `json:"description,omitempty"`
	SourceIDs   []string   `json:"sourceIds"`
	Character   *Character `json:"character,omitempty"`
	Looks       []Look     `json:"looks,omitempty"`
	Origin      string     `json:"origin,omitempty"`
	ImageFileID string     `json:"imageFileId,omitempty"`
}
