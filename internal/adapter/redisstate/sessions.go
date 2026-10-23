package redisstate

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"time"

	a "drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

type session struct {
	Digest    string
	Role      ports.Role
	ExpiresAt time.Time
	Boot      string
}

func (s *Store) sessionKey(id, audience string) string { return s.key("session", id+":"+audience) }

func (s *Store) PutSession(ctx context.Context, id, audience, digest string, role ports.Role, until time.Time) (bool, error) {
	if !a.ValidAudience(audience) || !until.After(time.Now()) || digest == "" || id == "" {
		return false, fault.ErrInvalid
	}
	boot, _ := s.boot.Load().(string)
	raw, err := json.Marshal(session{Digest: digest, Role: role, ExpiresAt: until, Boot: boot})
	if err != nil {
		return false, err
	}
	// A short tombstone window preserves the existing SESSION_EXPIRED response;
	// the logical expiry below never authorizes a request beyond ExpiresAt.
	n, err := s.eval(ctx, `local old=redis.call('EXISTS',KEYS[1]);
 redis.call('SET',KEYS[1],ARGV[1],'PXAT',ARGV[2]);
redis.call('DEL',KEYS[2]);
 return old`, []string{s.sessionKey(id, audience), s.key("disabled", id)}, raw, until.Add(time.Hour).UnixMilli()).Int()
	if err != nil {
		return false, unavailable()
	}
	// Replacing a login needs no fsync: Redis always holds exactly one digest per
	// audience, so even a lost write leaves a single valid login.
	if current, _ := s.boot.Load().(string); !s.durable.Load() && current != boot {
		_, _ = s.DeleteSessionDigest(ctx, id, audience, digest)
		return false, unavailable()
	}
	return n == 1, nil
}

// persistRevocation returns once Redis has fsynced the preceding revocation to
// its AOF, so a crash right afterwards cannot bring the session back.
func (s *Store) persistRevocation(ctx context.Context) error {
	if !s.durable.Load() {
		return nil
	}
	wait := max(s.settings.OperationTimeout*3/4, time.Second)
	reply, err := s.client.Do(ctx, "WAITAOF", 1, 0, wait.Milliseconds()).Slice()
	if err != nil || len(reply) != 2 {
		return unavailable()
	}
	if local, ok := reply[0].(int64); !ok || local < 1 {
		return unavailable()
	}
	return nil
}

func (s *Store) Authenticate(ctx context.Context, token string) (ports.Actor, error) {
	id, audience, digest, err := a.SessionParts(token)
	if err != nil {
		return ports.Actor{}, err
	}
	values, err := s.client.MGet(ctx, s.sessionKey(id, audience), s.key("disabled", id)).Result()
	if err != nil {
		return ports.Actor{}, unavailable()
	}
	if values[1] != nil {
		return ports.Actor{}, fault.New("ACCOUNT_DISABLED", "账号已禁用")
	}
	if values[0] == nil {
		return ports.Actor{}, fault.New("SESSION_REVOKED", "会话已撤销，请重新登录")
	}
	raw := []byte(values[0].(string))
	var current session
	if json.Unmarshal(raw, &current) != nil {
		return ports.Actor{}, unavailable()
	}
	if boot, _ := s.boot.Load().(string); !s.durable.Load() && current.Boot != boot {
		return ports.Actor{}, fault.New("SESSION_REVOKED", "会话服务已重启，请重新登录")
	}
	if subtle.ConstantTimeCompare([]byte(current.Digest), []byte(digest)) != 1 {
		return ports.Actor{}, fault.New("SESSION_REPLACED", "账号已在其他位置登录")
	}
	if !time.Now().Before(current.ExpiresAt) {
		return ports.Actor{}, fault.New("SESSION_EXPIRED", "登录已过期")
	}
	actor := ports.Actor{UserID: id, Role: current.Role}
	if err := actor.Require(); err != nil {
		return ports.Actor{}, err
	}
	if audience == a.Operations {
		if err := actor.RequireOperator(); err != nil {
			return ports.Actor{}, err
		}
	}
	return actor, nil
}

func (s *Store) SetSessionsDisabled(ctx context.Context, id string, disabled bool) error {
	var err error
	if disabled {
		err = s.client.Set(ctx, s.key("disabled", id), "1", 0).Err()
	} else {
		err = s.client.Del(ctx, s.key("disabled", id)).Err()
	}
	if err != nil {
		return unavailable()
	}
	return s.persistRevocation(ctx)
}

func (s *Store) DeleteSession(ctx context.Context, token string) (bool, error) {
	id, audience, digest, err := a.SessionParts(token)
	if err != nil {
		return false, err
	}
	return s.DeleteSessionDigest(ctx, id, audience, digest)
}
func (s *Store) DeleteSessionDigest(ctx context.Context, id, audience, digest string) (bool, error) {
	n, err := s.eval(ctx, `local raw=redis.call('GET',KEYS[1]);
 if not raw then return 0 end;
 if cjson.decode(raw).Digest~=ARGV[1] then return 0 end;
 return redis.call('DEL',KEYS[1])`, []string{s.sessionKey(id, audience)}, digest).Int()
	if err != nil {
		return false, unavailable()
	}
	if n == 1 {
		if err = s.persistRevocation(ctx); err != nil {
			return false, err
		}
	}
	return n == 1, nil
}

func (s *Store) DeleteUserSessions(ctx context.Context, id string) ([]string, error) {
	n, err := s.eval(ctx, `local a=redis.call('DEL',KEYS[1]);
local b=redis.call('DEL',KEYS[2]);
return a+b*2`, []string{s.sessionKey(id, a.Workspace), s.sessionKey(id, a.Operations)}).Int()
	if err != nil {
		return nil, unavailable()
	}
	if n != 0 {
		if err = s.persistRevocation(ctx); err != nil {
			return nil, err
		}
	}
	out := []string{}
	if n&1 != 0 {
		out = append(out, a.Workspace)
	}
	if n&2 != 0 {
		out = append(out, a.Operations)
	}
	return out, nil
}
