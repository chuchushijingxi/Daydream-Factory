package redisstate

import (
	"context"
	"log"
	"sync"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
)

func (s *Store) Lock(ctx context.Context, name string, wait bool) (ports.Lease, bool, error) {
	key, token := s.key("lock", name), identity.New("lease")
	for {
		ok, err := s.client.SetNX(ctx, key, token, s.settings.Lease).Result()
		if err != nil {
			return ports.Lease{}, false, unavailable()
		}
		if ok {
			break
		}
		if !wait {
			return ports.Lease{}, false, nil
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ports.Lease{}, false, ctx.Err()
		case <-timer.C:
		}
	}
	execution, cancel := context.WithCancel(ctx)
	boot, _ := s.boot.Load().(string)
	check := func(c context.Context) error {
		if execution.Err() != nil {
			return fault.New("LEASE_LOST", "执行协调租约已失效")
		}
		value, err := s.client.Get(c, key).Result()
		current, _ := s.boot.Load().(string)
		if err != nil || value != token || current != boot {
			cancel()
			return fault.New("LEASE_LOST", "执行协调租约已失效")
		}
		return nil
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		tick := time.NewTicker(s.settings.Lease / 3)
		defer tick.Stop()
		for {
			select {
			case <-execution.Done():
				return
			case <-tick.C:
				n, err := s.eval(execution, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('PEXPIRE',KEYS[1],ARGV[2]) end;
return 0`, []string{key}, token, s.settings.Lease.Milliseconds()).Int()
				current, _ := s.boot.Load().(string)
				if err != nil || n != 1 || current != boot {
					cancel()
					return
				}
			}
		}
	}()
	release := func() {
		once.Do(func() {
			cancel()
			<-done
			c, stop := context.WithTimeout(context.WithoutCancel(ctx), s.settings.OperationTimeout)
			defer stop()
			if err := s.eval(c, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end;
return 0`, []string{key}, token).Err(); err != nil {
				log.Print("Redis lease release unconfirmed; TTL retains the recovery boundary")
			}
		})
	}
	return ports.Lease{Context: execution, Check: check, Release: release}, true, nil
}

// HoldCapacity is a renewable semaphore. Expired owners cannot remove a newer permit.
func (s *Store) HoldCapacity(ctx context.Context, account, stage string, limit int) (ports.Lease, bool, error) {
	key, token := s.key("capacity", account+":"+stage), identity.New("permit")
	lease := s.settings.Lease
	script := `local now=redis.call('TIME');
local ms=now[1]*1000+math.floor(now[2]/1000);
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ms);
if redis.call('ZCARD',KEYS[1])>=tonumber(ARGV[1]) then return 0 end;
redis.call('ZADD',KEYS[1],ms+tonumber(ARGV[3]),ARGV[2]);
redis.call('PEXPIRE',KEYS[1],ARGV[3]*2);
return 1`
	n, err := s.eval(ctx, script, []string{key}, limit, token, lease.Milliseconds()).Int()
	if err != nil {
		return ports.Lease{}, false, unavailable()
	}
	if n != 1 {
		return ports.Lease{}, false, nil
	}
	execution, cancel := context.WithCancel(ctx)
	boot, _ := s.boot.Load().(string)
	done := make(chan struct{})
	var once sync.Once
	check := func(c context.Context) error {
		n, err := s.eval(c, `local now=redis.call('TIME');
local ms=now[1]*1000+math.floor(now[2]/1000);
local score=redis.call('ZSCORE',KEYS[1],ARGV[1]);
if score and tonumber(score)>ms then return 1 end;
return 0`, []string{key}, token).Int()
		current, _ := s.boot.Load().(string)
		if execution.Err() != nil || err != nil || n != 1 || current != boot {
			cancel()
			return fault.New("LEASE_LOST", "厂商并发许可已失效")
		}
		return nil
	}
	go func() {
		defer close(done)
		tick := time.NewTicker(lease / 3)
		defer tick.Stop()
		for {
			select {
			case <-execution.Done():
				return
			case <-tick.C:
				n, err := s.eval(execution, `local now=redis.call('TIME');
local ms=now[1]*1000+math.floor(now[2]/1000);
local old=redis.call('ZSCORE',KEYS[1],ARGV[1]);
if not old or tonumber(old)<=ms then return 0 end;
redis.call('ZADD',KEYS[1],ms+ARGV[2],ARGV[1]);
redis.call('PEXPIRE',KEYS[1],ARGV[2]*2);
return 1`, []string{key}, token, lease.Milliseconds()).Int()
				current, _ := s.boot.Load().(string)
				if err != nil || n != 1 || current != boot {
					cancel()
					return
				}
			}
		}
	}()
	release := func() {
		once.Do(func() {
			cancel()
			<-done
			c, stop := context.WithTimeout(context.WithoutCancel(ctx), s.settings.OperationTimeout)
			defer stop()
			if err := s.client.ZRem(c, key, token).Err(); err != nil {
				log.Print("Redis capacity release unconfirmed; TTL retains the recovery boundary")
			}
		})
	}
	return ports.Lease{Context: execution, Check: check, Release: release}, true, nil
}
