// Package spacefixture supplies explicit personal-space authorization for isolated in-memory tests.
package spacefixture

import (
	"context"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
)

type Personal struct{}

func (Personal) Resolve(_ context.Context, a ports.Actor, id string) (ports.SpaceScope, error) {
	if err := a.Require(); err != nil {
		return ports.SpaceScope{}, err
	}
	if id == "" {
		id = a.UserID
	}
	if id != a.UserID {
		return ports.SpaceScope{}, fault.ErrNotFound
	}
	return ports.SpaceScope{SpaceID: id, UserID: a.UserID, Kind: spaces.Personal, Role: spaces.Owner, Status: spaces.Active, Policy: spaces.Isolated}, nil
}
func (p Personal) Access(ctx context.Context, a ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	scope, err := p.Resolve(ctx, a, r.SpaceID)
	if err != nil {
		return spaces.None, scope, err
	}
	return spaces.Decide(scope, r, nil), scope, nil
}
