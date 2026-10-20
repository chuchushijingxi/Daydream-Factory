package accounts

import (
	"context"
	"strings"
	"testing"

	"drama-agent/internal/application/ports"
)

func TestProfileNamesUseUnicodeAndEmptyActorNeverReachesStore(t *testing.T) {
	for _, value := range []string{"小剧 🌿", strings.Repeat("界", 40)} {
		got, err := Username("  " + value + "  ")
		if err != nil || got != value {
			t.Fatalf("valid Unicode display name rejected: %v", err)
		}
	}
	for _, value := range []string{"", " \n ", strings.Repeat("界", 41), "hello\nworld", string([]byte{0xff})} {
		if _, err := Username(value); err == nil {
			t.Fatal("invalid display name accepted")
		}
	}
	// Nil dependencies deliberately ensure empty identity is rejected before any persistence.
	s := &Service{}
	ctx := context.Background()
	actor := ports.Actor{}
	if _, err := s.Profile(ctx, actor); err == nil {
		t.Fatal("empty profile actor")
	}
	if _, err := s.UpdateProfile(ctx, actor, "valid"); err == nil {
		t.Fatal("empty update actor")
	}
	if _, err := s.SetAvatar(ctx, actor, []byte("x")); err == nil {
		t.Fatal("empty upload actor")
	}
	if _, err := s.RemoveAvatar(ctx, actor); err == nil {
		t.Fatal("empty removal actor")
	}
	if _, err := s.Avatar(ctx, actor); err == nil {
		t.Fatal("empty read actor")
	}
	if err := s.ChangePassword(ctx, actor, "", "old", "new"); err == nil {
		t.Fatal("empty password actor")
	}
}
