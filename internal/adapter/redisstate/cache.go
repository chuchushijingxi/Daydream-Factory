package redisstate

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func (s *Store) ReadCached(ctx context.Context, name string, ttl time.Duration, load func(context.Context) ([]byte, error)) ([]byte, error) {
	key, version := s.key("cache", name), s.key("cache-generation", name)
	family := strings.SplitN(name, ":", 2)[0]
	familyVersion := s.key("cache-generation", family)
	read := func() ([]byte, error) {
		value, err := s.eval(ctx, `local value=redis.call('HGET',KEYS[1],'raw');
if not value then return false end;
if (redis.call('HGET',KEYS[1],'generation') or '')~=(redis.call('GET',KEYS[2]) or '') or (redis.call('HGET',KEYS[1],'family') or '')~=(redis.call('GET',KEYS[3]) or '') then redis.call('DEL',KEYS[1]);
return false end;
return value`, []string{key, version, familyVersion}).Text()
		return []byte(value), err
	}
	if raw, err := read(); err == nil {
		return raw, nil
	} else if !errors.Is(err, redis.Nil) {
		return nil, unavailable()
	}
	lease, _, err := s.Lock(ctx, "cache-fill:"+name, true)
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	if raw, err := read(); err == nil {
		return raw, nil
	} else if !errors.Is(err, redis.Nil) {
		return nil, unavailable()
	}
	generations, err := s.client.MGet(ctx, version, familyVersion).Result()
	if err != nil {
		return nil, unavailable()
	}
	generation, group := "", ""
	if generations[0] != nil {
		generation = generations[0].(string)
	}
	if generations[1] != nil {
		group = generations[1].(string)
	}
	loadCtx, cancel := context.WithTimeout(lease.Context, 15*time.Second)
	defer cancel()
	raw, err := load(loadCtx)
	if err != nil {
		return nil, err
	}
	if err = lease.Check(ctx); err != nil {
		return nil, err
	}
	// Invalidation advances a generation. A read started before it cannot restore an old value.
	err = s.eval(ctx, `local generation=redis.call('GET',KEYS[2]) or '';
local family=redis.call('GET',KEYS[3]) or '';
if generation==ARGV[1] and family==ARGV[4] then redis.call('HSET',KEYS[1],'generation',ARGV[1],'family',ARGV[4],'raw',ARGV[2]);
redis.call('PEXPIRE',KEYS[1],ARGV[3]) end;
return 1`, []string{key, version, familyVersion}, generation, raw, ttl.Milliseconds(), group).Err()
	if err != nil {
		return nil, unavailable()
	}
	return raw, nil
}

func (s *Store) Invalidate(ctx context.Context, names ...string) error {
	for _, name := range names {
		if err := s.eval(ctx, `redis.call('INCR',KEYS[2]);
redis.call('PEXPIRE',KEYS[2],86400000);
redis.call('DEL',KEYS[1]);
return 1`, []string{s.key("cache", name), s.key("cache-generation", name)}).Err(); err != nil {
			return unavailable()
		}
	}
	return nil
}
