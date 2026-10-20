package redisstate

import (
	"context"
	"sync"

	"github.com/redis/go-redis/v9"
)

func (s *Store) Watch(ctx context.Context, key string) (<-chan struct{}, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, unavailable()
	}
	if s.sub == nil {
		sub := s.client.Subscribe(ctx, s.prefix+"updates")
		if _, err := sub.Receive(ctx); err != nil {
			_ = sub.Close()
			return nil, nil, unavailable()
		}
		s.sub = sub
		go s.listen(sub)
	}
	ch := make(chan struct{}, 1)
	if s.watches[key] == nil {
		s.watches[key] = make(map[chan struct{}]struct{})
	}
	s.watches[key][ch] = struct{}{}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.watches[key], ch)
			if len(s.watches[key]) == 0 {
				delete(s.watches, key)
			}
		})
	}, nil
}

func (s *Store) resetWatches() {
	for key, listeners := range s.watches {
		for ch := range listeners {
			close(ch)
		}
		delete(s.watches, key)
	}
}
func (s *Store) listen(sub *redis.PubSub) {
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.sub == sub {
			s.resetWatches()
			s.sub = nil
		}
		_ = sub.Close()
	}()
	for {
		value, err := sub.Receive(context.Background())
		if err != nil {
			return
		}
		s.mu.Lock()
		switch v := value.(type) {
		case *redis.Message:
			keys := []string{v.Payload}
			if v.Payload == "notifications:*" {
				keys = nil
				for key := range s.watches {
					if len(key) >= 14 && key[:14] == "notifications:" {
						keys = append(keys, key)
					}
				}
			}
			for _, key := range keys {
				for ch := range s.watches[key] {
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		case *redis.Subscription:
			// Re-subscription follows a gap: force consumers to reconnect and re-read.
			s.resetWatches()
		}
		s.mu.Unlock()
	}
}
func (s *Store) Publish(ctx context.Context, key string) error {
	if s.client.Publish(ctx, s.prefix+"updates", key).Err() != nil {
		return unavailable()
	}
	return nil
}
func (s *Store) WatchCreditAccount(ctx context.Context, user string) (<-chan struct{}, func(), error) {
	return s.Watch(ctx, "credits:"+user)
}
func (s *Store) WatchNotifications(ctx context.Context, user string) (<-chan struct{}, func(), error) {
	return s.Watch(ctx, "notifications:"+user)
}
