package validation

import "testing"

func TestStrictDecoderDoesNotInterpretBusinessFieldNames(t *testing.T) {
	var value struct {
		LookID string `json:"lookId"`
	}
	if err := DecodeStrict([]byte(`{"lookId":""}`), &value); err != nil {
		t.Fatal("通用解析不能按字段名称裁决业务规则", err)
	}
	for _, raw := range []string{`{}`, `{"lookId":12}`, `{"lookId":"x","extra":true}`} {
		if DecodeStrict([]byte(raw), &value) == nil {
			t.Fatal("格式边界失效", raw)
		}
	}
}
