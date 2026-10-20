package notificationmarkdown

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommonMarkImageDestinations(t *testing.T) {
	for _, body := range []string{"![图](/api/notifications/n/images/i)", "![图][ref]\n\n[ref]: /api/notifications/n/images/i"} {
		if got := (Parser{}).ImageDestinations(body); !reflect.DeepEqual(got, []string{"/api/notifications/n/images/i"}) {
			t.Fatal(body, got)
		}
	}
	if got := (Parser{}).ImageDestinations("`![example](https://example.com/x)`\n\n[普通链接](https://example.com)"); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestSummaryVisiblePlainText(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"# 标题\n\n**正文**\n下一行 [了解更多](https://private.invalid)\n\n- 列表", "标题 正文 下一行 了解更多 列表"},
		{"![隐藏说明](/api/notifications/n/images/i)\n\n可见", "可见"},
		{"![图片][ref]\n\n[ref]: /api/notifications/n/images/i", ""},
		{"正常\n\n<div hidden>隐藏正文<img src='private'></div>\n\n继续", "正常 继续"},
		{"忽略 <span hidden>隐藏</span> 同一段\n\n保留", "保留"},
		{"<!-- 隐藏注释 -->\n\n可见", "可见"},
		{"```html\n<img src='private'>\n```\n\n`私密代码` 文字 <https://private.invalid>", "文字"},
		{"A &amp; B &#x4E2D; 文\\*字\t结束", "A & B 中 文*字 结束"},
		{"😀" + strings.Repeat("中", 119), "😀" + strings.Repeat("中", 119)},
		{strings.Repeat("😀", 121), strings.Repeat("😀", 119) + "…"},
	} {
		got := (Parser{}).Summary(tc.body)
		if got != tc.want || utf8.RuneCountInString(got) > 120 || !utf8.ValidString(got) {
			t.Errorf("summary(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}
