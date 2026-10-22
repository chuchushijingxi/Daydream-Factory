package spaces

import "testing"

func TestSharingManagementDoesNotFollowEditingGrants(t *testing.T) {
	r := Resource{SpaceID: "team", OwnerID: "responsible", Kind: ProjectResource, ID: "project"}
	for _, role := range []Role{Owner, Admin, Member} {
		for _, user := range []string{"responsible", "other"} {
			for _, status := range []Status{Active, Expired, Disabled} {
				for _, readOnly := range []bool{false, true} {
					scope := Scope{SpaceID: "team", UserID: user, Kind: TeamKind, Role: role, Status: status, Policy: EditAll, ReadOnly: readOnly}
					want := status != Disabled && (role == Owner || role == Admin || user == r.OwnerID)
					if got := CanManageSharing(scope, r); got != want {
						t.Fatalf("scope=%+v got %v want %v", scope, got, want)
					}
				}
			}
		}
	}
	for _, kind := range []string{UploadResource, MediaResource, AgentSessionResource, "unknown"} {
		copy := r
		copy.Kind = kind
		if CanManageSharing(Scope{SpaceID: "team", UserID: "responsible", Kind: TeamKind, Role: Owner}, copy) {
			t.Fatal(kind)
		}
	}
	if CanManageSharing(Scope{SpaceID: "other", UserID: "responsible", Kind: TeamKind, Role: Owner}, r) {
		t.Fatal("cross-space management")
	}
	if CanManageSharing(Scope{SpaceID: "team", UserID: "responsible", Kind: Personal, Role: Owner}, r) {
		t.Fatal("personal resource sharing")
	}
}
