package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

type SearchScriptEvidence struct {
	Query  string `json:"query" jsonschema:"minLength=1,maxLength=1024"`
	Limit  int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=20"`
	Offset int    `json:"offset,omitempty" jsonschema:"minimum=0,maximum=16362"`
}

type ReadScriptEvidence struct {
	SpanIDs []string `json:"spanIds" jsonschema:"minItems=1,maxItems=32"`
	Before  int      `json:"before,omitempty" jsonschema:"minimum=0,maximum=8"`
	After   int      `json:"after,omitempty" jsonschema:"minimum=0,maximum=8"`
}

func evidenceTools() []ports.ToolDefinition {
	return []ports.ToolDefinition{
		{Name: "search_script_evidence", Description: "在本项目当前源版本的完整原文向量索引中按语义检索候选段落。query表达要查的内容；limit默认5、最多20；offset分页。只返回定位及相关度，不证明角色归属或证据完整；先核对已有原文和真实读取回执，未提供的命中段再用read_script_evidence完整读取。", Parameters: schemaOf(SearchScriptEvidence{})},
		{Name: "read_script_evidence", Description: "读取本项目当前源版本的指定原文段落，可用before/after各0到8段扩展连续上下文。spanIds来自初始引用或检索回执，每次最多32个，可按需合并同批补读；优先使用已提供原文和真实读取回执，确需额外上下文或重读时再调用。完整返回所请求段落，不摘要、不截断，不修改资料。", Parameters: schemaOf(ReadScriptEvidence{})},
	}
}

func linkedEvidence(p d.Project, assets []d.Asset) ([]d.SourceSpan, error) {
	ids := map[string]bool{}
	for _, a := range assets {
		for _, id := range a.SourceIDs {
			ids[id] = true
		}
	}
	out := []d.SourceSpan{}
	for _, span := range p.ScriptSource.Spans {
		if ids[span.ID] {
			out = append(out, span)
			delete(ids, span.ID)
		}
	}
	if len(ids) != 0 {
		return nil, fmt.Errorf("%w: target contains a stale source reference", fault.ErrInvalid)
	}
	return out, nil
}

func (s *Service) evidenceTool(ctx context.Context, p d.Project, a d.Action) (map[string]any, error) {
	result := map[string]any{"sourceVersion": p.ScriptSource.Version}
	if r := p.LatestRun(); r != nil && len(r.Calls) > 0 {
		arguments, err := sourceArguments(p, r.Calls[len(r.Calls)-1], a.Arguments)
		if err != nil {
			return nil, err
		}
		a.Arguments = arguments
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(a.Arguments), &fields); err != nil {
		return nil, fault.ErrInvalid
	}
	switch a.Name {
	case "search_script_evidence":
		limit, err := evidenceInteger(fields, "limit", 1, 20, 5)
		if err != nil {
			return nil, err
		}
		offset, err := evidenceInteger(fields, "offset", 0, 16362, 0)
		if err != nil {
			return nil, err
		}
		var v SearchScriptEvidence
		if err := decodeTool(a.Arguments, &v); err != nil {
			return nil, err
		}
		v.Limit, v.Offset = limit, offset
		if strings.TrimSpace(v.Query) == "" || len([]rune(v.Query)) > 1024 {
			return nil, fault.New("INVALID_INPUT", fmt.Sprintf("query 字符数=%d；须为非空白字符串，允许 1～1024 字符", len([]rune(v.Query))))
		}
		if s.Evidence == nil {
			return nil, fault.New("EVIDENCE_UNAVAILABLE", "原文向量检索尚未配置")
		}
		hits, more, err := s.Evidence.Search(ctx, ports.EvidenceDocument{ProjectID: p.ID, Source: p.ScriptSource}, v.Query, v.Limit, v.Offset)
		if err != nil {
			return nil, err
		}
		known := map[string]bool{}
		for _, span := range p.ScriptSource.Spans {
			known[span.ID] = true
		}
		for _, hit := range hits {
			if !known[hit.SpanID] {
				return nil, fault.New("EVIDENCE_INDEX_INVALID", "检索结果不属于当前项目原文")
			}
		}
		result["matches"], result["hasMore"] = hits, more
		result["coverage"] = "ranked_candidates_not_complete"
		if more {
			result["nextOffset"] = v.Offset + len(hits)
		}
	case "read_script_evidence":
		before, err := evidenceInteger(fields, "before", 0, 8, 0)
		if err != nil {
			return nil, err
		}
		after, err := evidenceInteger(fields, "after", 0, 8, 0)
		if err != nil {
			return nil, err
		}
		var v ReadScriptEvidence
		if err := decodeTool(a.Arguments, &v); err != nil {
			return nil, err
		}
		v.Before, v.After = before, after
		if len(v.SpanIDs) == 0 || len(v.SpanIDs) > 32 {
			return nil, fault.New("INVALID_INPUT", fmt.Sprintf("spanIds 数量=%d；允许 1～32 个当前原文段落引用", len(v.SpanIDs)))
		}
		positions := map[string]int{}
		for i, span := range p.ScriptSource.Spans {
			positions[span.ID] = i
		}
		selected := map[int]bool{}
		for _, id := range v.SpanIDs {
			i, ok := positions[id]
			if !ok {
				return nil, fault.New("NOT_FOUND", "段落不属于当前项目原文版本")
			}
			for n := max(0, i-v.Before); n <= min(len(p.ScriptSource.Spans)-1, i+v.After); n++ {
				selected[n] = true
			}
		}
		spans := []d.SourceSpan{}
		for i, span := range p.ScriptSource.Spans {
			if selected[i] {
				spans = append(spans, span)
			}
		}
		result["source"] = spans
		result["coverage"] = "requested_paragraphs_only"
	default:
		return nil, fault.ErrInvalid
	}
	return result, nil
}

