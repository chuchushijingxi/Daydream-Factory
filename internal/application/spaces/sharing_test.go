package spaces

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/spaces"
)

func (tx *memoryTx) SharingResource(_ context.Context, kind, id string) (d.Resource, error) {
	r, ok := tx.state.sharingResources[kind+":"+id]
	if !ok || r.SpaceID != tx.state.team.ID {
		return d.Resource{}, fault.ErrNotFound
	}
	return r, nil
}
func (tx *memoryTx) ResourceShares(_ context.Context, kind, id string) ([]d.Share, error) {
	var out []d.Share
	for key, v := range tx.state.shares {
		if strings.HasPrefix(key, kind+":"+id+":") {
			out = append(out, v)
		}
	}
	return out, nil
}
func (tx *memoryTx) SaveResourceShare(_ context.Context, kind, id string, share d.Share, _ string) error {
	tx.state.shares[kind+":"+id+":"+share.Grantee] = share
	return nil
}
func (tx *memoryTx) DeleteResourceShare(_ context.Context, kind, id, grantee string) error {
	delete(tx.state.shares, kind+":"+id+":"+grantee)
	return nil
}
func sharingFixture() (context.Context, *Service, *memoryStore) {
	ctx, service, store := fixture()
	store.state.team.Policy = d.Isolated
	store.state.sharingResources = map[string]d.Resource{"project:project-one": {SpaceID: "team_test", OwnerID: "member", Kind: d.ProjectResource, ID: "project-one"}}
	store.state.shares = map[string]d.Share{}
	return ctx, service, store
}
func TestSharingRequiresResponsibleMemberOrCurrentManager(t *testing.T) {
	ctx, s, store := sharingFixture()
	// Resource management follows its path team, independently of the selector.
	ctx = ports.WithRequestedSpace(ctx, "different-space")
	for _, user := range []string{"member", "admin", "owner"} {
		if _, err := s.PutShare(ctx, actor(user), "team_test", "project", "project-one", ShareInput{Grantee: "admin", Permission: d.Read}); err != nil {
			t.Fatal(user, err)
		}
	}
	store.state.sharingResources["project:project-one"] = d.Resource{SpaceID: "team_test", OwnerID: "owner", Kind: d.ProjectResource, ID: "project-one"}
	store.state.team.Policy = d.EditAll
	store.state.shares["project:project-one:member"] = d.Share{Grantee: "member", Permission: d.Edit}
	before := store.state.clone()
	// Both edit_all and an explicit editing grant permit edits, but never re-sharing.
	if _, err := s.PutShare(ctx, actor("member"), "team_test", "project", "project-one", ShareInput{Grantee: "admin", Permission: d.Edit}); !hasCode(err, "ROLE_FORBIDDEN") {
		t.Fatal(err)
	}
	if _, err := s.Sharing(ctx, actor("member"), "team_test", "project", "project-one"); !hasCode(err, "ROLE_FORBIDDEN") {
		t.Fatal("grantee saw private sharing configuration", err)
	}
	if _, err := s.PutShare(ports.WithRequestedSpace(ctx, "team_test"), actor("new-a"), "team_test", "project", "project-one", ShareInput{Grantee: "member", Permission: d.Edit}); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("selector granted membership", err)
	}
	if !reflect.DeepEqual(before, store.state.clone()) {
		t.Fatal("denied management changed facts")
	}
}
func TestSharingValidatesResourceAndCurrentGranteeInsideTransaction(t *testing.T) {
	ctx, s, store := sharingFixture()
	for _, tc := range []struct {
		kind, id, grantee string
		permission        d.Access
	}{
		{"upload", "project-one", "admin", d.Read}, {"canvas", "canvas-missing", "admin", d.Read}, {"project", "missing", "admin", d.Read}, {"project", "project-one", "new-a", d.Read}, {"project", "project-one", "admin", d.None}, {"project", "project-one", "admin", d.Access(9)},
	} {
		before := store.state.clone()
		if _, err := s.PutShare(ctx, actor("member"), "team_test", tc.kind, tc.id, ShareInput{Grantee: tc.grantee, Permission: tc.permission}); err == nil {
			t.Fatal("accepted invalid share", tc)
		}
		if !reflect.DeepEqual(before, store.state.clone()) {
			t.Fatal("invalid share partially changed facts", tc)
		}
	}
	store.state.team.Disabled = true
	if _, err := s.PutShare(ctx, actor("owner"), "team_test", "project", "project-one", ShareInput{Grantee: "*", Permission: d.Edit}); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestSharingAndRemovalAuditNotificationsAreAtomicAndDoNotTightenPolicy(t *testing.T) {
	ctx, s, store := sharingFixture()
	input := ShareInput{Grantee: "*", Permission: d.Read}
	before := store.state.clone()
	store.failNotify = true
	if _, err := s.PutShare(ctx, actor("member"), "team_test", "project", "project-one", input); err == nil || !reflect.DeepEqual(before, store.state.clone()) {
		t.Fatal("share notification failure committed", err)
	}
	store.failNotify = false
	out, err := s.PutShare(ctx, actor("member"), "team_test", "project", "project-one", input)
	if err != nil || len(out.Shares) != 1 || len(store.state.notifications) != 3 || len(store.state.audit) != 1 {
		t.Fatal(out, store.state, err)
	}
	if store.state.notifications[0].RecipientUserID != "admin" || store.state.notifications[1].RecipientUserID != "member" || store.state.notifications[2].RecipientUserID != "owner" {
		t.Fatal("broadcast lock ordering is unstable")
	}
	// An identical PUT is idempotent and does not duplicate audit or inbox messages.
	if _, err = s.PutShare(ctx, actor("member"), "team_test", "project", "project-one", input); err != nil || len(store.state.notifications) != 3 || len(store.state.audit) != 1 {
		t.Fatal(err)
	}
	before = store.state.clone()
	store.failAudit = true
	if err = s.DeleteShare(ctx, actor("owner"), "team_test", "project", "project-one", "*"); err == nil || !reflect.DeepEqual(before, store.state.clone()) {
		t.Fatal("removal audit failure committed", err)
	}
	store.failAudit = false
	if err = s.DeleteShare(ctx, actor("member"), "team_test", "project", "project-one", "*"); err != nil {
		t.Fatal(err)
	}
	if len(store.state.shares) != 0 || len(store.state.notifications) != 6 || len(store.state.audit) != 2 {
		t.Fatal(store.state)
	}
	// Removing a grant preserves the independent team default; sharing only widens it.
	resource := store.state.sharingResources["project:project-one"]
	scope := d.Scope{SpaceID: "team_test", UserID: "admin-reader", Kind: d.TeamKind, Role: d.Member, Status: d.Active, Policy: d.ReadAll}
	if d.Decide(scope, resource, nil) != d.Read {
		t.Fatal("removed share tightened base policy")
	}
	// Re-share is a new fact, so the new notice has a fresh operation identity.
	if _, err = s.PutShare(ctx, actor("member"), "team_test", "project", "project-one", input); err != nil || store.state.notifications[0].OperationID == store.state.notifications[6].OperationID {
		t.Fatal(err)
	}
}

func TestSharingConfigurationCannotProbeInvisibleResource(t *testing.T) {
	ctx, s, store := sharingFixture()
	resource := store.state.sharingResources["project:project-one"]
	resource.OwnerID = "owner"
	store.state.sharingResources["project:project-one"] = resource
	before := store.state.clone()
	if _, err := s.Sharing(ctx, actor("member"), "team_test", "project", "project-one"); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("invisible resource existence leaked", err)
	}
	if _, err := s.PutShare(ctx, actor("member"), "team_test", "project", "project-one", ShareInput{Grantee: "admin", Permission: d.Read}); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("invisible resource existence leaked on write", err)
	}
	if !reflect.DeepEqual(before, store.state.clone()) {
		t.Fatal("denied request mutated sharing")
	}
	store.state.shares["project:project-one:member"] = d.Share{Grantee: "member", Permission: d.Read}
	if _, err := s.Sharing(ctx, actor("member"), "team_test", "project", "project-one"); !hasCode(err, "ROLE_FORBIDDEN") {
		t.Fatal("readable member saw sharing configuration", err)
	}
}
