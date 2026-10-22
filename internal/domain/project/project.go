package project

import (
	"encoding/json"
	"time"
)

type Project struct {
	SpaceID         string           `json:"spaceId"`
	CreatorID       string           `json:"creatorId"`
	OwnerID         string           `json:"ownerId,omitempty"`
	MediaSelections []MediaSelection `json:"mediaSelections,omitempty"`
	Storyboard      *Storyboard      `json:"storyboard,omitempty"`
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	UploadedAt      *time.Time       `json:"uploadedAt"`
	DeletedAt       *time.Time       `json:"deletedAt,omitempty"`
	Revision        uint64           `json:"revision"`
	ScriptSource    ScriptSource     `json:"source"`
	Original        []byte           `json:"original,omitempty"`
	OriginalFile    *OriginalFile    `json:"originalFile,omitempty"`
	SourceFileID    string           `json:"sourceFileId,omitempty"`
	Assets          []Asset          `json:"assets"`
	Runs            []Run            `json:"runs"`
	// 零表示只记用量；正额度覆盖项目全部调用，额度本身不是外发授权。
	LimitMicroCNY       int64            `json:"limitMicroCny,omitempty"`
	ProjectSettings     *ProjectSettings `json:"settings"`
	Style               *ProjectStyle    `json:"style"`
	Outline             *Outline         `json:"outline"`
	VisualPrompts       []VisualPrompt   `json:"visualPrompts"`
	CatalogEditedByUser bool             `json:"catalogEditedByUser"`
}

func Clone(p Project) Project {
	raw, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	var q Project
	if err := json.Unmarshal(raw, &q); err != nil {
		panic(err)
	}
	return q
}

func (p *Project) LatestRun() *Run {
	if len(p.Runs) == 0 {
		return nil
	}
	return &p.Runs[len(p.Runs)-1]
}

func (p Project) UsedMicroCNY() int64 {
	var n int64
	for _, r := range p.Runs {
		for _, c := range r.Calls {
			if c.Usage.Known {
				n += c.CostMicroCNY
			} else {
				n += c.ReservedMicroCNY
			}
		}
	}
	return n
}
