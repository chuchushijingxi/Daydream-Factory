package notifications

import "strings"

// EscapeMarkdownText 把已校验的名称等单行动态文本嵌入通知Markdown正文。
// 只在拼接插值时调用；整段运营正文仍保留其原有Markdown能力。
func EscapeMarkdownText(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for _, r := range value {
		if r == '&' {
			// 先保护原有实体文本，避免名称中的“&amp;”被显示为另一个名称。
			out.WriteString("&amp;")
			continue
		}
		if r >= '!' && r <= '/' || r >= ':' && r <= '@' || r >= '[' && r <= '`' || r >= '{' && r <= '~' {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}
