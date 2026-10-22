package redisstate

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

const projectEditingTTL = 90 * time.Second

var _ ports.ProjectEditing = (*Store)(nil)

// 编辑租约由页面显式续期，不使用后台自动续租，离开页面/停止交互后必然到期。
// 所有变更在一个脚本内比对完整user/session，旧会话不能续租或释放新占用者。
const projectEditingScript = `local raw=redis.call('GET',KEYS[1]);
local owner=nil;
if raw then owner=cjson.decode(raw) end;
local same=owner and owner.userId==ARGV[2] and owner.sessionId==ARGV[3];
if ARGV[1]=='acquire' and (not owner or same) then
 owner={userId=ARGV[2],sessionId=ARGV[3]};
 redis.call('SET',KEYS[1],cjson.encode(owner),'PX',ARGV[4]);
elseif ARGV[1]=='renew' and same then
 redis.call('PEXPIRE',KEYS[1],ARGV[4]);
elseif ARGV[1]=='release' then
 if same then redis.call('DEL',KEYS[1]) end;
 return {'','','0'};
end;
if not owner then return {'','','0'} end;
local now=redis.call('TIME');
local expires=now[1]*1000+math.floor(now[2]/1000)+redis.call('PTTL',KEYS[1]);
return {owner.userId,owner.sessionId,tostring(expires)}`

func (s *Store) projectEditing(ctx context.Context, action, project, user, session string) (ports.ProjectEditor, error) {
	if project == "" || user == "" || session == "" {
		return ports.ProjectEditor{}, fault.ErrInvalid
	}
	values, err := s.eval(ctx, projectEditingScript, []string{s.key("project-editor", project)}, action, user, session, projectEditingTTL.Milliseconds()).Slice()
	if err != nil || len(values) != 3 {
		return ports.ProjectEditor{}, unavailable()
	}
	userID, ok := values[0].(string)
	if !ok {
		return ports.ProjectEditor{}, unavailable()
	}
	sessionID, ok := values[1].(string)
	if !ok {
		return ports.ProjectEditor{}, unavailable()
	}
	if userID == "" {
		return ports.ProjectEditor{}, nil
	}
	expires, err := strconv.ParseInt(fmt.Sprint(values[2]), 10, 64)
	if err != nil {
		return ports.ProjectEditor{}, unavailable()
	}
	return ports.ProjectEditor{UserID: userID, SessionID: sessionID, ExpiresAt: time.UnixMilli(expires).UTC()}, nil
}
func (s *Store) AcquireProjectEditing(ctx context.Context, project, user, session string) (ports.ProjectEditor, error) {
	return s.projectEditing(ctx, "acquire", project, user, session)
}
func (s *Store) RenewProjectEditing(ctx context.Context, project, user, session string) (ports.ProjectEditor, error) {
	return s.projectEditing(ctx, "renew", project, user, session)
}
func (s *Store) ReleaseProjectEditing(ctx context.Context, project, user, session string) error {
	_, err := s.projectEditing(ctx, "release", project, user, session)
	return err
}
func (s *Store) CheckProjectEditing(ctx context.Context, project, user, session string) error {
	owner, err := s.projectEditing(ctx, "check", project, user, session)
	if err != nil {
		return err
	}
	if owner.UserID != user || owner.SessionID != session {
		return fault.New("PROJECT_EDITING_BY_OTHER", "项目编辑占用已失效或由他人持有，请重新取得编辑权")
	}
	return nil
}
