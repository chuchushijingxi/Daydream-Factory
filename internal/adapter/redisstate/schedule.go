package redisstate

import (
	"context"
	"strconv"
	"time"

	"drama-agent/internal/application/ports"
	"github.com/redis/go-redis/v9"
)

func (s *Store) ScheduleTask(ctx context.Context, id string, version uint64, at *time.Time) error {
	score := ""
	if at != nil {
		score = strconv.FormatInt(at.UnixMicro(), 10)
	}
	err := s.eval(ctx, `local old=redis.call('HGET',KEYS[2],ARGV[1]);
if old and tonumber(old)>tonumber(ARGV[2]) then return 0 end;
redis.call('HSET',KEYS[2],ARGV[1],ARGV[2]);
if ARGV[3]=='' then redis.call('ZREM',KEYS[1],ARGV[1]) else redis.call('ZADD',KEYS[1],ARGV[3],ARGV[1]) end;
return 1`, []string{s.prefix + "tasks:due", s.prefix + "tasks:versions"}, id, version, score).Err()
	if err != nil {
		return unavailable()
	}
	return nil
}
func (s *Store) ScheduledTasks(ctx context.Context, cursor *ports.TaskCursor, limit int) ([]ports.TaskDue, error) {
	min, id := "-inf", ""
	if cursor != nil {
		min = strconv.FormatInt(cursor.At.UnixMicro(), 10)
		id = cursor.ID
	}
	values, err := s.eval(ctx, `local out={};
local offset=0;
local n=tonumber(ARGV[4]);
repeat local rows=redis.call('ZRANGEBYSCORE',KEYS[1],ARGV[1],ARGV[3],'WITHSCORES','LIMIT',offset,n);
for i=1,#rows,2 do if ARGV[2]=='' or tonumber(rows[i+1])>tonumber(ARGV[1]) or rows[i]>ARGV[2] then table.insert(out,rows[i]);
table.insert(out,rows[i+1]);
if #out>=n*2 then return out end end end;
offset=offset+n;
until #rows<n*2;
return out`, []string{s.prefix + "tasks:due"}, min, id, time.Now().UnixMicro(), limit).StringSlice()
	if err != nil {
		return nil, unavailable()
	}
	out := []ports.TaskDue{}
	for i := 0; i < len(values); i += 2 {
		score, err := strconv.ParseFloat(values[i+1], 64)
		if err != nil {
			return nil, err
		}
		out = append(out, ports.TaskDue{ID: values[i], At: time.UnixMicro(int64(score)).UTC()})
	}
	return out, nil
}
func (s *Store) ScheduleReady(ctx context.Context) (bool, error) {
	marker, err := s.client.Get(ctx, s.prefix+"tasks:ready").Result()
	if err != nil && err != redis.Nil {
		return false, unavailable()
	}
	boot, _ := s.boot.Load().(string)
	return err == nil && marker == boot, nil
}
func (s *Store) MarkScheduleReady(ctx context.Context) error {
	boot, _ := s.boot.Load().(string)
	if s.client.Set(ctx, s.prefix+"tasks:ready", boot, 0).Err() != nil {
		return unavailable()
	}
	return nil
}
