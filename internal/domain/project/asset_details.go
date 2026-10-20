package project

import (
	"strings"

	"drama-agent/internal/domain/fault"
)

type Character struct {
	RoleType    string `json:"roleType"`
	Age         string `json:"age"`
	Gender      string `json:"gender"`
	Biography   string `json:"biography"`
	Appearance  string `json:"appearance"`
	Personality string `json:"personality"`
}

type AssetInput struct {
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	Aliases     []string   `json:"aliases"`
	Description string     `json:"description"`
	Character   *Character `json:"character"`
}

type LookInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Look struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Origin      string `json:"origin"`
	ImageFileID string `json:"imageFileId,omitempty"`
}

func (a AssetInput) Validate() error {
	if strings.TrimSpace(a.Name) == "" || a.Aliases == nil {
		return fault.ErrInvalid
	}
	switch a.Kind {
	case "character":
		if a.Character == nil {
			return fault.ErrInvalid
		}
	case "location", "prop":
		if a.Character != nil {
			return fault.ErrInvalid
		}
	default:
		return fault.ErrInvalid
	}
	return nil
}
