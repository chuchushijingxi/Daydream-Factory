package spaces_test

import (
	"testing"
	"time"

	"drama-agent/internal/domain/spaces"
	"drama-agent/internal/domain/spaces/spacestest"
)

func TestDecideAccessMatrix(t *testing.T) {
	for _, test := range spacestest.AccessCases() {
		t.Run(test.Name, func(t *testing.T) {
			if got := spaces.Decide(test.Scope, test.Resource, test.Shares); got != test.Want {
				t.Fatalf("access = %v, want %v", got, test.Want)
			}
			otherSpace := test.Resource
			otherSpace.SpaceID = "other-space"
			if got := spaces.Decide(test.Scope, otherSpace, test.Shares); got != spaces.None {
				t.Fatal("cross-space resource became visible", got)
			}
		})
	}
}

func TestDecideRejectsMissingIdentityAndUntrustedSharePermission(t *testing.T) {
	scope := spaces.Scope{SpaceID: "space", Kind: spaces.TeamKind, UserID: "user", Role: spaces.Member, Policy: spaces.Isolated, Status: spaces.Active}
	resource := spaces.Resource{SpaceID: "space", OwnerID: "other", Kind: spaces.ProjectResource}
	for _, invalid := range []spaces.Access{-1, 3, 99} {
		if got := spaces.Decide(scope, resource, []spaces.Share{{Grantee: "user", Permission: invalid}}); got != spaces.None {
			t.Fatal("invalid share granted access", invalid, got)
		}
	}
	for _, mutate := range []func(*spaces.Scope){
		func(s *spaces.Scope) { s.SpaceID = "" },
		func(s *spaces.Scope) { s.UserID = "" },
		func(s *spaces.Scope) { s.Role = "operator" },
		func(s *spaces.Scope) { s.Kind = "unknown" },
	} {
		invalid := scope
		mutate(&invalid)
		if got := spaces.Decide(invalid, resource, []spaces.Share{{Grantee: "*", Permission: spaces.Edit}}); got != spaces.None {
			t.Fatal("invalid scope granted access", invalid, got)
		}
	}
	resource.OwnerID = ""
	if got := spaces.Decide(scope, resource, []spaces.Share{{Grantee: "*", Permission: spaces.Edit}}); got != spaces.None {
		t.Fatal("missing owner granted access", got)
	}
}

func TestDecideCombinesMatchingSharesWithoutRestrictingPolicy(t *testing.T) {
	scope := spaces.Scope{SpaceID: "space", Kind: spaces.TeamKind, UserID: "user", Role: spaces.Member, Policy: spaces.Isolated, Status: spaces.Active}
	resource := spaces.Resource{SpaceID: "space", OwnerID: "other", Kind: spaces.ProjectResource}
	shares := []spaces.Share{{Grantee: "another-user", Permission: spaces.Edit}, {Grantee: "*", Permission: spaces.Read}, {Grantee: "user", Permission: spaces.Edit}}
	if got := spaces.Decide(scope, resource, shares); got != spaces.Edit {
		t.Fatal("matching shares did not preserve highest grant", got)
	}
	scope.Policy = spaces.EditAll
	if got := spaces.Decide(scope, resource, []spaces.Share{{Grantee: "user", Permission: spaces.Read}}); got != spaces.Edit {
		t.Fatal("share restricted team-wide editing", got)
	}
}

func TestTeamStatusAndReadOnlyUseExpiryAndTotalBalance(t *testing.T) {
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		team     spaces.Team
		balance  int64
		status   spaces.Status
		readOnly bool
	}{
		{"active-empty", spaces.Team{ExpiresAt: at.Add(time.Second)}, 0, spaces.Active, false},
		{"expiry-included", spaces.Team{ExpiresAt: at}, 0, spaces.Expired, true},
		{"expired-with-allocation", spaces.Team{ExpiresAt: at.Add(-time.Second)}, 1, spaces.Expired, false},
		{"disabled-priority", spaces.Team{ExpiresAt: at, Disabled: true}, 0, spaces.Disabled, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.team.Status(at); got != test.status {
				t.Fatalf("status = %s, want %s", got, test.status)
			}
			if got := test.team.ReadOnly(at, test.balance); got != test.readOnly {
				t.Fatalf("readOnly = %t, want %t", got, test.readOnly)
			}
		})
	}
}
