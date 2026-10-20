package redisstate

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"drama-agent/internal/adapter/postgres"
	c "drama-agent/internal/application/credits"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/identity"
	"github.com/jackc/pgx/v5"
)

type countedGiftStore struct {
	c.GiftStore
	loads atomic.Int32
}

func (s *countedGiftStore) ClaimDailyCreditGift(ctx context.Context, user string) (c.DailyGift, error) {
	s.loads.Add(1)
	return s.GiftStore.ClaimDailyCreditGift(ctx, user)
}

func giftDatabase(t *testing.T, runtime *Store) (*postgres.Store, *pgx.Conn, string) {
	t.Helper()
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("isolated PG opt-in required")
	}
	parsed, err := url.Parse(database)
	if err != nil || !strings.HasPrefix(parsed.Path, "/drama_accounts_test_") {
		t.Fatal("daily gift fixtures require their dedicated isolated database")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	store.UseRuntime(runtime)
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	policy, err := store.CreditGiftPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(), `UPDATE drama_catalog_v1.credit_gift_policy SET registration_gift=$1,daily_gift=$2 WHERE id`, policy.RegistrationGift, policy.DailyGift)
		if err != nil {
			t.Error(err)
		}
	})
	return store, conn, database
}

func giftUser(t *testing.T, conn *pgx.Conn) ports.Actor {
	t.Helper()
	id := identity.New("cache_gift")
	_, err := conn.Exec(context.Background(), `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,'isolated fixture','user',$3)`, id, id+"@example.test", []byte("no-login-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return ports.Actor{UserID: id, Role: ports.RoleUser}
}

func TestDailyGiftRedisPGZeroConcurrentAndCacheLoss(t *testing.T) {
	runtime := fixture(t)
	other := second(t, runtime)
	store, conn, _ := giftDatabase(t, runtime)
	ctx := context.Background()
	setAmount := func(amount int64) {
		t.Helper()
		policy, err := store.CreditGiftPolicy(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: policy.Revision, DailyGift: amount}); err != nil {
			t.Fatal(err)
		}
	}
	setAmount(0)
	counted := &countedGiftStore{GiftStore: store}
	nodes := []*c.Gifts{{Store: counted, Cache: runtime}, {Store: counted, Cache: other}}
	actor := giftUser(t, conn)
	key := runtime.key("daily-gift", actor.UserID)
	for range 2 {
		out, err := nodes[0].Daily(ctx, actor)
		if err != nil || out.Claimed || out.AlreadyClaimed || out.Amount != 0 {
			t.Fatal("zero consumed eligibility", out, err)
		}
	}
	if n, err := runtime.client.Exists(ctx, key).Result(); err != nil || n != 0 || counted.loads.Load() != 2 {
		t.Fatal("zero was cached", n, counted.loads.Load(), err)
	}
	var claims int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_gift_claims WHERE user_id=$1`, actor.UserID).Scan(&claims); err != nil || claims != 0 {
		t.Fatal("zero inserted a claim", claims, err)
	}
	setAmount(60)
	concurrent := func(wantFirst, wantLoads int32) {
		t.Helper()
		var first atomic.Int32
		var wg sync.WaitGroup
		for i := range 24 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				out, err := nodes[i%len(nodes)].Daily(ctx, actor)
				if err != nil || out.Amount != 60 || !out.Claimed {
					t.Error("concurrent receipt", out, err)
					return
				}
				if !out.AlreadyClaimed {
					first.Add(1)
				}
			}(i)
		}
		wg.Wait()
		if first.Load() != wantFirst || counted.loads.Load() != wantLoads {
			t.Fatal("cold misses were not coalesced", first.Load(), counted.loads.Load(), wantFirst, wantLoads)
		}
	}
	concurrent(1, 3)
	// A real row lock would block the grant query. A warm cache must return without PG.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT id FROM drama_catalog_v1.users WHERE id=$1 FOR UPDATE`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	out, err := nodes[1].Daily(bounded, actor)
	cancel()
	if err != nil || !out.AlreadyClaimed || counted.loads.Load() != 3 {
		t.Fatal("warm cache reached a locked PG row", out, err, counted.loads.Load())
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	setAmount(0)
	concurrent(0, 3)
	if err = runtime.client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	concurrent(0, 4)
	if err = runtime.client.PExpire(ctx, key, time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	concurrent(0, 5)
	if err = runtime.client.Set(ctx, key, `{"amount":0,"claimed":true}`, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	concurrent(0, 6)
	var ledgers int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND source='daily-gift'`, actor.UserID).Scan(&ledgers); err != nil || ledgers != 1 {
		t.Fatal("cache loss duplicated funding", ledgers, err)
	}
	wallet, err := store.CreditWallet(ctx, actor.UserID)
	if err != nil || wallet.Balance != 60 || wallet.GiftBalance != 60 {
		t.Fatal("PG wallet differs from actual funding", wallet, err)
	}
	t.Logf("2 nodes: zero requests=2, first 24 requests=1 PG grant, warm 24=0 PG, delete/TTL/corrupt rebuilds=1 PG each; ledger=%d wallet=%d", ledgers, wallet.Balance)
}

func TestDailyGiftRedisCrossesDatabaseMidnight(t *testing.T) {
	runtime := fixture(t)
	store, conn, database := giftDatabase(t, runtime)
	ctx := context.Background()
	policy, err := store.CreditGiftPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: policy.Revision, DailyGift: 11}); err != nil {
		t.Fatal(err)
	}
	actor := giftUser(t, conn)
	schema := pgx.Identifier{identity.New("gift_clock")}.Sanitize()
	// Only this isolated test connection sees the ticking fixture clock. No host,
	// Redis or real PG server clock is changed; production has no clock override.
	if _, err = conn.Exec(ctx, `CREATE SCHEMA `+schema+`; CREATE TABLE `+schema+`.clock(at timestamptz, base timestamptz);
CREATE FUNCTION `+schema+`.clock_timestamp() RETURNS timestamptz LANGUAGE sql VOLATILE AS 'SELECT at+(pg_catalog.clock_timestamp()-base) FROM `+schema+`.clock'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	parsed, _ := url.Parse(database)
	query := parsed.Query()
	query.Set("search_path", schema+",pg_catalog")
	query.Set("timezone", "Pacific/Kiritimati")
	parsed.RawQuery = query.Encode()
	clockStore, err := postgres.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clockStore.Close)
	clockStore.UseRuntime(runtime)
	counted := &countedGiftStore{GiftStore: clockStore}
	service := &c.Gifts{Store: counted, Cache: runtime}
	if _, err = conn.Exec(ctx, `INSERT INTO `+schema+`.clock VALUES('2040-12-31T15:59:59.5Z',pg_catalog.clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	first, err := service.Daily(ctx, actor)
	if err != nil || first.Date != "2040-12-31" || first.AlreadyClaimed || first.Amount != 11 || first.CacheTTL > 500*time.Millisecond {
		t.Fatal("last-day receipt or PG expiry budget", first, err)
	}
	key := runtime.key("daily-gift", actor.UserID)
	ttl, err := runtime.client.PTTL(ctx, key).Result()
	if err != nil || ttl <= 0 || ttl > first.CacheTTL {
		t.Fatal("cache extended the PG deadline", ttl, first.CacheTTL, err)
	}
	if again, err := service.Daily(ctx, actor); err != nil || !again.AlreadyClaimed || counted.loads.Load() != 1 {
		t.Fatal("same day missed the cache", again, err, counted.loads.Load())
	}
	time.Sleep(600 * time.Millisecond)
	next, err := service.Daily(ctx, actor)
	if err != nil || next.Date != "2041-01-01" || next.AlreadyClaimed || next.Amount != 11 || counted.loads.Load() != 2 {
		t.Fatal("yesterday's cache blocked the new day", next, err, counted.loads.Load())
	}
	if again, err := service.Daily(ctx, actor); err != nil || again.Date != next.Date || !again.AlreadyClaimed || counted.loads.Load() != 2 {
		t.Fatal("new day cache was not rebuilt", again, err, counted.loads.Load())
	}
	var grants, expirations int
	if err = conn.QueryRow(ctx, `SELECT count(*) FILTER(WHERE kind='add'), count(*) FILTER(WHERE kind='expire') FROM drama_catalog_v1.credit_ledger WHERE account_id=$1`, actor.UserID).Scan(&grants, &expirations); err != nil || grants != 2 || expirations != 1 {
		t.Fatal("cross-day ledger facts", grants, expirations, err)
	}
	wallet, err := clockStore.CreditWallet(ctx, actor.UserID)
	if err != nil || wallet.Balance != 11 {
		t.Fatal("yesterday's gift remained spendable", wallet, err)
	}
	t.Logf("PG fixture 2040-12-31 -> 2041-01-01, DB session UTC+14; grants=%d expiration=%d wallet=%d, Redis TTL=%s <= PG budget=%s", grants, expirations, wallet.Balance, ttl, first.CacheTTL)
}

func TestDailyGiftCacheDoesNotCacheErrorsOrExtendLateFills(t *testing.T) {
	runtime := fixture(t)
	ctx := context.Background()
	user := identity.New("failed_gift")
	failed := errors.New("PG commit unknown fixture")
	for range 2 {
		if _, err := runtime.ReadDailyGift(ctx, user, func(context.Context) (c.DailyGift, error) { return c.DailyGift{}, failed }); !errors.Is(err, failed) {
			t.Fatal("PG error was hidden", err)
		}
	}
	if n, err := runtime.client.Exists(ctx, runtime.key("daily-gift", user)).Result(); err != nil || n != 0 {
		t.Fatal("failed PG result was cached", n, err)
	}
	late := identity.New("late_gift")
	out, err := runtime.ReadDailyGift(ctx, late, func(context.Context) (c.DailyGift, error) {
		time.Sleep(60 * time.Millisecond)
		return c.DailyGift{Date: "2040-12-31", Amount: 10, Claimed: true, CacheTTL: 20 * time.Millisecond}, nil
	})
	if err != nil || out.Amount != 10 {
		t.Fatal("committed late reply was lost", out, err)
	}
	if n, err := runtime.client.Exists(ctx, runtime.key("daily-gift", late)).Result(); err != nil || n != 0 {
		t.Fatal("late fill extended yesterday's lifetime", n, err)
	}
}

func TestDailyGiftCacheFailureAfterPGCommitRecoversWithoutAnotherGrant(t *testing.T) {
	runtime := fixture(t)
	other := second(t, runtime)
	store, conn, _ := giftDatabase(t, other)
	ctx := context.Background()
	policy, err := store.CreditGiftPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveCreditGiftPolicy(ctx, c.GiftPolicyInput{Revision: policy.Revision, DailyGift: 17}); err != nil {
		t.Fatal(err)
	}
	actor := giftUser(t, conn)
	_, err = runtime.ReadDailyGift(ctx, actor.UserID, func(ctx context.Context) (c.DailyGift, error) {
		out, err := store.ClaimDailyCreditGift(ctx, actor.UserID)
		if err == nil {
			// Simulate losing Redis after the real PG transaction has committed.
			_ = runtime.client.Close()
		}
		return out, err
	})
	if err == nil {
		t.Fatal("Redis failure after commit was hidden")
	}
	if _, err = runtime.ReadDailyGift(ctx, "uncached-user", func(context.Context) (c.DailyGift, error) {
		t.Error("Redis failure fell through to PG")
		return c.DailyGift{}, nil
	}); code(err) != "RUNTIME_UNAVAILABLE" {
		t.Fatal("unavailable cache did not fail explicitly", err)
	}
	out, err := other.ReadDailyGift(ctx, actor.UserID, func(ctx context.Context) (c.DailyGift, error) {
		return store.ClaimDailyCreditGift(ctx, actor.UserID)
	})
	if err != nil || !out.AlreadyClaimed || out.Amount != 17 {
		t.Fatal("committed grant was not recovered", out, err)
	}
	var count int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND source='daily-gift'`, actor.UserID).Scan(&count); err != nil || count != 1 {
		t.Fatal("retry after cache failure duplicated funding", count, err)
	}
}
