package postgres

import (
	"context"
	"encoding/json"
	"time"
)

func readProjection[T any](ctx context.Context, s *Store, key string, load func(context.Context) (T, error)) (T, error) {
	var out T
	r, err := s.runtimeState()
	if err != nil {
		return out, err
	}
	raw, err := r.ReadCached(ctx, key, 5*time.Second, func(c context.Context) ([]byte, error) {
		value, err := load(c)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	})
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}
