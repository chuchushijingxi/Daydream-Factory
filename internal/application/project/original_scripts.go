package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func (s *Service) OriginalScript(ctx context.Context, id string) (d.Project, []byte, error) {
	p, err := s.Project(ctx, id)
	if err != nil {
		return p, nil, err
	}
	raw := p.Original
	if p.SourceFileID != "" {
		if s.Files == nil {
			return p, nil, fault.New("FILE_STORAGE_UNAVAILABLE", "文件存储不可用")
		}
		_, raw, err = s.Files.ReadOriginal(ctx, p.SourceFileID)
		if err != nil {
			return p, nil, err
		}
	} else if p.OriginalFile != nil {
		if s.OriginalScripts == nil {
			return p, nil, fault.New("SCRIPT_STORAGE_UNAVAILABLE", "原剧本存储不可用")
		}
		raw, err = s.OriginalScripts.Read(ctx, p.ID, *p.OriginalFile)
		if err != nil {
			return p, nil, err
		}
	}
	if len(raw) == 0 {
		return p, nil, fault.ErrNotFound
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != p.ScriptSource.SHA256 {
		return p, nil, fault.New("SCRIPT_FILE_CORRUPT", "原剧本文件校验失败")
	}
	return p, raw, nil
}
