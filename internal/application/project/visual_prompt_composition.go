package project

import (
	"context"
	"fmt"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// ComposeVisualPrompt 统一组装未来媒体请求的正文与固定风格。
// 此处只回读与拼接，不外发、不改写已保存内容。
func (s *Service) ComposeVisualPrompt(ctx context.Context, id string, target d.VisualTarget) (string, error) {
	p, err := s.Project(ctx, id)
	if err != nil {
		return "", err
	}
	if err = p.ValidateTarget(target); err != nil {
		return "", err
	}
	if p.FailedTask() != nil || p.Style == nil {
		return "", fault.ErrInvalid
	}
	v := p.Prompt(target)
	if !p.ValidPrompt(v, "") {
		return "", fault.ErrInvalid
	}
	if p.Style.Prompts.ZH == "" {
		return "", fmt.Errorf("%w: fixed style has no Chinese prompt", fault.ErrInvalid)
	}
	return v.Text + "\n\n风格要求:\n" + p.Style.Prompts.ZH, nil
}
