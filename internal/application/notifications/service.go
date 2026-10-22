// Package notifications owns in-app messages and recipient receipts.
package notifications

import (
	"context"
	"strings"
	"time"
	"unicode"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	domain "drama-agent/internal/domain/notifications"
)

type Input = domain.Draft
type Message = domain.Message
type Link = domain.Link
type SystemInput = domain.SystemInput
type Item struct {
	Kind        domain.Kind  `json:"kind"`
	Link        *domain.Link `json:"link,omitempty"`
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Summary     string       `json:"summary"`
	PublishedAt time.Time    `json:"publishedAt"`
	ReadAt      *time.Time   `json:"readAt"`
}
type Detail struct {
	Item
	Body string `json:"body"`
}

// StoredItem 只把本授权页的原文带给应用投影；HTTP 列表一律输出 Item，正文不随列表序列化。
type StoredItem struct {
	Item
	Body string
}

// Page 是接收人视角的信封：比共享 ports.Page 多一个未读数，因此自持定义而非嵌入
// （嵌入字段名 Page 会与内部分页字段 Page 撞名）。
type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
	Unread   int64 `json:"unread"`
}
type ReadReceipt struct {
	ID     string    `json:"id"`
	ReadAt time.Time `json:"readAt"`
}
type ReadAllReceipt struct {
	Updated int64 `json:"updated"`
	Unread  int64 `json:"unread"`
}
type Query struct {
	Page     int
	PageSize int
	Status   string
}

func (q Query) Valid() bool {
	return q.Page > 0 && q.Page <= 1000000 && q.PageSize > 0 && q.PageSize <= 100 && (q.Status == "all" || q.Status == "read" || q.Status == "unread")
}

type Store interface {
	SystemNotification(context.Context, SystemInput) (Message, error)
	NotificationList(context.Context, string, Query) (Page[StoredItem], error)
	NotificationUnread(context.Context, string) (int64, error)
	NotificationDetail(context.Context, string, string) (Detail, error)
	NotificationRead(context.Context, string, string) (ReadReceipt, error)
	NotificationsReadAll(context.Context, string) (ReadAllReceipt, error)
	OperatorNotifications(context.Context, string, Query) (Page[Message], error)
	OperatorNotification(context.Context, string, string) (Message, error)
	NotificationCommand(context.Context, string, string) (Message, error)
	CreateNotification(context.Context, string, string, Input) (Message, error)
	UpdateNotification(context.Context, string, string, Input) (Message, error)
	PublishNotification(context.Context, string, string, uint64) (Message, error)
	WithdrawNotification(context.Context, string, string, uint64) (Message, error)
	SaveNotificationImage(context.Context, string, string, Image) (Image, error)
	NotificationImage(context.Context, string, string, string) (Image, error)
	OperatorNotificationImage(context.Context, string, string, string) (Image, error)
}
type Service struct {
	Store    Store
	Updates  ports.NotificationUpdates
	Markdown ports.NotificationMarkdown
}

// SystemNotify 只供已授权业务动作调用，不向HTTP暴露系统消息写入口。
func (s *Service) SystemNotify(ctx context.Context, in SystemInput) (Message, error) {
	input, err := in.Normalize()
	if err != nil {
		return Message{}, err
	}
	if s == nil || s.Store == nil {
		return Message{}, fault.New("NOTIFICATION_UNAVAILABLE", "系统通知暂不可用")
	}
	return s.Store.SystemNotification(ctx, input)
}

