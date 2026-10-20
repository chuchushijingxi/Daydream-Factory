package credits

import (
	"context"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
)

type giftFixture struct{ reads, writes, claims int }

func (f *giftFixture) CreditGiftPolicy(context.Context) (GiftPolicy, error) {
	f.reads++
	return GiftPolicy{}, nil
}
func (f *giftFixture) SaveCreditGiftPolicy(context.Context, GiftPolicyInput) (GiftPolicy, error) {
	f.writes++
	return GiftPolicy{}, nil
}
func (f *giftFixture) ClaimDailyCreditGift(context.Context, string) (DailyGift, error) {
	f.claims++
	return DailyGift{}, nil
}
func (f *giftFixture) ReadDailyGift(ctx context.Context, _ string, load func(context.Context) (DailyGift, error)) (DailyGift, error) {
	return load(ctx)
}

func TestGiftsShareActorAndRejectBeforeStorage(t *testing.T) {
	f := &giftFixture{}
	s := Gifts{Store: f, Cache: f}
	ctx := context.Background()
	user := ports.Actor{UserID: "a", Role: ports.RoleUser}
	for _, actor := range []ports.Actor{{}, user} {
		if _, err := s.Policy(ctx, actor); err == nil {
			t.Fatal("policy role boundary")
		}
		if _, err := s.Save(ctx, actor, GiftPolicyInput{}); err == nil {
			t.Fatal("policy write role boundary")
		}
	}
	if _, err := s.Daily(ctx, ports.Actor{}); err == nil {
		t.Fatal("empty actor allowed")
	}
	if f.reads+f.writes+f.claims != 0 {
		t.Fatal("forbidden access reached storage")
	}
	op := ports.Actor{UserID: "op", Role: ports.RoleOperator}
	for _, in := range []GiftPolicyInput{{RegistrationGift: -1}, {DailyGift: -1}, {Revision: 9007199254740991}, {DailyGift: 9007199254740992}} {
		if _, err := s.Save(ctx, op, in); err == nil {
			t.Fatal("invalid policy accepted", in)
		}
	}
	if f.writes != 0 {
		t.Fatal("invalid policy persisted")
	}
	if _, err := s.Daily(ctx, user); err != nil || f.claims != 1 {
		t.Fatal(err, f.claims)
	}
	s.Cache = nil
	if _, err := s.Daily(ctx, user); err == nil || f.claims != 1 {
		t.Fatal("missing Redis runtime fell through to PostgreSQL", err, f.claims)
	}
}

func TestGiftDayUsesBeijingBoundaryRegardlessOfInputTimezone(t *testing.T) {
	for _, row := range []struct{ at, want string }{
		{"2026-10-17T15:59:59.999999999Z", "2026-10-17"},
		{"2026-10-17T16:00:00Z", "2026-10-18"},
		{"2026-12-31T16:00:00Z", "2027-01-01"},
		{"2028-02-28T16:00:00Z", "2028-02-29"},
		{"2028-02-29T16:00:00Z", "2028-03-01"},
	} {
		at, err := time.Parse(time.RFC3339Nano, row.at)
		if err != nil || GiftDate(at) != row.want {
			t.Fatal(row, err, GiftDate(at))
		}
		for _, offset := range []int{-12, -8, 0, 8, 14} {
			if got := GiftDate(at.In(time.FixedZone("deployment", offset*3600))); got != row.want {
				t.Fatalf("same instant, timezone UTC%+d: got %s, want %s", offset, got, row.want)
			}
		}
	}
}
