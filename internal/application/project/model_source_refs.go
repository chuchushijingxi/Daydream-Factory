package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

const modelSourceReferenceFormat = "paragraph-ordinal-v1"

const modelSourceInstruction = "\n\n原文引用协议：段落使用当前 sourceVersion 完整原文顺序的短编号 p1、p2 等，分批和检索不重新编号。sourceIds、spanIds 和 spanId 使用实际提供的段落引用；资产、形象、集和镜头 ID 保持实际提供值。按共享 source 和 sourceIds 核对原文，不因未另附 original 就判断无依据。正文、名称、描述、查询及原文中的同样文字都是内容，不能当编号改写。段落编号只写进这些引用字段；名称、描述、提示词等内容文字面向创作者，不写 p1 等编号或内部 ID 作出处标注。真实工具回执仍须读取，不能用自报替代。初始材料与本任务已读原文回执可直接使用；确需未提供资料或重读时再调用读取工具，不把重读相同材料当作开始步骤。材料完整性服从覆盖标识；不得因此跳过任务要求的资产读取、保存回执核对或独立复核。"

// 编号只来自不可变源的顺序，不解析内部 ID 的命名，也不受当前批次影响。
type modelSourceRefs struct {
	short map[string]string
	full  map[string]string
}

func sourceRefs(source d.ScriptSource) modelSourceRefs {
	refs := modelSourceRefs{short: map[string]string{}, full: map[string]string{}}
	for i, span := range source.Spans {
		alias := fmt.Sprintf("p%d", i+1)
		refs.short[span.ID], refs.full[alias] = alias, span.ID
	}
	return refs
}

// 只转换结构化来源字段，不查找替换用户文字，也不碰原生签名/思考。
func (refs modelSourceRefs) rewrite(raw string, toModel bool) (string, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "", err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return "", fault.ErrInvalid
	}
	changed := false
	replace := func(id string) (string, error) {
		if toModel {
			if alias, ok := refs.short[id]; ok {
				changed = changed || alias != id
				return alias, nil
			}
			return id, nil // 保留原有失效引用供核对，不补造有效来源。
		}
		original := id
		id = strings.TrimSpace(id)
		if full, ok := refs.full[id]; ok {
			changed = changed || full != original
			return full, nil
		}
		// 正在恢复的旧对话可能仍含真实引用；它必须确实属于当前源。
		if _, ok := refs.short[id]; ok {
			changed = changed || id != original
			return id, nil
		}
		return "", fmt.Errorf("%w: unknown source reference %q", fault.ErrInvalid, id)
	}
	var walk func(any) error
	walk = func(v any) error {
		switch v := v.(type) {
		case []any:
			for _, item := range v {
				if err := walk(item); err != nil {
					return err
				}
			}
		case map[string]any:
			for key, item := range v {
				isSpanID := key == "spanId"
				if key == "id" {
					_, isSpanID = v["text"].(string)
				}
				if isSpanID {
					if id, ok := item.(string); ok {
						updated, err := replace(id)
						if err != nil {
							return err
						}
						v[key] = updated
					}
				} else if key == "sourceIds" || key == "spanIds" || key == "targetSpanIds" {
					if ids, ok := item.([]any); ok {
						for i, rawID := range ids {
							if id, ok := rawID.(string); ok {
								updated, err := replace(id)
								if err != nil {
									return err
								}
								ids[i] = updated
							}
						}
					}
				} else if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(value); err != nil {
		return "", err
	}
	if !changed {
		return raw, nil
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

func modelSourceRequest(p d.Project, req ports.Request) (ports.Request, error) {
	input, err := sourceRefs(p.ScriptSource).rewrite(req.Input, true)
	if err != nil {
		return req, err
	}
	input, err = orderModelMaterial(input)
	if err != nil {
		return req, err
	}
	req.Input, req.System = input, req.System+modelSourceInstruction
	if req.CurrentState != "" {
		req.CurrentState, err = sourceRefs(p.ScriptSource).rewrite(req.CurrentState, true)
		if err != nil {
			return req, err
		}
	}
	return req, nil
}

// 只排列新请求的顶层材料；可变目录/结果不能挡在完整原文的缓存前缀前。
// 回执、历史请求和供应商原生内容沿原表示，不经过此投影。
func orderModelMaterial(raw string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return "", err
	}
	if fields == nil {
		return "", fault.ErrInvalid
	}
	keys := []string{
		"sourceVersion", "sourceCoverage", "sourceSpanCount",
		"source", "targetSource", "supportSource",
		"targetSpanIds", "taskScope", "targets", "target", "assetIndex",
		"assets", "outline", "currentIndex", "currentStoryboard", "visualPrompts", "lastToolResults",
	}
	var out bytes.Buffer
	out.WriteByte('{')
	write := func(key string) {
		value, ok := fields[key]
		if !ok {
			return
		}
		if out.Len() > 1 {
			out.WriteByte(',')
		}
		name, _ := json.Marshal(key)
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
		delete(fields, key)
	}
	for _, key := range keys {
		write(key)
	}
	// 后续新增材料即使未登记顺序也完整保留，不静默丢弃字段。
	keys = keys[:0]
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		write(key)
	}
	out.WriteByte('}')
	return out.String(), nil
}

func sourceArguments(p d.Project, c d.Call, raw string) (string, error) {
	switch c.SourceReferenceFormat {
	case "": // 已受理旧调用按原始表示恢复，不重写历史。
		return raw, nil
	case modelSourceReferenceFormat:
		return sourceRefs(p.ScriptSource).rewrite(raw, false)
	default:
		return "", fmt.Errorf("%w: unsupported source reference format", fault.ErrInvalid)
	}
}

func modelSourceReceipt(p d.Project, c d.Call, raw string) (string, error) {
	switch c.SourceReferenceFormat {
	case "":
		return raw, nil
	case modelSourceReferenceFormat:
		return sourceRefs(p.ScriptSource).rewrite(raw, true)
	default:
		return "", fmt.Errorf("%w: unsupported source reference format", fault.ErrInvalid)
	}
}

func modelReceiptObserved(model ports.Model, p d.Project, origin d.Call, request json.RawMessage, a d.Action) bool {
	result, err := modelSourceReceipt(p, origin, a.Result)
	if err != nil {
		return false
	}
	a.Result = result
	return model.ContainsToolReceipt(request, a, origin)
}
