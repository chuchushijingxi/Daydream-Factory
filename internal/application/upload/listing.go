package upload

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

type Listing struct {
	Items      []ports.UploadedFileItem `json:"items"`
	NextCursor *string                  `json:"nextCursor"`
}

func (s *Service) List(ctx context.Context, filter ports.ListFilter, search, cursor string, limit int) (Listing, error) {
	out := Listing{Items: []ports.UploadedFileItem{}}
	if s == nil || s.scope == nil {
		return out, fault.New("AUTH_REQUIRED", "缺少用户空间身份")
	}
	scope, err := ports.ResolveSpace(ctx, s.Resolver, *s.scope)
	if err != nil {
		return out, err
	}
	filter, err = filter.Normalize()
	if err != nil {
		return out, err
	}
	search = strings.TrimSpace(search)
	if !utf8.ValidString(search) || utf8.RuneCountInString(search) > 200 || strings.IndexByte(search, 0) >= 0 || len(cursor) > 2048 {
		return out, fault.ErrInvalid
	}
	if limit == 0 {
		limit = 24
	}
	if limit < 1 || limit > 100 {
		return out, fault.ErrInvalid
	}
	digest := sha256.Sum256([]byte(scope.SpaceID + "\x00" + scope.UserID + "\x00" + string(filter) + "\x00" + search))
	key := hex.EncodeToString(digest[:])
	query := ports.UploadedFileQuery{Filter: filter, Search: search, Limit: limit + 1}
	if cursor != "" {
		var position ports.UploadedFilePosition
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(raw, &position) != nil || position.At.IsZero() || position.ID == "" || len(position.ID) > 128 || position.Filter != key {
			return out, fault.ErrInvalid
		}
		query.After = &position
	}
	queries, ok := s.Store.(ports.UploadedFileQueries)
	if !ok {
		return out, fault.New("FILE_STORAGE_UNAVAILABLE", "上传文件列表尚未配置")
	}
	rows, err := queries.ListUploadedFiles(ctx, scope, query)
	if err != nil {
		return out, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		raw, e := json.Marshal(ports.UploadedFilePosition{At: last.CreatedAt, ID: last.ID, Filter: key})
		if e != nil {
			return out, e
		}
		next := base64.RawURLEncoding.EncodeToString(raw)
		out.NextCursor = &next
	}
	if rows != nil {
		out.Items = rows
	}
	return out, nil
}
