package notifications

import (
	"context"
	"testing"

	"drama-agent/internal/adapter/notificationmarkdown"
	domain "drama-agent/internal/domain/notifications"
	"drama-agent/internal/domain/spaces"
)

func TestSystemNotificationNamesRemainLiteralMarkdownText(t *testing.T) {
	parser := notificationmarkdown.Parser{}
	names := []string{
		"![x](https://example.test/x)", "[text](https://example.test)", "`团队名字`",
		"**名字**", "# 团队", "<https://example.test>", "<b>名字</b>", "A & B", "&amp; 团队", "反斜杠\\名字", "[名字][ref]",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if _, err := spaces.NormalizeName(name); err != nil {
				t.Fatal("fixture is not a legal team name", err)
			}
			input := SystemInput{OperationID: "name-fixture", RecipientUserID: "recipient", Title: "团队邀请", Body: name + "：邀请你加入团队。", Link: Link{Type: "invite", SpaceID: "team", TargetID: "invite"}}
			// 系统发布只做内容校验，不走运营图片归属检查；特殊字符本来就不应阻断事实。
			if _, err := input.Normalize(); err != nil {
				t.Fatal("legal name blocked system notification", err)
			}
			input.Body = domain.EscapeMarkdownText(name) + "：邀请你加入团队。"
			store := &systemStore{}
			service := &Service{Store: store}
			if _, err := service.SystemNotify(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			if images := parser.ImageDestinations(store.input.Body); len(images) != 0 {
				t.Fatal("name became an image", images)
			}
			if got := parser.Summary(store.input.Body); got != name+"：邀请你加入团队。" {
				t.Fatalf("dynamic name changed: got %q, want %q", got, name+"：邀请你加入团队。")
			}
		})
	}
	// helper不应用于运营正文；现有Markdown解析语义保持原样。
	authored := "**运营加粗** [正文链接](https://example.test) ![图](/api/notifications/n/images/i)"
	if images := parser.ImageDestinations(authored); len(images) != 1 || images[0] != "/api/notifications/n/images/i" {
		t.Fatal("operator Markdown image behavior changed", images)
	}
	if summary := parser.Summary(authored); summary != "运营加粗 正文链接" {
		t.Fatal("operator Markdown text behavior changed", summary)
	}
}
