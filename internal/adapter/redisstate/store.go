// Package redisstate owns short-lived runtime state, never business or billing facts.
package redisstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"github.com/redis/go-redis/v9"
)

type Store struct {
	client   *redis.Client
	prefix   string
	settings appconfig.Redis
	mu       sync.Mutex
	watches  map[string]map[chan struct{}]struct{}
	sub      *redis.PubSub
	closed   bool
	boot     atomic.Value
}

var _ ports.RuntimeState = (*Store)(nil)

var scripts sync.Map

// Known internal scripts use EVALSHA after their first execution. On restart,
// go-redis reloads a missing script; command arguments never enter this cache.
func (s *Store) eval(ctx context.Context, source string, keys []string, args ...any) *redis.Cmd {
	value, ok := scripts.Load(source)
	if !ok {
		value, _ = scripts.LoadOrStore(source, redis.NewScript(source))
	}
	return value.(*redis.Script).Run(ctx, s.client, keys, args...)
}

func Open(ctx context.Context, settings appconfig.Redis) (*Store, error) {
	if settings.Address == "" || settings.Namespace == "" || settings.Lease < 3*time.Second {
		return nil, fmt.Errorf("invalid Redis configuration")
	}
	s := &Store{prefix: "{" + settings.Namespace + "}:", settings: settings, watches: make(map[string]map[chan struct{}]struct{})}
	c := redis.NewClient(&redis.Options{Addr: settings.Address, Password: settings.Password, DialTimeout: settings.DialTimeout, ReadTimeout: settings.OperationTimeout, WriteTimeout: settings.OperationTimeout, MaxRetries: -1, ContextTimeoutEnabled: true, ClientName: settings.Namespace, OnConnect: func(ctx context.Context, conn *redis.Conn) error {
		info, err := conn.Info(ctx, "server").Result()
		if err != nil {
			return err
		}
		for _, line := range strings.Split(info, "\n") {
			if strings.HasPrefix(line, "run_id:") {
				s.boot.Store(strings.TrimSpace(strings.TrimPrefix(line, "run_id:")))
				return nil
			}
		}
		return fmt.Errorf("Redis server identity unavailable")
	}})
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, unavailable()
	}
	s.client = c
	return s, nil
}

func unavailable() error {
	return fault.New("RUNTIME_UNAVAILABLE", "Redis运行状态暂不可用，请稍后重试")
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (s *Store) key(kind, value string) string { return s.prefix + kind + ":" + digest(value) }

func (s *Store) Close() {
	s.mu.Lock()
	s.closed = true
	if s.sub != nil {
		_ = s.sub.Close()
		s.sub = nil
	}
	for key, listeners := range s.watches {
		for ch := range listeners {
			close(ch)
		}
		delete(s.watches, key)
	}
	s.mu.Unlock()
	_ = s.client.Close()
}

// Allow's counter and expiry are a single operation; a rejected request never extends the window.
func (s *Store) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if limit <= 0 || window <= 0 {
		return false, fmt.Errorf("invalid rate limit")
	}
	n, err := s.eval(ctx, `local n=redis.call('INCR',KEYS[1]);
 if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end;
 return n`, []string{s.key("rate", key)}, window.Milliseconds()).Int64()
	if err != nil {
		return false, unavailable()
	}
	return n <= int64(limit), nil
}
func (s *Store) ResetLimit(ctx context.Context, key string) error {
	if s.client.Del(ctx, s.key("rate", key)).Err() != nil {
		return unavailable()
	}
	return nil
}
