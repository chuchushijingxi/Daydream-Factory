package postgres

import (
	"context"
	a "drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	"github.com/jackc/pgx/v5"
)

func sessionEvent(ctx context.Context, tx pgx.Tx, id, audience, kind string) error {
	_, err := tx.Exec(ctx, `INSERT INTO drama_catalog_v1.session_events(user_id,audience,kind) VALUES($1,$2,$3)`, id, audience, kind)
	return err
}

// Caller holds the durable account row lock: a concurrent login cannot restore a revoked session.
// Redis is revoked before the durable change commits; an aborted change may log the user out safely.
func (s *Store) revokeSessions(ctx context.Context, tx pgx.Tx, id, kind string) error {
	runtime, err := s.runtimeState()
	if err != nil {
		return err
	}
	audiences, err := runtime.DeleteUserSessions(ctx, id)
	if err != nil {
		return err
	}
	for _, audience := range audiences {
		if err = sessionEvent(ctx, tx, id, audience, kind); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Authenticate(ctx context.Context, token string) (ports.Actor, error) {
	runtime, err := s.runtimeState()
	if err != nil {
		return ports.Actor{}, err
	}
	return runtime.Authenticate(ctx, token)
}
func (s *Store) Logout(ctx context.Context, token string) error {
	id, audience, _, err := a.SessionParts(token)
	if err != nil {
		return err
	}
	runtime, err := s.runtimeState()
	if err != nil {
		return err
	}
	deleted, err := runtime.DeleteSession(ctx, token)
	if err != nil || !deleted {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.session_events(user_id,audience,kind) VALUES($1,$2,'logout')`, id, audience)
	return err
}
