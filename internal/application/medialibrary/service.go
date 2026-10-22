// Package medialibrary provides a space-scoped read-only view of saved generation outputs.
package medialibrary

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
)

type Position struct {
	At       time.Time `json:"at"`
	TaskID   string    `json:"taskId"`
	OutputID string    `json:"outputId"`
	Filter   string    `json:"filter"`
}
type Query struct {
	Filter ports.ListFilter
	Kind   string
	Search string
	After  *Position
	Limit  int
}
type Item struct {
	SpaceID         string        `json:"spaceId"`
	OwnerID         string        `json:"ownerId"`
	CreatorID       string        `json:"creatorId"`
	CreatorName     string        `json:"creatorName"`
	SharedWithMe    bool          `json:"sharedWithMe"`
	EffectiveAccess spaces.Access `json:"effectiveAccess"`
	FileID          string        `json:"-"`
	TaskID          string        `json:"taskId"`
	OutputID        string        `json:"outputId"`
	ProjectID       string        `json:"projectId"`
	ProjectTitle    string        `json:"projectTitle"`
	Kind            string        `json:"kind"`
	CreatedAt       time.Time     `json:"createdAt"`
	ModelID         string        `json:"modelId"`
	RequestModelID  string        `json:"requestModelId"`
	AspectRatio     string        `json:"aspectRatio"`
	Resolution      string        `json:"resolution"`
	Width           int           `json:"width"`
	Height          int           `json:"height"`
	DurationSeconds float64       `json:"durationSeconds"`
	ContentType     string        `json:"contentType"`
	SizeBytes       int64         `json:"sizeBytes"`
	PromptPreview   string        `json:"promptPreview"`
	Prompt          string        `json:"prompt,omitempty"`
	ContentURL      string        `json:"contentUrl"`
	ThumbnailURL    string        `json:"thumbnailUrl"`
}
type Listing struct {
	Items      []Item  `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

// Store is a narrow business projection, not a second task store or output writer.
type Store interface {
	ListGeneratedMedia(context.Context, ports.SpaceScope, Query) ([]Item, error)
	GeneratedMedia(context.Context, ports.SpaceScope, string, string) (Item, error)
}
type Service struct {
	Tasks    *taskapp.Service
	Files    ports.ContainerFileReader
	Store    Store
	Resolver ports.SpaceResolver
	Access   ports.SpaceAccess
	scope    ports.SpaceScope
}

func (s *Service) ForScope(scope ports.SpaceScope) *Service {
	if s == nil {
		s = &Service{}
	}
	copy := *s
	copy.scope = scope
	return &copy
}

func (s *Service) List(ctx context.Context, kind, search string, selection ports.ListFilter, cursor string, limit int) (Listing, error) {
	out := Listing{Items: []Item{}}
	scope, err := ports.ResolveSpace(ctx, s.Resolver, s.scope)
	if err != nil {
		return out, err
	}
	selection, err = selection.Normalize()
	if err != nil {
		return out, err
	}
	search = strings.TrimSpace(search)
	if kind != "" && kind != "image" && kind != "video" || !utf8.ValidString(search) || utf8.RuneCountInString(search) > 200 || strings.IndexByte(search, 0) >= 0 || len(cursor) > 2048 {
		return out, fault.ErrInvalid
	}
	if limit == 0 {
		limit = 24
	}
	if limit < 1 || limit > 100 {
		return out, fault.ErrInvalid
	}
	digest := sha256.Sum256([]byte(scope.SpaceID + "\x00" + scope.UserID + "\x00" + kind + "\x00" + search + "\x00" + string(selection)))
	filter := hex.EncodeToString(digest[:])
	q := Query{Kind: kind, Search: search, Filter: selection, Limit: limit + 1}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		var p Position
		if err != nil || json.Unmarshal(raw, &p) != nil || p.At.IsZero() || p.TaskID == "" || p.OutputID == "" || len(p.TaskID) > 256 || len(p.OutputID) > 256 || p.Filter != filter {
			return out, fault.ErrInvalid
		}
		q.After = &p
	}
	if s == nil || s.Store == nil {
		return out, fault.New("TASK_UNAVAILABLE", "创作资产读取尚未配置")
	}
	rows, err := s.Store.ListGeneratedMedia(ctx, scope, q)
	if err != nil {
		return out, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		raw, err := json.Marshal(Position{At: last.CreatedAt, TaskID: last.TaskID, OutputID: last.OutputID, Filter: filter})
		if err != nil {
			return out, err
		}
		next := base64.RawURLEncoding.EncodeToString(raw)
		out.NextCursor = &next
	}
	out.Items = rows
	if out.Items == nil {
		out.Items = []Item{}
	}
	return out, nil
}
func (s *Service) Detail(ctx context.Context, task, output string) (Item, error) {
	if err := (ports.Actor{UserID: s.scope.UserID, Role: ports.RoleUser}).Require(); err != nil {
		return Item{}, err
	}
	if task == "" || output == "" || len(task) > 256 || len(output) > 256 {
		return Item{}, fault.ErrInvalid
	}
	if s == nil || s.Store == nil {
		return Item{}, fault.New("TASK_UNAVAILABLE", "创作资产读取尚未配置")
	}
	// 详情先按稳定 ID 读取资源归属；查询层不把当前选择误当作授权。
	item, err := s.Store.GeneratedMedia(ctx, ports.SpaceScope{UserID: s.scope.UserID}, task, output)
	if err != nil {
		return Item{}, err
	}
	if s.Access == nil {
		return Item{}, fault.New("AUTH_REQUIRED", "空间权限服务尚未组装")
	}
	permission, _, err := s.Access.Access(ctx, ports.Actor{UserID: s.scope.UserID, Role: ports.RoleUser}, spaces.Resource{SpaceID: item.SpaceID, OwnerID: item.OwnerID, Kind: spaces.MediaResource, ID: item.TaskID})
	if err != nil {
		return Item{}, err
	}
	if permission < spaces.Read {
		return Item{}, fault.ErrNotFound
	}
	item.EffectiveAccess = permission
	return item, nil
}