func invalid() error {
	return fault.New("NOTIFICATION_INVALID", "通知内容、接收范围或版本无效")
}
func (s *Service) List(ctx context.Context, a ports.Actor, q Query) (Page[Item], error) {
	if e := a.Require(); e != nil {
		return Page[Item]{}, e
	}
	if !q.Valid() {
		return Page[Item]{}, invalid()
	}
	if s.Markdown == nil {
		return Page[Item]{}, fault.New("NOTIFICATION_UNAVAILABLE", "通知摘要暂不可用")
	}
	stored, e := s.Store.NotificationList(ctx, a.UserID, q)
	if e != nil {
		return Page[Item]{}, e
	}
	out := Page[Item]{Items: []Item{}, Total: stored.Total, Page: stored.Page, PageSize: stored.PageSize, Unread: stored.Unread}
	for _, v := range stored.Items {
		v.Item.Summary = s.Markdown.Summary(v.Body)
		out.Items = append(out.Items, v.Item)
	}
	return out, nil
}
func (s *Service) Unread(ctx context.Context, a ports.Actor) (int64, error) {
	if e := a.Require(); e != nil {
		return 0, e
	}
	return s.Store.NotificationUnread(ctx, a.UserID)
}
func (s *Service) Detail(ctx context.Context, a ports.Actor, id string) (Detail, error) {
	if e := a.Require(); e != nil {
		return Detail{}, e
	}
	if s.Markdown == nil {
		return Detail{}, fault.New("NOTIFICATION_UNAVAILABLE", "通知摘要暂不可用")
	}
	v, e := s.Store.NotificationDetail(ctx, a.UserID, id)
	if e != nil {
		return Detail{}, e
	}
	v.Summary = s.Markdown.Summary(v.Body)
	return v, nil
}
func (s *Service) Read(ctx context.Context, a ports.Actor, id string) (ReadReceipt, error) {
	if e := a.Require(); e != nil {
		return ReadReceipt{}, e
	}
	return s.Store.NotificationRead(ctx, a.UserID, id)
}
func (s *Service) ReadAll(ctx context.Context, a ports.Actor) (ReadAllReceipt, error) {
	if e := a.Require(); e != nil {
		return ReadAllReceipt{}, e
	}
	return s.Store.NotificationsReadAll(ctx, a.UserID)
}
func (s *Service) OperatorList(ctx context.Context, a ports.Actor, q Query) (Page[Message], error) {
	if e := a.RequireOperator(); e != nil {
		return Page[Message]{}, e
	}
	if !q.Valid() || q.Status != "all" {
		return Page[Message]{}, invalid()
	}
	return s.Store.OperatorNotifications(ctx, a.UserID, q)
}
func (s *Service) OperatorDetail(ctx context.Context, a ports.Actor, id string) (Message, error) {
	if e := a.RequireOperator(); e != nil {
		return Message{}, e
	}
	return s.Store.OperatorNotification(ctx, a.UserID, id)
}
func (s *Service) Create(ctx context.Context, a ports.Actor, in Input) (Message, error) {
	if e := a.RequireOperator(); e != nil {
		return Message{}, e
	}
	v, e := in.Normalize()
	if e != nil {
		return Message{}, e
	}
	if len(v.CommandID) < 8 || len(v.CommandID) > 128 || strings.ContainsFunc(v.CommandID, unicode.IsControl) || v.Revision != 0 {
		return Message{}, invalid()
	}
	id := identity.New("notice")
	v.ImageIDs, e = s.imageReferences(v.Body, id)
	if e != nil {
		return Message{}, e
	}
	return s.Store.CreateNotification(ctx, a.UserID, id, v)
}
func (s *Service) Update(ctx context.Context, a ports.Actor, id string, in Input) (Message, error) {
	if e := a.RequireOperator(); e != nil {
		return Message{}, e
	}
	if _, e := s.Store.OperatorNotification(ctx, a.UserID, id); e != nil {
		return Message{}, e
	}
	v, e := in.Normalize()
	if e != nil {
		return Message{}, e
	}
	if v.Revision == 0 || v.Revision >= ports.MaxSafeInteger || v.CommandID != "" {
		return Message{}, invalid()
	}
	v.ImageIDs, e = s.imageReferences(v.Body, id)
	if e != nil {
		return Message{}, e
	}
	return s.Store.UpdateNotification(ctx, a.UserID, id, v)
}
func (s *Service) Publish(ctx context.Context, a ports.Actor, id string, revision uint64) (Message, error) {
	if e := a.RequireOperator(); e != nil {
		return Message{}, e
	}
	if revision == 0 || revision >= ports.MaxSafeInteger {
		return Message{}, invalid()
	}
	m, e := s.Store.OperatorNotification(ctx, a.UserID, id)
	if e != nil {
		return Message{}, e
	}
	if m.Status == "draft" {
		if e = m.RequirePublishableBody(); e != nil {
			return Message{}, e
		}
		if _, e = s.imageReferences(m.Body, id); e != nil {
			return Message{}, e
		}
	}
	return s.Store.PublishNotification(ctx, a.UserID, id, revision)
}

func (s *Service) Command(ctx context.Context, a ports.Actor, command string) (Message, error) {
	if e := a.RequireOperator(); e != nil {
		return Message{}, e
	}
	if len(command) < 8 || len(command) > 128 {
		return Message{}, invalid()
	}
	return s.Store.NotificationCommand(ctx, a.UserID, command)
}

func (s *Service) Withdraw(ctx context.Context, a ports.Actor, id string, revision uint64) (Message, error) {
	if e := a.RequireOperator(); e != nil {
		return Message{}, e
	}
	if revision == 0 || revision >= ports.MaxSafeInteger {
		return Message{}, invalid()
	}
	return s.Store.WithdrawNotification(ctx, a.UserID, id, revision)
}

func (s *Service) Watch(ctx context.Context, a ports.Actor) (<-chan struct{}, func(), error) {
	if e := a.Require(); e != nil {
		return nil, nil, e
	}
	if s.Updates == nil {
		return nil, nil, fault.New("NOTIFICATION_UNAVAILABLE", "站内信实时同步暂不可用")
	}
	return s.Updates.WatchNotifications(ctx, a.UserID)
}
