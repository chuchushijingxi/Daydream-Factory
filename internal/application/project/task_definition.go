package project

import (
	"encoding/json"
	"strings"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/application/validation"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"

	"github.com/eino-contrib/jsonschema"
)

func taskInstruction(contract string) string {
	switch contract {
	case d.EpisodeIndexContract:
		return "识别、保存并核对完整剧本的分集与支持范围。"
	case d.StoryboardContract:
		return "完成当前集冻结任务范围，保存并核对场景、连续镜头及带ID引用的视频正文。"
	case d.OutlineContract:
		return "阅读完整剧本并生成、保存与核对故事资料。"
	case d.VisualContract:
		return "为本次完整冻结目标范围生成、保存与核对内容视觉正文。"
	default:
		return "阅读原文并整理资产目录。"
	}
}

func schemaOf(value any) json.RawMessage {
	shape := (&jsonschema.Reflector{DoNotReference: true}).Reflect(value)
	raw, _ := json.Marshal(shape)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	delete(m, "$schema")
	raw, _ = json.Marshal(m)
	return raw
}

func TaskTools(contract string) []ports.ToolDefinition {
	empty := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	switch contract {
	case d.EpisodeIndexContract, d.StoryboardContract:
		return runTaskTools(&d.Run{Contract: contract, StoryboardScope: &d.StoryboardScope{Operation: "breakdown"}})
	case d.OutlineContract:
		return []ports.ToolDefinition{{Name: "save_outline", Description: "原子保存或修正五项故事资料，返回实际 before/after；其他资料只读。", Parameters: schemaOf(SaveOutline{})}, {Name: "finish_outline", Description: "实际回执已返回且故事资料核对完成后结束。", Parameters: empty}}
	case d.VisualContract:
		return append([]ports.ToolDefinition{{Name: "save_visual_prompts", Description: "原子保存明确目标的完整正文与设计说明，返回真实变化及依赖失效；只能写冻结范围。", Parameters: schemaOf(SaveVisualPrompts{})}, {Name: "finish_visual_prompts", Description: "完整非空范围正文有效且实际回执已返回后结束。", Parameters: empty}}, evidenceTools()...)
	default:
		return Tools()
	}
}

func validTaskActions(contract, finish string, actions []d.Action) bool {
	if contract == d.CatalogExtractionContract {
		return validActions(finish, actions)
	}
	if !d.KnownContract(contract) || finish != "tool_calls" || len(actions) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, a := range actions {
		if a.ID == "" || seen[a.ID] || a.Name == "" {
			return false
		}
		seen[a.ID] = true
	}
	return true
}

// stripModelFence 剥离模型输出外层 markdown 围栏；围栏不完整视为无效输出。
// 围栏不是模型写协议的一部分，剥离后仍走严格解码，不放宽字段检查。
func stripModelFence(text string) (string, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text, nil
	}
	first := strings.IndexByte(text, '\n')
	if first < 0 || !strings.HasSuffix(text, "```") {
		return "", fault.ErrInvalid
	}
	return strings.TrimSpace(text[first+1 : len(text)-3]), nil
}

func decodeTool(raw string, v any) error {
	text, err := stripModelFence(raw)
	if err != nil {
		return err
	}
	return validation.DecodeStrict([]byte(text), v)
}

func contentToolNames(contract string) (save, finish string) {
	switch contract {
	case d.OutlineContract:
		return "save_outline", "finish_outline"
	case d.VisualContract:
		return "save_visual_prompts", "finish_visual_prompts"
	default:
		return "", ""
	}
}
