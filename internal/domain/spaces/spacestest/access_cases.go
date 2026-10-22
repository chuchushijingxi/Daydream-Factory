// Package spacestest shares access cases between pure rules and SQL contract tests.
package spacestest

import (
	"fmt"

	"drama-agent/internal/domain/spaces"
)

type AccessCase struct {
	Name     string
	Scope    spaces.Scope
	Resource spaces.Resource
	Shares   []spaces.Share
	Want     spaces.Access
}

// AccessCases 用明确的档位/共享权限表推导组合期望，供单资源与列表 SQL 同表核对。
func AccessCases() []AccessCase {
	type policyShare struct {
		policy spaces.Policy
		share  spaces.Access
		want   spaces.Access
	}
	permissions := []policyShare{
		{spaces.Isolated, spaces.None, spaces.None},
		{spaces.Isolated, spaces.Read, spaces.Read},
		{spaces.Isolated, spaces.Edit, spaces.Edit},
		{spaces.ReadAll, spaces.None, spaces.Read},
		{spaces.ReadAll, spaces.Read, spaces.Read},
		{spaces.ReadAll, spaces.Edit, spaces.Edit},
		{spaces.EditAll, spaces.None, spaces.Edit},
		{spaces.EditAll, spaces.Read, spaces.Edit},
		{spaces.EditAll, spaces.Edit, spaces.Edit},
	}
	var cases []AccessCase
	for _, kind := range []spaces.Kind{spaces.Personal, spaces.TeamKind} {
		spaceID := "space"
		if kind == spaces.Personal {
			spaceID = "user"
		}
		for _, role := range []spaces.Role{spaces.Owner, spaces.Admin, spaces.Member} {
			for _, permission := range permissions {
				for _, status := range []spaces.Status{spaces.Active, spaces.Expired, spaces.Disabled} {
					for _, readOnly := range []bool{false, true} {
						for _, own := range []bool{false, true} {
							for _, resourceKind := range []string{spaces.ProjectResource, spaces.CanvasResource, spaces.UploadResource, spaces.MediaResource, spaces.AgentSessionResource} {
								for _, grantee := range []string{"user", "*", "another-user"} {
									want := permission.want
									owner := "another-user"
									if own {
										owner = "user"
									}
									if kind == spaces.Personal {
										want = spaces.None
										if own {
											want = spaces.Edit
										}
									} else if own || role == spaces.Owner || role == spaces.Admin {
										want = spaces.Edit
									} else if resourceKind == spaces.AgentSessionResource {
										want = spaces.None
									} else if grantee == "another-user" || (resourceKind != spaces.ProjectResource && resourceKind != spaces.CanvasResource) {
										want = map[spaces.Policy]spaces.Access{spaces.Isolated: spaces.None, spaces.ReadAll: spaces.Read, spaces.EditAll: spaces.Edit}[permission.policy]
									}
									if kind == spaces.TeamKind && status == spaces.Disabled {
										want = spaces.None
									}
									if readOnly && want == spaces.Edit {
										want = spaces.Read
									}
									cases = append(cases, AccessCase{
										Name:     fmt.Sprintf("%s/%s/%s/share%d/%s/readonly%t/own%t/%s/%s", kind, role, permission.policy, permission.share, status, readOnly, own, resourceKind, grantee),
										Scope:    spaces.Scope{SpaceID: spaceID, Kind: kind, UserID: "user", Role: role, Policy: permission.policy, Status: status, ReadOnly: readOnly},
										Resource: spaces.Resource{SpaceID: spaceID, OwnerID: owner, Kind: resourceKind, ID: "resource"},
										Shares:   []spaces.Share{{Grantee: grantee, Permission: permission.share}}, Want: want,
									})
								}
							}
						}
					}
				}
			}
		}
	}
	return cases
}
