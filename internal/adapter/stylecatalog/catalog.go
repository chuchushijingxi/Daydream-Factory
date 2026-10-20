package stylecatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func LoadStyles(demo string) ([]d.StyleDefinition, error) {
	raw, err := os.ReadFile(filepath.Join(demo, "assets/styles/catalog.json"))
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Styles []d.StyleDefinition `json:"styles"`
	}
	if err = json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	if len(catalog.Styles) == 0 {
		return nil, fault.ErrInvalid
	}
	for _, s := range catalog.Styles {
		if s.ID == "" || seen[s.ID] || s.PromptVersion == "" || strings.TrimSpace(s.Prompts.ZH) == "" || strings.TrimSpace(s.Prompts.EN) == "" {
			return nil, fault.ErrInvalid
		}
		seen[s.ID] = true
	}
	return catalog.Styles, nil
}
