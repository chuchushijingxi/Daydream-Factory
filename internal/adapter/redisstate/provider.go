package redisstate

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/task"
)

// Different connection records with the same provider account share one quota.
// Only this digest, never credentials or endpoint query parameters, becomes a Redis key.
func providerAccount(b d.Binding) string {
	host := b.Endpoint
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = strings.ToLower(u.Host)
	}
	return digest(b.Protocol + ":" + host + ":" + b.AccountScope + ":" + string(b.Credential))
}
func (s *Store) AcquireTaskCapacity(ctx context.Context, b d.Binding, stage string, limit int) (ports.Lease, bool, error) {
	if limit > s.settings.ProviderConcurrency {
		limit = s.settings.ProviderConcurrency
	}
	account := providerAccount(b)
	lease, ok, err := s.HoldCapacity(ctx, account, stage, limit)
	if err != nil || !ok {
		return lease, ok, err
	}
	if stage != "save" {
		allowed, err := s.Allow(ctx, "provider:"+account, s.settings.ProviderRequestsPerMinute, time.Minute)
		if err != nil || !allowed {
			lease.Release()
			return ports.Lease{}, false, err
		}
	}
	return lease, true, nil
}

type limitedModel struct {
	ports.Model
	runtime *Store
	binding d.Binding
}

// Capacity failures occur before the provider adapter is invoked. Preserve that
// evidence so the existing caller can settle a known zero-cost rejection.
type notSent struct{ cause error }

func (e *notSent) Error() string        { return e.cause.Error() }
func (e *notSent) Unwrap() error        { return e.cause }
func (e *notSent) RequestNotSent() bool { return true }

func (s *Store) LimitModel(model ports.Model, protocol, endpoint, credential string) ports.Model {
	return &limitedModel{Model: model, runtime: s, binding: d.Binding{Protocol: protocol, Endpoint: endpoint, Credential: []byte(credential)}}
}
func (m *limitedModel) Generate(ctx context.Context, request ports.Request, save func(json.RawMessage) error) (ports.Response, error) {
	for {
		lease, ok, err := m.runtime.AcquireTaskCapacity(ctx, m.binding, "submit", m.runtime.settings.ProviderConcurrency)
		if err != nil {
			return ports.Response{}, &notSent{err}
		}
		if ok {
			defer lease.Release()
			if err = lease.Check(ctx); err != nil {
				return ports.Response{}, &notSent{err}
			}
			return m.Model.Generate(lease.Context, request, save)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ports.Response{}, &notSent{ctx.Err()}
		case <-timer.C:
		}
	}
}
func (m *limitedModel) ResponseRequestID(raw json.RawMessage) string {
	if reader, ok := m.Model.(ports.ResponseMetadata); ok {
		return reader.ResponseRequestID(raw)
	}
	return ""
}

var _ ports.Model = (*limitedModel)(nil)
var _ ports.ResponseMetadata = (*limitedModel)(nil)
