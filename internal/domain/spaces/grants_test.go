package spaces_test

import (
	d "drama-agent/internal/domain/spaces"
	"testing"
	"time"
)

func TestGrantCadenceAndExpiredRenewal(t *testing.T) {
	opened := time.Date(2026, 1, 31, 3, 0, 0, 0, time.UTC)
	initial := d.InitialGrant(opened)
	if initial.Sequence != 1 || !initial.NextAt.Equal(opened.Add(30*24*time.Hour)) {
		t.Fatal(initial)
	}
	next, err := initial.AfterGrant()
	if err != nil || next.Sequence != 2 || !next.NextAt.Equal(opened.Add(60*24*time.Hour)) {
		t.Fatal(next, err)
	}
	renewedAt := opened.Add(100 * 24 * time.Hour)
	renewed, err := next.AfterRenewal(renewedAt)
	if err != nil || renewed.Sequence != 3 || !renewed.NextAt.Equal(renewedAt.Add(30*24*time.Hour)) {
		t.Fatal(renewed, err)
	}
}
func TestEnableSkipsDisabledPeriodsAlongOriginalCadence(t *testing.T) {
	anchor := time.Date(2026, 1, 1, 3, 0, 0, 123000, time.UTC)
	schedule := d.InitialGrant(anchor)
	for _, test := range []struct {
		name     string
		at       time.Time
		sequence uint64
		next     time.Time
	}{
		{"before-due", anchor.Add(15 * 24 * time.Hour), 1, anchor.Add(d.GrantInterval)},
		{"at-due", anchor.Add(d.GrantInterval), 2, anchor.Add(2 * d.GrantInterval)},
		{"between-cycles", anchor.Add(76 * 24 * time.Hour), 3, anchor.Add(3 * d.GrantInterval)},
		{"fraction-before-boundary", anchor.Add(2 * d.GrantInterval).Add(-time.Nanosecond), 2, anchor.Add(2 * d.GrantInterval)},
	} {
		t.Run(test.name, func(t *testing.T) {
			next, err := schedule.AfterEnable(test.at)
			if err != nil || next.Sequence != test.sequence || !next.NextAt.Equal(test.next) || !next.NextAt.After(test.at) {
				t.Fatal(next, err)
			}
		})
	}
}

func TestGrantSequenceRejectsMissingOrUnsafePersistentCounter(t *testing.T) {
	at := time.Now().UTC()
	for _, sequence := range []uint64{0, 9007199254740992} {
		schedule := d.GrantSchedule{NextAt: at, Sequence: sequence}
		if _, err := schedule.AfterGrant(); err == nil {
			t.Fatal("invalid sequence advanced", sequence)
		}
		if _, err := schedule.AfterRenewal(at); err == nil {
			t.Fatal("invalid sequence renewed", sequence)
		}
		if _, err := schedule.AfterEnable(at); err == nil {
			t.Fatal("invalid sequence resumed", sequence)
		}
	}
}
