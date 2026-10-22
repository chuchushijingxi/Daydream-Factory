package ports

import (
	"context"
	"testing"
)

func TestWithoutSpaceRequestDropsOnlyAuthorizationReuse(t *testing.T) {
	type otherKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), otherKey{}, "keep"))
	defer cancel()
	request := WithRequestedSpace(ctx, "team")
	calls := 0
	load := func() (SpaceScope, error) { calls++; return SpaceScope{SpaceID: "team"}, nil }
	a := Actor{UserID: "user", Role: RoleUser}
	for i := 0; i < 2; i++ {
		if _, err := ResolveSpaceOnce(request, a, "team", load); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	fresh := WithoutSpaceRequest(context.WithoutCancel(request))
	if RequestedSpace(fresh) != "" || fresh.Value(otherKey{}) != "keep" {
		t.Fatal("unrelated context lost")
	}
	for i := 0; i < 2; i++ {
		if _, err := ResolveSpaceOnce(fresh, a, "team", load); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 {
		t.Fatal("Agent inherited member cache", calls)
	}
	cancelled := WithoutSpaceRequest(request)
	cancel()
	if cancelled.Err() != context.Canceled {
		t.Fatal("cancel propagation changed", cancelled.Err())
	}
}
