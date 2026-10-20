package notifications

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"drama-agent/internal/domain/fault"
)

type Draft struct {
	ImageIDs        []string `json:"-"`
	CommandID       string   `json:"commandId,omitempty"`
	Title           string   `json:"title"`
	Body            string   `json:"body"`
	Audience        string   `json:"audience"`
	RecipientUserID string   `json:"recipientUserId"`
	Revision        uint64   `json:"revision,omitempty"`
}
type Message struct {
	AuthorID        string     `json:"authorId"`
	AuthorName      string     `json:"authorName"`
	WithdrawnAt     *time.Time `json:"withdrawnAt"`
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Body            string     `json:"body"`
	Audience        string     `json:"audience"`
	RecipientUserID string     `json:"recipientUserId"`
	RecipientName   string     `json:"recipientName"`
	RecipientEmail  string     `json:"recipientEmail"`
	Status          string     `json:"status"`
	Revision        uint64     `json:"revision"`
	RecipientCount  int64      `json:"recipientCount"`
	CreatedAt       time.Time  `json:"createdAt"`
	PublishedAt     *time.Time `json:"publishedAt"`
}

func invalidContent() error {
	return fault.New("NOTIFICATION_INVALID", "通知内容、接收范围或版本无效")
}

func (v Draft) Normalize() (Draft, error) {
	v.Title = strings.TrimSpace(v.Title)
	if !utf8.ValidString(v.Title) || !utf8.ValidString(v.Body) || strings.ContainsRune(v.Body, 0) || strings.ContainsFunc(v.Title, unicode.IsControl) {
		return Draft{}, invalidContent()
	}
	if utf8.RuneCountInString(v.Title) < 1 || utf8.RuneCountInString(v.Title) > 120 || len(v.Body) > 20000 {
		return Draft{}, invalidContent()
	}
	if v.Audience != "all" && v.Audience != "user" {
		return Draft{}, invalidContent()
	}
	if v.Audience == "all" && v.RecipientUserID != "" {
		return Draft{}, invalidContent()
	}
	if v.Audience == "user" && (v.RecipientUserID == "" || len(v.RecipientUserID) > 128) {
		return Draft{}, invalidContent()
	}
	return v, nil
}
func (m Message) RequireDraft(revision uint64) error {
	if m.Status != "draft" || m.Revision != revision {
		return conflict()
	}
	return nil
}

// PublicationNeeded 在同一版本重复发布时回读原结果，不再写回执或推送。
func (m Message) PublicationNeeded(revision uint64) (bool, error) {
	if m.Status == "published" && m.Revision == revision+1 {
		return false, nil
	}
	if err := m.RequireDraft(revision); err != nil {
		return false, err
	}
	return true, m.RequirePublishableBody()
}

func (m Message) RequirePublishableBody() error {
	if strings.TrimSpace(m.Body) == "" {
		return fault.New("NOTIFICATION_INVALID", "发布正文不能为空")
	}
	return nil
}

func (m Message) WithdrawalNeeded(revision uint64) (bool, error) {
	if m.Status == "withdrawn" && m.Revision == revision+1 {
		return false, nil
	}
	if m.Status != "published" || m.Revision != revision {
		return false, conflict()
	}
	return true, nil
}

func conflict() error {
	return fault.New("NOTIFICATION_CONFLICT", "通知已被修改或发布，请读取最新内容")
}
