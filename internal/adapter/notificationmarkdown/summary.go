package notificationmarkdown

import (
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Summary 投影 Markdown 的可见文本，不渲染 HTML、不解析资源。
// 含内联 HTML 的块整体保守省略：仅剥标签会把隐藏/script 内容当普通摘要文本暴露。
func (Parser) Summary(body string) string {
	source := []byte(body)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	var out strings.Builder
	var visit func(ast.Node)
	visit = func(n ast.Node) {
		switch n.Kind() {
		case ast.KindImage, ast.KindAutoLink, ast.KindCodeSpan, ast.KindCodeBlock, ast.KindFencedCodeBlock, ast.KindHTMLBlock, ast.KindRawHTML:
			return
		}
		if n.Kind() == ast.KindParagraph || n.Kind() == ast.KindTextBlock || n.Kind() == ast.KindHeading {
			hasHTML := false
			_ = ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
				if entering && child.Kind() == ast.KindRawHTML {
					hasHTML = true
					return ast.WalkStop, nil
				}
				return ast.WalkContinue, nil
			})
			if hasHTML {
				return
			}
		}
		switch v := n.(type) {
		case *ast.Text:
			raw := util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v.Segment.Value(source))))
			out.Write(raw)
			if v.SoftLineBreak() || v.HardLineBreak() {
				out.WriteByte(' ')
			}
		case *ast.String:
			out.Write(v.Value)
		default:
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				visit(child)
			}
			if n.Type() == ast.TypeBlock {
				out.WriteByte(' ')
			}
		}
	}
	visit(doc)
	flat := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, out.String())
	runes := []rune(strings.Join(strings.Fields(flat), " "))
	if len(runes) > 120 {
		return string(runes[:119]) + "…"
	}
	return string(runes)
}
