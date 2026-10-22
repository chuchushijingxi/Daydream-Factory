package spaces

// SharingKind restricts explicit grants to top-level content containers.
func SharingKind(kind string) bool { return kind == ProjectResource || kind == CanvasResource }

// CanManageSharing deliberately does not use Decide(Edit): a grantee, or a
// member in edit_all, may edit content without acquiring the right to re-share.
// ReadOnly caps content writes; it does not remove team management permissions.
func CanManageSharing(s Scope, r Resource) bool {
	return s.Kind == TeamKind && s.SpaceID != "" && s.SpaceID == r.SpaceID &&
		s.UserID != "" && s.Role.Valid() && s.Status != Disabled && SharingKind(r.Kind) &&
		r.ID != "" && r.OwnerID != "" && (r.OwnerID == s.UserID || s.Role == Owner || s.Role == Admin)
}

func (s Share) Valid() bool { return s.Grantee != "" && (s.Permission == Read || s.Permission == Edit) }
