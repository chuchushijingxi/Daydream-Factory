package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/fault"
)

// 空间选择不证明成员身份；实际用例按所选空间或资源自身空间鉴权。
func requestSpace(r *http.Request, personalID string) (string, error) {
	headers, hasHeader := r.Header[http.CanonicalHeaderKey("X-Drama-Space")]
	values, err := url.ParseQuery(r.URL.RawQuery)
	query, hasQuery := values["space"]
	invalid := fault.New("INVALID_SPACE", "空间选择无效")
	valid := func(v string) bool {
		return v != "" && len(v) <= 256 && strings.IndexFunc(v, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
	}
	if err != nil || (hasHeader && (len(headers) != 1 || !valid(headers[0]))) || (hasQuery && (len(query) != 1 || !valid(query[0]))) {
		return "", invalid
	}
	if hasHeader && hasQuery && headers[0] != query[0] {
		return "", invalid
	}
	if hasHeader {
		return headers[0], nil
	}
	if hasQuery {
		return query[0], nil
	}
	return personalID, nil
}

func requestScope(r *http.Request) ports.SpaceScope {
	return ports.SpaceScope{UserID: actor(r).UserID, SpaceID: ports.RequestedSpace(r.Context())}
}
func (h *Handler) projectService(r *http.Request) *app.Service {
	return h.Service.ForScope(requestScope(r))
}
func (h *Handler) fileService(r *http.Request) *uploadapp.Service {
	return h.Files.ForScope(requestScope(r))
}