// 省略值与显式 null/0 不混淆；错误给出允许范围，不把越界请求裁小后冒充成功。
func evidenceInteger(fields map[string]json.RawMessage, name string, minimum, maximum, fallback int) (int, error) {
	raw, present := fields[name]
	if !present {
		return fallback, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return 0, fault.New("INVALID_INPUT", fmt.Sprintf("%s=null；须为 %d～%d 的整数，省略时为 %d", name, minimum, maximum, fallback))
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, fault.New("INVALID_INPUT", fmt.Sprintf("%s 类型无效；须为 %d～%d 的整数，省略时为 %d：%v", name, minimum, maximum, fallback, err))
	}
	if value < minimum || value > maximum {
		return 0, fault.New("INVALID_INPUT", fmt.Sprintf("%s=%d 超出允许范围 %d～%d；省略时为 %d", name, value, minimum, maximum, fallback))
	}
	return value, nil
}

var errEvidenceCapacity = fault.New("EVIDENCE_CAPACITY_EXCEEDED", "请求的原文证据无法完整放入下一轮上下文，请减少本次段落或上下文范围")

var errVisualSaveCapacity = fault.New("CAPACITY_EXCEEDED", "本次保存后的完整正文、依赖变化与回执无法放入下一轮上下文，请减少本次保存范围或正文长度")

func visualCapacityError(name string) error {
	if name == "search_script_evidence" || name == "read_script_evidence" {
		return errEvidenceCapacity
	}
	return errVisualSaveCapacity
}

func (s *Service) visualReceiptCapacity(ctx context.Context, p d.Project) error {
	candidate := d.Clone(p)
	r := candidate.LatestRun()
	c := &r.Calls[len(r.Calls)-1]
	// 已完成动作保留其完整真实回执；尚未执行的同响应动作先预留容量拒绝
	// 回执的空间。该候选只用于估算，不存储、不发送，也不采用未来动作。
	for i := range c.Actions {
		if c.Actions[i].Status == "pending" {
			setContentToolReceipt(&candidate, &c.Actions[i], map[string]any{}, visualCapacityError(c.Actions[i].Name))
		}
	}
	// 与下一轮请求使用同一路径，涵盖公共回执字段、assistant 内容/工具
	// 参数、已有兄弟回执以及 Tool.Content 再次 JSON 编码的真实长度。
	_, _, err := s.contentRequest(ctx, candidate)
	return err
}
