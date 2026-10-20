package redisstate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	c "drama-agent/internal/application/credits"
	d "drama-agent/internal/domain/credits"
)

var _ c.DailyGiftCache = (*Store)(nil)

func (s *Store) readDailyGift(ctx context.Context, key string) (c.DailyGift, int64, bool, error) {
	values, err := s.eval(ctx, `local now=redis.call('TIME');
return {redis.call('GET',KEYS[1]) or '',now[1]*1000+math.floor(now[2]/1000)}`, []string{key}).Slice()
	if err != nil {
		return c.DailyGift{}, 0, false, unavailable()
	}
	raw, started := values[0].(string), values[1].(int64)
	if raw == "" {
		return c.DailyGift{}, started, false, nil
	}
	var out c.DailyGift
	err = json.Unmarshal([]byte(raw), &out)
	date, dateErr := time.Parse("2006-01-02", out.Date)
	if err != nil || dateErr != nil || date.Format("2006-01-02") != out.Date || out.Amount <= 0 || out.Amount > d.MaxAmount || !out.Claimed || !out.AlreadyClaimed {
		// 派生缓存损坏只清除此用户的值，重建仍须核对PG；不能当作已领取。
		removed, err := s.eval(ctx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end;
return 0`, []string{key}, raw).Int()
		if err != nil {
			return c.DailyGift{}, 0, false, unavailable()
		}
		if removed == 1 {
			log.Print("invalid Redis daily gift receipt; rebuilding from PostgreSQL")
		}
		return c.DailyGift{}, started, false, nil
	}
	return out, started, true, nil
}

func (s *Store) ReadDailyGift(ctx context.Context, user string, load func(context.Context) (c.DailyGift, error)) (c.DailyGift, error) {
	key := s.key("daily-gift", user)
	if out, _, found, err := s.readDailyGift(ctx, key); err != nil || found {
		return out, err
	}
	lease, _, err := s.Lock(ctx, "daily-gift:"+user, true)
	if err != nil {
		return c.DailyGift{}, err
	}
	defer lease.Release()
	out, started, found, err := s.readDailyGift(ctx, key)
	if err != nil || found {
		return out, err
	}
	loadCtx, cancel := context.WithTimeout(lease.Context, 15*time.Second)
	defer cancel()
	out, err = load(loadCtx)
	if err != nil {
		return c.DailyGift{}, err
	}
	if !out.Claimed && out.Amount == 0 && !out.AlreadyClaimed {
		return out, nil
	}
	if !out.Claimed || out.Amount <= 0 || out.Amount > d.MaxAmount || out.CacheTTL <= 0 || out.CacheTTL > 24*time.Hour {
		return c.DailyGift{}, fmt.Errorf("daily gift loader returned an invalid positive receipt")
	}
	if err = lease.Check(ctx); err != nil {
		return c.DailyGift{}, err
	}
	cached := out
	cached.AlreadyClaimed = true
	raw, err := json.Marshal(cached)
	if err != nil {
		return c.DailyGift{}, err
	}
	// Redis起点早于PG资格时刻：查询和提交耗时消耗预算，即使延迟回填
	// 也只能更早失效，不依赖两台机器的时区或墙上时钟偏差。
	err = s.eval(ctx, `local now=redis.call('TIME');
local deadline=tonumber(ARGV[1])+tonumber(ARGV[2]);
if deadline>now[1]*1000+math.floor(now[2]/1000) then
  redis.call('SET',KEYS[1],ARGV[3],'PXAT',deadline)
end;
return 1`, []string{key}, started, out.CacheTTL.Milliseconds(), raw).Err()
	if err != nil {
		return c.DailyGift{}, unavailable()
	}
	return out, nil
}
