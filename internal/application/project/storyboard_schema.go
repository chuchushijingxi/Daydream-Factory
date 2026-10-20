package project

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strings"
	"unicode/utf8"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

//go:embed storyboard_schema.json
var storyboardSchemaJSON []byte
var storyboardDefinitions = func() map[string]any {
	var schema map[string]any
	if err := json.Unmarshal(storyboardSchemaJSON, &schema); err != nil {
		panic(err)
	}
	return schema["$defs"].(map[string]any)
}()

// Expand the exact write union for providers; no unresolved local $refs escape on the wire.
func StoryboardSchema(name string) json.RawMessage {
	var expand func(any) any
	expand = func(value any) any {
		switch v := value.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				return expand(storyboardDefinitions[strings.TrimPrefix(ref, "#/$defs/")])
			}
			m := map[string]any{}
			for k, x := range v {
				m[k] = expand(x)
			}
			return m
		case []any:
			a := make([]any, len(v))
			for i, x := range v {
				a[i] = expand(x)
			}
			return a
		default:
			return value
		}
	}
	raw, err := json.Marshal(expand(storyboardDefinitions[name]))
	if err != nil {
		panic(err)
	}
	return raw
}

// 在展开后的本次工具 schema 上施加冻结上限；不改全局 schema 或其他任务。
func storyboardSaveSchema(name string, scope *d.StoryboardScope) json.RawMessage {
	raw := StoryboardSchema(name)
	var schema any
	if e := json.Unmarshal(raw, &schema); e != nil {
		panic(e)
	}
	var constrain func(any)
	constrain = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			// 内容要求已在主指令中完整提供；不在 create/patch 各分支重复运输相同说明。
			delete(v, "description")
			if field, ok := v["durationSeconds"].(map[string]any); ok {
				for _, branch := range field["anyOf"].([]any) {
					n := branch.(map[string]any)
					if n["type"] == "number" {
						n["type"] = "integer"
						if scope != nil && scope.Operation == "breakdown" && scope.VideoConstraint != nil {
							n["maximum"] = scope.VideoConstraint.MaxDurationSeconds
						}
					}
				}
			}
			for _, child := range v {
				constrain(child)
			}
		case []any:
			for _, child := range v {
				constrain(child)
			}
		}
	}
	constrain(schema)
	return mustJSON(schema)
}

func storyboardArgumentError(raw string, original error) error {
	var syntax *json.SyntaxError
	var value any
	if err := json.Unmarshal([]byte(raw), &value); errors.As(err, &syntax) {
		// 偏移与短片段帮助模型修复真实 JSON，绝不自动补括号或剥离参数包装。
		at := int(syntax.Offset) - 1
		start, end := max(0, at-32), min(len(raw), at+48)
		return fmt.Errorf("%w: JSON byte %d: %v; near %q; 只提交合法参数对象，不包装 arguments 或 JSON 字符串", fault.ErrInvalid, syntax.Offset, syntax, raw[start:end])
	}
	return original
}

