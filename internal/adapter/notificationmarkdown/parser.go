package notificationmarkdown

import (
	"drama-agent/internal/application/ports"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type Parser struct{}

var _ ports.NotificationMarkdown = Parser{}

func (Parser) ImageDestinations(body string) []string {
	out := []string{}
	doc := goldmark.New().Parser().Parse(text.NewReader([]byte(body)))
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if img, ok := node.(*ast.Image); ok && entering {
			out = append(out, string(img.Destination))
		}
		return ast.WalkContinue, nil
	})
	return out
}
