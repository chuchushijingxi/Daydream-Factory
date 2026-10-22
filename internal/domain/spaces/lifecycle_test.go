package spaces_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/spaces"
)

func TestCapacityIncludesOwnerAndDoesNotRemoveExistingMembers(t *testing.T) {
	for _, test := range []struct {
		limit, count int
		allowed      bool
	}{{1, 1, false}, {3, 2, true}, {3, 3, false}, {2, 4, false}, {1, 0, true}} {
		err := (d.Team{MemberLimit: test.limit}).RequireCapacity(test.count)
		if (err == nil) != test.allowed {
			t.Errorf("limit %d count %d: %v", test.limit, test.count, err)
		}
	}
}
func TestTeamNameAndPolicy(t *testing.T) {
	for _, name := range []string{"", " \t ", strings.Repeat("字", 41), "团\n队", string([]byte{0xff})} {
		if _, err := d.NormalizeName(name); err == nil {
			t.Fatal("invalid team name accepted", name)
		}
	}
	if name, err := d.NormalizeName(" 团队 "); err != nil || name != "团队" {
		t.Fatal(name, err)
	}
	for _, p := range []d.Policy{d.Isolated, d.ReadAll, d.EditAll} {
		if !p.Valid() {
			t.Fatal(p)
		}
	}
	if d.Policy("unknown").Valid() {
		t.Fatal("unknown policy accepted")
	}
}
func TestInviteAndRequestTransitionsRespectBoundary(t *testing.T) {
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	future := at.Add(time.Second)
	for _, test := range []struct {
		state   string
		expires time.Time
		revoked *time.Time
		valid   bool
	}{{"pending", future, nil, true}, {"pending", at, nil, false}, {"pending", future, &at, false}, {"accepted", future, nil, false}, {"declined", future, nil, false}, {"expired", future, nil, false}} {
		if err := d.RequireInvite(test.state, test.expires, test.revoked, at); (err == nil) != test.valid {
			t.Error(test, err)
		}
	}
	if err := d.RequirePendingRequest("pending"); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"approved", "rejected", ""} {
		if d.RequirePendingRequest(status) == nil {
			t.Fatal(status)
		}
	}
	if err := d.RequireOwnershipAcceptance("pending", "old", "old", "target", "target"); err != nil {
		t.Fatal(err)
	}
	if err := d.RequireOwnershipAcceptance("pending", "old", "new", "target", "target"); err == nil {
		t.Fatal("old transfer overwrote a newer owner")
	}
	if err := d.RequireOwnershipAcceptance("pending", "old", "old", "target", "other"); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestReminderDatesUseShanghaiAndRenewalPeriod(t *testing.T) {
	expiry := time.Date(2026, 10, 28, 0, 30, 0, 0, time.FixedZone("CST", 8*3600))
	team := d.Team{ExpiresAt: expiry}
	for _, test := range []struct {
		at   time.Time
		want string
	}{{expiry.AddDate(0, 0, -7), "expires_7d"}, {expiry.AddDate(0, 0, -6), ""}, {expiry.AddDate(0, 0, -1), "expires_1d"}, {expiry.Add(time.Hour), "expires_today"}, {expiry.AddDate(0, 0, 1), ""}} {
		got := team.ReminderKinds(test.at.UTC())
		if test.want == "" {
			if len(got) != 0 {
				t.Fatal(test, got)
			}
		} else if len(got) != 1 || got[0] != test.want {
			t.Fatal(test, got)
		}
	}
	team.Disabled = true
	if len(team.ReminderKinds(expiry)) != 0 {
		t.Fatal("disabled team sent a reminder")
	}
}
