package spaces

import (
	"context"
	"sort"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	n "drama-agent/internal/domain/notifications"
	d "drama-agent/internal/domain/spaces"
)

// Sharing configuration is private to the responsible member and team managers.
// All reads use the same locked resource facts as mutations, never caller roles.
func (s *Service) withSharing(ctx context.Context, a ports.Actor, spaceID, kind, resourceID string, fn func(Tx, d.Team, d.Resource) error) error {
	if !d.SharingKind(kind) || !validID(resourceID) {
		return fault.ErrNotFound
	}
	return s.withTeam(ctx, a, spaceID, d.ViewMembers, func(tx Tx, team d.Team, member Member) error {
		resource, err := tx.SharingResource(ctx, kind, resourceID)
		if err != nil {
			return err
		}
		if resource.SpaceID != spaceID || resource.Kind != kind || resource.ID != resourceID {
			return fault.ErrNotFound
		}
		scope := d.Scope{SpaceID: spaceID, UserID: a.UserID, Kind: d.TeamKind, Role: member.Role, Status: team.Status(s.now()), Policy: team.Policy}
		if !d.CanManageSharing(scope, resource) {
			// Sharing configuration cannot be used as an existence probe for content
			// hidden by the team's policy. A readable grantee still has no sharing right.
			shares, e := tx.ResourceShares(ctx, kind, resourceID)
			if e != nil {
				return e
			}
			if d.Decide(scope, resource, shares) == d.None {
				return fault.ErrNotFound
			}
			return forbidden()
		}
		return fn(tx, team, resource)
	})
}
func sharingView(ctx context.Context, tx Tx, resource d.Resource) (SharingView, error) {
	shares, err := tx.ResourceShares(ctx, resource.Kind, resource.ID)
	if err != nil {
		return SharingView{}, err
	}
	if shares == nil {
		shares = []d.Share{}
	}
	sort.Slice(shares, func(i, j int) bool { return shares[i].Grantee < shares[j].Grantee })
	return SharingView{SpaceID: resource.SpaceID, ResourceKind: resource.Kind, ResourceID: resource.ID, Shares: shares, CanManage: true}, nil
}
func (s *Service) Sharing(ctx context.Context, a ports.Actor, spaceID, kind, resourceID string) (SharingView, error) {
	var out SharingView
	err := s.withSharing(ctx, a, spaceID, kind, resourceID, func(tx Tx, _ d.Team, r d.Resource) error {
		var e error
		out, e = sharingView(ctx, tx, r)
		return e
	})
	return out, err
}
func validGrantee(id string) bool { return id == "*" || validID(id) }
func (s *Service) PutShare(ctx context.Context, a ports.Actor, spaceID, kind, resourceID string, in ShareInput) (SharingView, error) {
	share := d.Share{Grantee: in.Grantee, Permission: in.Permission}
	if !validGrantee(in.Grantee) || !share.Valid() {
		return SharingView{}, invalid()
	}
	var out SharingView
	err := s.withSharing(ctx, a, spaceID, kind, resourceID, func(tx Tx, team d.Team, r d.Resource) error {
		if in.Grantee != "*" {
			if _, err := tx.Member(ctx, in.Grantee); err != nil {
				return err
			}
		}
		current, err := tx.ResourceShares(ctx, kind, resourceID)
		if err != nil {
			return err
		}
		for _, previous := range current {
			if previous == share {
				out, err = sharingView(ctx, tx, r)
				return err
			}
		}
		if err = tx.SaveResourceShare(ctx, kind, resourceID, share, a.UserID); err != nil {
			return err
		}
		eventID := identity.New("share")
		if err = audit(ctx, tx, a, spaceID, "resource_shared", resourceID, map[string]any{"kind": kind, "grantee": in.Grantee, "permission": in.Permission, "eventId": eventID}); err != nil {
			return err
		}
		if err = notifySharedMembers(ctx, tx, team, r, in.Grantee, eventID, false); err != nil {
			return err
		}
		out, err = sharingView(ctx, tx, r)
		return err
	})
	return out, err
}
func (s *Service) DeleteShare(ctx context.Context, a ports.Actor, spaceID, kind, resourceID, grantee string) error {
	if !validGrantee(grantee) {
		return invalid()
	}
	return s.withSharing(ctx, a, spaceID, kind, resourceID, func(tx Tx, team d.Team, r d.Resource) error {
		shares, err := tx.ResourceShares(ctx, kind, resourceID)
		if err != nil {
			return err
		}
		found := false
		for _, share := range shares {
			if share.Grantee == grantee {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
		if err = tx.DeleteResourceShare(ctx, kind, resourceID, grantee); err != nil {
			return err
		}
		eventID := identity.New("share")
		if err = audit(ctx, tx, a, spaceID, "resource_share_removed", resourceID, map[string]string{"kind": kind, "grantee": grantee, "eventId": eventID}); err != nil {
			return err
		}
		return notifySharedMembers(ctx, tx, team, r, grantee, eventID, true)
	})
}

// Broadcast recipients are sorted to keep notification runtime-row locks in one
// order even when different teams share the same registered members.
func notifySharedMembers(ctx context.Context, tx Tx, team d.Team, r d.Resource, grantee, eventID string, removed bool) error {
	members, err := tx.Members(ctx)
	if err != nil {
		return err
	}
	sort.Slice(members, func(i, j int) bool { return members[i].UserID < members[j].UserID })
	title, body := "资源共享已更新", "团队「"+n.EscapeMarkdownText(team.Name)+"」中的资源已向你共享。"
	if removed {
		title = "资源共享已取消"
		body = "团队「" + n.EscapeMarkdownText(team.Name) + "」中的资源已取消对你的单独共享。可见性仍由团队档位和其他共享配置决定。"
	}
	for _, member := range members {
		if grantee != "*" && member.UserID != grantee {
			continue
		}
		if err := notify(ctx, tx, member.UserID, "space-share:"+eventID, title, body, n.Link{Type: r.Kind, SpaceID: team.ID, TargetID: r.ID}); err != nil {
			return err
		}
	}
	return nil
}