// Decode against the same closed union used for model tool registration and HTTP.
// It deliberately implements only the schema vocabulary present in storyboard_schema.json.
func DecodeStoryboard(raw []byte, name string, value any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var parse func() (any, error)
	parse = func() (any, error) {
		t, e := dec.Token()
		if e != nil {
			return nil, e
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				m := map[string]any{}
				for dec.More() {
					k, e := dec.Token()
					if e != nil {
						return nil, e
					}
					key := k.(string)
					if _, ok := m[key]; ok {
						return nil, fault.ErrInvalid
					}
					v, e := parse()
					if e != nil {
						return nil, e
					}
					m[key] = v
				}
				_, e = dec.Token()
				return m, e
			case '[':
				a := []any{}
				for dec.More() {
					v, e := parse()
					if e != nil {
						return nil, e
					}
					a = append(a, v)
				}
				_, e = dec.Token()
				return a, e
			default:
				return nil, fault.ErrInvalid
			}
		}
		return t, nil
	}
	data, e := parse()
	if e != nil {
		return fmt.Errorf("%w: %v", fault.ErrInvalid, e)
	}
	if _, e = dec.Token(); e != io.EOF {
		return fault.ErrInvalid
	}
	if err := checkStoryboardSchema(data, storyboardDefinitions[name].(map[string]any)); err != nil {
		return fmt.Errorf("%w: %v", fault.ErrInvalid, err)
	}
	canonical, err := json.Marshal(canonicalStoryboardNumbers(data))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(canonical, value); err != nil {
		return fmt.Errorf("%w: %v", fault.ErrInvalid, err)
	}
	return nil
}
func checkStoryboardSchema(v any, s map[string]any) error {
	bad := func() error { return fmt.Errorf("value violates storyboard schema") }
	// 现有 schema 边界不变；把实际数值/长度及约束交回模型，避免只报泛化错误。
	constraint := func(actual any, keys ...string) error {
		limits := []string{}
		for _, key := range keys {
			if limit, ok := s[key]; ok {
				limits = append(limits, fmt.Sprintf("%s=%v", key, limit))
			}
		}
		return fmt.Errorf("actual=%v; require %s", actual, strings.Join(limits, ", "))
	}
	if ref, ok := s["$ref"].(string); ok {
		return checkStoryboardSchema(v, storyboardDefinitions[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any))
	}
	for _, union := range []string{"oneOf", "anyOf"} {
		if list, ok := s[union].([]any); ok {
			n := 0
			var matched error
			for _, x := range list {
				branch := x.(map[string]any)
				err := checkStoryboardSchema(v, branch)
				if err == nil {
					n++
				} else if storyboardDiscriminatorMatches(v, branch) {
					matched = err
				}
			}
			if n == 0 || union == "oneOf" && n != 1 {
				if n == 0 && matched != nil {
					return matched
				}
				return bad()
			}
			return nil
		}
	}
	if c, ok := s["const"]; ok && !reflect.DeepEqual(c, v) {
		return bad()
	}
	if enums, ok := s["enum"].([]any); ok {
		found := false
		for _, e := range enums {
			found = found || reflect.DeepEqual(e, v)
		}
		if !found {
			return bad()
		}
	}
	number := func(key string) (float64, bool) { x, ok := s[key].(float64); return x, ok }
	switch s["type"] {
	case "null":
		if v != nil {
			return bad()
		}
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]any)
		for _, k := range required {
			if _, ok := m[k.(string)]; !ok {
				if k == "operations" {
					return fmt.Errorf("missing operations; 参数根对象须为 {\"operations\":[...]}，不包装 arguments 或 JSON 字符串")
				}
				return fmt.Errorf("missing %s", k)
			}
		}
		if min, ok := number("minProperties"); ok && len(m) < int(min) {
			return bad()
		}
		for k, x := range m {
			shape, ok := props[k]
			if !ok {
				if s["additionalProperties"] == false {
					return fmt.Errorf("unknown %s", k)
				}
				continue
			}
			if e := checkStoryboardSchema(x, shape.(map[string]any)); e != nil {
				return fmt.Errorf("%s: %w", k, e)
			}
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			return bad()
		}
		if min, ok := number("minItems"); ok && len(a) < int(min) {
			return constraint(len(a), "minItems", "maxItems")
		}
		if max, ok := number("maxItems"); ok && len(a) > int(max) {
			return constraint(len(a), "minItems", "maxItems")
		}
		seen := map[string]bool{}
		for i, x := range a {
			if shape, ok := s["items"].(map[string]any); ok {
				if e := checkStoryboardSchema(x, shape); e != nil {
					return fmt.Errorf("[%d]: %w", i, e)
				}
			}
			if s["uniqueItems"] == true {
				b, _ := json.Marshal(x)
				if seen[string(b)] {
					return bad()
				}
				seen[string(b)] = true
			}
		}
	case "string":
		text, ok := v.(string)
		if !ok {
			return bad()
		}
		n := utf8.RuneCountInString(text)
		if min, ok := number("minLength"); ok && n < int(min) {
			return constraint(n, "minLength", "maxLength")
		}
		if max, ok := number("maxLength"); ok && n > int(max) {
			return constraint(n, "minLength", "maxLength")
		}
	case "integer", "number":
		n, ok := v.(json.Number)
		if !ok {
			return constraint(fmt.Sprintf("type %T", v), "type", "minimum", "exclusiveMinimum", "maximum")
		}
		f, e := n.Float64()
		if e != nil || math.IsInf(f, 0) || math.IsNaN(f) || s["type"] == "integer" && !storyboardInteger(n) {
			return bad()
		}
		if min, ok := number("minimum"); ok && f < min {
			return constraint(f, "minimum", "exclusiveMinimum", "maximum")
		}
		if min, ok := number("exclusiveMinimum"); ok && f <= min {
			return constraint(f, "minimum", "exclusiveMinimum", "maximum")
		}
		if max, ok := number("maximum"); ok && f > max {
			return constraint(f, "minimum", "exclusiveMinimum", "maximum")
		}
	default:
		if _, ok := s["type"]; ok {
			return bad()
		}
	}
	return nil
}

// 只按协议显式判别字段定位分支；不从自由文本推断业务动作。
func storyboardDiscriminatorMatches(value any, schema map[string]any) bool {
	if ref, ok := schema["$ref"].(string); ok {
		return storyboardDiscriminatorMatches(value, storyboardDefinitions[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any))
	}
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	props, _ := schema["properties"].(map[string]any)
	for _, key := range []string{"op", "type"} {
		if field, ok := props[key].(map[string]any); ok {
			if expected, ok := field["const"]; ok && reflect.DeepEqual(object[key], expected) {
				return true
			}
		}
	}
	if props["op"] != nil || props["type"] != nil {
		return false
	}
	switch schema["type"] {
	case "object":
		return true
	}
	return false
}

type StoryboardChangeInput struct {
	Revision   uint64                  `json:"revision"`
	Operations []d.StoryboardOperation `json:"operations"`
}
type EpisodeRunInput struct {
	Revision      uint64   `json:"revision"`
	Operation     string   `json:"operation"`
	ShotIDs       []string `json:"shotIds,omitempty"`
	BreakdownMode string   `json:"breakdownMode,omitempty"`
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

func storyboardInteger(n json.Number) bool {
	r, ok := storyboardNumber(n)
	return ok && r.IsInt()
}
func canonicalStoryboardNumbers(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			v[k] = canonicalStoryboardNumbers(x)
		}
		return v
	case []any:
		for i, x := range v {
			v[i] = canonicalStoryboardNumbers(x)
		}
		return v
	case json.Number:
		if r, ok := storyboardNumber(v); ok && r.IsInt() {
			return json.Number(r.Num().String())
		}
		return v
	default:
		return value
	}
}

// An underflowing nonzero literal cannot be an integer. Handle zero without allocating a huge exponent.
func storyboardNumber(n json.Number) (*big.Rat, bool) {
	f, e := n.Float64()
	if e != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return nil, false
	}
	if f == 0 {
		coefficient := strings.SplitN(strings.ToLower(string(n)), "e", 2)[0]
		if strings.Trim(coefficient, "-0.") == "" {
			return new(big.Rat), true
		}
		return nil, false
	}
	return new(big.Rat).SetString(string(n))
}
