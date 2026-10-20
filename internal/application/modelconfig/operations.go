package modelconfig

import (
	"context"
	"drama-agent/internal/application/ports"
)

// Operations is the only application entry to configuration management. HTTP
// and other trusted adapters supply the same Actor and share its precondition.
type Operations struct {
	s     *Service
	actor ports.Actor
}

func (c *Service) ForOperator(a ports.Actor) *Operations { return &Operations{s: c, actor: a} }
func (o *Operations) View(ctx context.Context) (ModelConfigView, error) {
	if e := o.actor.RequireOperator(); e != nil {
		return ModelConfigView{}, e
	}
	return o.s.configView(ctx)
}
func (o *Operations) Update(ctx context.Context, in ModelConfigInput) (ModelConfigView, error) {
	if e := o.actor.RequireOperator(); e != nil {
		return ModelConfigView{}, e
	}
	return o.s.update(ctx, in)
}
func (o *Operations) Delete(ctx context.Context, id string, revision uint64) (ModelConfigView, error) {
	if e := o.actor.RequireOperator(); e != nil {
		return ModelConfigView{}, e
	}
	return o.s.delete(ctx, id, revision)
}
func (o *Operations) DiscoverModels(ctx context.Context, in DiscoverModelsInput) (DiscoverModelsView, error) {
	if e := o.actor.RequireOperator(); e != nil {
		return DiscoverModelsView{}, e
	}
	return o.s.discoverModels(ctx, in)
}
func (o *Operations) UploadLogo(ctx context.Context, raw []byte) (LogoView, error) {
	if e := o.actor.RequireOperator(); e != nil {
		return LogoView{}, e
	}
	return o.s.uploadLogo(ctx, raw)
}
func (o *Operations) ReadLogo(ctx context.Context, id string) ([]byte, error) {
	if e := o.actor.RequireOperator(); e != nil {
		return nil, e
	}
	return o.s.readLogo(ctx, id)
}

type Reader struct {
	s     *Service
	actor ports.Actor
}

func (c *Service) ForActor(a ports.Actor) *Reader { return &Reader{c, a} }
func (r *Reader) ReadLogo(ctx context.Context, id string) ([]byte, error) {
	if e := r.actor.Require(); e != nil {
		return nil, e
	}
	return r.s.readLogo(ctx, id)
}
