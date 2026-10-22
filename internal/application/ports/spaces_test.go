package ports_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
)

type accessStub struct {
	permission spaces.Access
	scope      spaces.Scope
	err        error
	actor      ports.Actor
	resource   spaces.Resource
}

func (s *accessStub) Access(_ context.Context, a ports.Actor, r spaces.Resource) (spaces.Access, spaces.Scope, error) {
	s.actor, s.resource = a, r
	return s.permission, s.scope, s.err
}

func errorCode(err error) string {
	if errors.Is(err, fault.ErrNotFound) {
		return "NOT_FOUND"
	}
	var f *fault.Error
	if errors.As(err, &f) {
		return f.Code
	}
	return ""
}

func TestRequireSpaceAccessUsesResourceSpaceAndGuardsWrites(t *testing.T) {
	resource := spaces.Resource{SpaceID: "resource-space", OwnerID: "other-user", Kind: spaces.ProjectResource, ID: "project"}
	for _, test := range []struct {
		name       string
		requested  string
		permission spaces.Access
		required   spaces.Access
		readOnly   bool
		want       string
	}{
		{"cross-space-read", "selected-space", spaces.Read, spaces.Read, false, ""},
		{"cross-space-write", "selected-space", spaces.Edit, spaces.Edit, false, "SPACE_CHANGED"},
		{"default-personal-blocks-team-write", "", spaces.Edit, spaces.Edit, false, "SPACE_CHANGED"},
		{"same-space-write", "resource-space", spaces.Edit, spaces.Edit, false, ""},
		{"missing-permission-does-not-leak-space", "selected-space", spaces.None, spaces.Edit, false, "NOT_FOUND"},
		{"read-all-cannot-edit", "resource-space", spaces.Read, spaces.Edit, false, "ROLE_FORBIDDEN"},
		{"expired-empty-cannot-edit", "resource-space", spaces.Read, spaces.Edit, true, "TEAM_READ_ONLY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &accessStub{permission: test.permission, scope: spaces.Scope{SpaceID: resource.SpaceID, UserID: "user", ReadOnly: test.readOnly}}
			scope, err := ports.RequireSpaceAccess(context.Background(), stub, spaces.Scope{SpaceID: test.requested, UserID: "user"}, resource, test.required)
			if got := errorCode(err); got != test.want {
				t.Fatalf("error = %v, want code %s", err, test.want)
			}
			if err == nil && scope.SpaceID != resource.SpaceID {
				t.Fatal("returned selector rather than resource scope", scope)
			}
			if stub.resource != resource || stub.actor.UserID != "user" {
				t.Fatal("access did not receive resource facts and authenticated user", stub)
			}
		})
	}
}

func TestRequestedSpaceIsSelectionOnly(t *testing.T) {
	ctx := context.Background()
	if ports.RequestedSpace(ctx) != "" {
		t.Fatal("request has an implicit selection")
	}
	selected := ports.WithRequestedSpace(ctx, "team")
	if ports.RequestedSpace(selected) != "team" || ports.RequestedSpace(ctx) != "" {
		t.Fatal("selection escaped its context")
	}
	if _, err := ports.ResolveSpace(selected, nil, spaces.Scope{SpaceID: "team", UserID: "user"}); err == nil {
		t.Fatal("unresolved selection granted a trusted scope")
	}
	if _, err := ports.RequireSpaceAccess(selected, nil, spaces.Scope{UserID: "user"}, spaces.Resource{}, spaces.Read); err == nil {
		t.Fatal("missing access service granted resource permission")
	}
}

func TestResolveSpaceOnceOnlyReusesExplicitRequestScope(t *testing.T) {
	actor := ports.Actor{UserID: "user", Role: ports.RoleUser}
	var loads atomic.Int32
	load := func() (ports.SpaceScope, error) {
		loads.Add(1)
		return ports.SpaceScope{SpaceID: "space", UserID: actor.UserID}, nil
	}
	background := context.Background()
	for range 2 {
		if _, err := ports.ResolveSpaceOnce(background, actor, "space", load); err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() != 2 {
		t.Fatal("background or Agent reused permissions", loads.Load())
	}
	request := ports.WithRequestedSpace(background, "space")
	var concurrent sync.WaitGroup
	for range 20 {
		concurrent.Add(1)
		go func() {
			defer concurrent.Done()
			scope, err := ports.ResolveSpaceOnce(request, actor, "space", load)
			if err != nil || scope.SpaceID != "space" {
				t.Error(scope, err)
			}
		}()
	}
	concurrent.Wait()
	if loads.Load() != 3 {
		t.Fatal("concurrent request reads did not share one resolution", loads.Load())
	}
	for _, call := range []struct {
		ctx   context.Context
		actor ports.Actor
		space string
	}{
		{ports.WithRequestedSpace(background, "space"), actor, "space"},
		{ports.WithRequestedSpace(request, "space"), actor, "space"},
		{request, actor, "another-space"},
		{request, ports.Actor{UserID: "another-user", Role: ports.RoleUser}, "space"},
	} {
		if _, err := ports.ResolveSpaceOnce(call.ctx, call.actor, call.space, load); err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() != 7 {
		t.Fatal("resolution escaped its request, user or resource space", loads.Load())
	}
}
