package project

import "unicode/utf8"

// MaxScriptBytes 是现有导入容量边界，HTTP 与解析器共用，超限明确拒绝。
const MaxScriptBytes = 20 << 20

type SourceSpan struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type ScriptSource struct {
	Version    uint64       `json:"version"`
	Filename   string       `json:"filename"`
	SHA256     string       `json:"sha256"`
	Spans      []SourceSpan `json:"spans"`
	rangeIndex map[string]spanBounds
}

// WithRangeIndex 返回只用于本次只读投影的值；调用方不得再修改其 Spans。
// 索引不持久化、不跨请求缓存，避免每个镜头反复解码整份原文。
func (s ScriptSource) WithRangeIndex() ScriptSource {
	s.rangeIndex = make(map[string]spanBounds, len(s.Spans))
	base := 0
	for _, span := range s.Spans {
		if _, duplicate := s.rangeIndex[span.ID]; duplicate {
			s.rangeIndex = nil
			return s
		}
		n := utf8.RuneCountInString(span.Text)
		s.rangeIndex[span.ID] = spanBounds{base, n}
		base += n
	}
	return s
}

type spanBounds struct{ base, length int }
