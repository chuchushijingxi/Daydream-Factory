package ports

import "context"

// NotificationUpdates 只携带账户范围的失效提示；GET 查询始终权威，源断开须重连并重读。
type NotificationUpdates interface {
	WatchNotifications(context.Context, string) (<-chan struct{}, func(), error)
}

// NotificationMarkdown 解析图片引用；应用自行核对资源范围，不引入 Markdown 解析器或 HTTP 实现。
type NotificationMarkdown interface {
	ImageDestinations(string) []string
	// Summary 返回至多 120 个 Unicode 字符的纯文本可见摘要。
	Summary(string) string
}
