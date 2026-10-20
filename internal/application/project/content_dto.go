package project

import (
	"encoding/json"

	"drama-agent/internal/application/validation"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// 模型写协议独立于领域正文；schema、解析和映射统一从此 DTO 出发。
type VisualTargetInput struct {
	AssetID string `json:"assetId" jsonschema:"minLength=1"`
	LookID  string `json:"lookId,omitempty" jsonschema:"nullable,minLength=1"`
}

func (t *VisualTargetInput) UnmarshalJSON(raw []byte) error {
	type wire VisualTargetInput
	var value wire
	if err := validation.DecodeStrict(raw, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	// 现行协议允许省略/null 表示基础目标，显式空字符串仍拒绝。
	if look, ok := fields["lookId"]; ok && string(look) != "null" && value.LookID == "" {
		return fault.ErrInvalid
	}
	*t = VisualTargetInput(value)
	return nil
}

type VisualPromptInput struct {
	Target      VisualTargetInput `json:"target"`
	Text        string            `json:"text" jsonschema:"minLength=1"`
	DesignNotes string            `json:"designNotes"`
}

type SaveOutline struct {
	Outline d.OutlineInput `json:"outline"`
}

type SaveVisualPrompts struct {
	Prompts []VisualPromptInput `json:"prompts" jsonschema:"minItems=1"`
}

func (v SaveVisualPrompts) Contents() []d.VisualPromptInput {
	out := make([]d.VisualPromptInput, len(v.Prompts))
	for i, p := range v.Prompts {
		out[i] = d.VisualPromptInput{Target: d.VisualTarget{AssetID: p.Target.AssetID, LookID: p.Target.LookID}, Text: p.Text, DesignNotes: p.DesignNotes}
	}
	return out
}
