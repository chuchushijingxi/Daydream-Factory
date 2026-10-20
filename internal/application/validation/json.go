package validation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	fault "drama-agent/internal/domain/fault"
)

// DecodeStrict 只保护 JSON 格式、必需键、类型及未知字段，不解释正文。
// 可空字段和业务归一化由具体 DTO 处理，通用解析不认识资产或 Look。
func DecodeStrict(raw []byte, value any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return fmt.Errorf("%w: %v", fault.ErrInvalid, err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return fmt.Errorf("%w: trailing JSON", fault.ErrInvalid)
	}
	var shape any
	if json.Unmarshal(raw, &shape) != nil {
		return fault.ErrInvalid
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var expected any
	_ = json.Unmarshal(canonical, &expected)
	var check func(any, any) error
	check = func(got, want any) error {
		switch w := want.(type) {
		case map[string]any:
			g, ok := got.(map[string]any)
			if !ok {
				return fault.ErrInvalid
			}
			for k, v := range w {
				x, ok := g[k]
				if !ok {
					return fmt.Errorf("%w: missing %s", fault.ErrInvalid, k)
				}
				if err := check(x, v); err != nil {
					return fmt.Errorf("%w: %s", err, k)
				}
			}
		case []any:
			g, ok := got.([]any)
			if !ok {
				return fault.ErrInvalid
			}
			for i := range g {
				if i < len(w) {
					if err := check(g[i], w[i]); err != nil {
						return err
					}
				}
			}
		case string:
			if _, ok := got.(string); !ok {
				return fault.ErrInvalid
			}
		case float64:
			if _, ok := got.(float64); !ok {
				return fault.ErrInvalid
			}
		case bool:
			if _, ok := got.(bool); !ok {
				return fault.ErrInvalid
			}
		default:
			if got != nil {
				return fault.ErrInvalid
			}
		}
		return nil
	}
	return check(shape, expected)
}
