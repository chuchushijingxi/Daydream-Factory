package ports

import (
	"context"
	"sync"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
)

type SpaceScope = spaces.Scope

type SpaceResolver interface {
	// Resolve 核验成员身份与当时的空间状态；未加入或已停用按不存在处理。
	Resolve(ctx context.Context, a Actor, spaceID string) (SpaceScope, error)
}

type SpaceAccess interface {
	// Access 按资源自身空间判断；None 必须向外表现为 404，不泄露资源存在。
	Access(ctx context.Context, a Actor, r spaces.Resource) (spaces.Access, SpaceScope, error)
}

type requestedSpaceKey struct{}

type spaceIdentity struct{ userID, spaceID string }
type spaceResolution struct {
	scope SpaceScope
	err   error
}
type spaceRequest struct {
	spaceID string
	mu      sync.Mutex
	results map[spaceIdentity]spaceResolution
}

// WithRequestedSpace 记录客户端选择，并开始一次 HTTP 操作内的空间身份解析范围。
// 它不授予权限；长连接每轮轮询必须重新调用，后台/Agent 不携带此范围。
// 解析事实不跨请求复用，避免成员移出、停用等变更延迟生效。
func WithRequestedSpace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestedSpaceKey{}, &spaceRequest{spaceID: id, results: make(map[spaceIdentity]spaceResolution)})
}

func RequestedSpace(ctx context.Context) string {
	if request, _ := ctx.Value(requestedSpaceKey{}).(*spaceRequest); request != nil {
		return request.spaceID
	}
	return ""
}

// ResolveSpaceOnce 只复用当前 HTTP 操作内已核验的成员/生命周期事实。
// 用户和实际资源空间共同作为键；没有显式请求范围时每次重新读取。
func ResolveSpaceOnce(ctx context.Context, a Actor, spaceID string, load func() (SpaceScope, error)) (SpaceScope, error) {
	request, _ := ctx.Value(requestedSpaceKey{}).(*spaceRequest)
	if request == nil {
		return load()
	}
	key := spaceIdentity{userID: a.UserID, spaceID: spaceID}
	request.mu.Lock()
	defer request.mu.Unlock()
	if result, ok := request.results[key]; ok {
		return result.scope, result.err
	}
	scope, err := load()
	request.results[key] = spaceResolution{scope: scope, err: err}
	return scope, err
}

// ResolveSpace 将列表/创建所用的选择值解析成可信 Scope，供资源用例复用。
// 内容权限不区分平台普通/运营角色，平台运营不能据此取得空间内容。
func ResolveSpace(ctx context.Context, resolver SpaceResolver, requested SpaceScope) (SpaceScope, error) {
	actor := Actor{UserID: requested.UserID, Role: RoleUser}
	if err := actor.Require(); err != nil {
		return SpaceScope{}, err
	}
	if resolver == nil {
		return SpaceScope{}, fault.New("AUTH_REQUIRED", "空间权限服务尚未组装")
	}
	return resolver.Resolve(ctx, actor, requested.SpaceID)
}

// RequireSpaceAccess 让按 ID 的入口使用资源空间鉴权，并统一写入的空间切换保护。
func RequireSpaceAccess(ctx context.Context, access SpaceAccess, requested SpaceScope, resource spaces.Resource, required spaces.Access) (SpaceScope, error) {
	actor := Actor{UserID: requested.UserID, Role: RoleUser}
	if err := actor.Require(); err != nil {
		return SpaceScope{}, err
	}
	if access == nil {
		return SpaceScope{}, fault.New("AUTH_REQUIRED", "空间权限服务尚未组装")
	}
	permission, scope, err := access.Access(ctx, actor, resource)
	if err != nil {
		return SpaceScope{}, err
	}
	if permission == spaces.None {
		return SpaceScope{}, fault.ErrNotFound
	}
	if permission < required {
		if scope.ReadOnly {
			return SpaceScope{}, fault.New("TEAM_READ_ONLY", "团队已只读")
		}
		return SpaceScope{}, fault.New("ROLE_FORBIDDEN", "无权编辑该资源")
	}
	selected := requested.SpaceID
	if selected == "" {
		selected = requested.UserID
	}
	if required == spaces.Edit && selected != resource.SpaceID {
		return SpaceScope{}, fault.New("SPACE_CHANGED", "当前空间已切换，请重新打开资源")
	}
	return scope, nil
}
