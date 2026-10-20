package postgres

import "context"

func (s *Store) WatchCreditAccount(ctx context.Context, user string) (<-chan struct{}, func(), error) {
	r, err := s.runtimeState()
	if err != nil {
		return nil, nil, err
	}
	return r.Watch(ctx, "credits:"+user)
}
func (s *Store) WatchNotifications(ctx context.Context, user string) (<-chan struct{}, func(), error) {
	r, err := s.runtimeState()
	if err != nil {
		return nil, nil, err
	}
	return r.Watch(ctx, "notifications:"+user)
}
