package project

import (
	"fmt"
	"strings"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
)

// AdoptCatalogChanges 只保护身份 ID、来源引用及原子性。
// 不比较名称、事实、标签或文案与原文，不替模型理解剧情。
func AdoptCatalogChanges(assets []Asset, removeIDs []string, current []Asset, source ScriptSource) ([]Asset, error) {
	validSource := map[string]bool{}
	for _, s := range source.Spans {
		validSource[s.ID] = true
	}
	validID := map[string]bool{}
	for _, a := range current {
		validID[a.ID] = true
	}
	used := map[string]bool{}
	removed := map[string]bool{}
	for i, id := range removeIDs {
		id = strings.TrimSpace(id)
		if !validID[id] {
			return nil, fmt.Errorf("%w: removeIds[%d] %q is not in the provided catalog", fault.ErrInvalid, i, id)
		}
		removed[id] = true
	}
	updates := map[string]Asset{}
	added := []Asset{}
	for i, a := range assets {
		a.ID = strings.TrimSpace(a.ID)
		a.Name = strings.TrimSpace(a.Name)
		a.Kind = strings.TrimSpace(a.Kind)
		if a.Kind != "character" && a.Kind != "location" && a.Kind != "prop" {
			return nil, fmt.Errorf("%w: assets[%d].kind unsupported", fault.ErrInvalid, i)
		}
		if a.Name == "" || len(a.SourceIDs) == 0 {
			return nil, fmt.Errorf("%w: assets[%d] needs name and sourceIds", fault.ErrInvalid, i)
		}
		if a.ID != "" && !validID[a.ID] {
			return nil, fmt.Errorf("%w: proposed assets[%d].id %q is not in the provided catalog; a new object must omit id", fault.ErrInvalid, i, a.ID)
		}
		if a.ID != "" && used[a.ID] {
			return nil, fmt.Errorf("%w: proposed assets[%d].id %q repeats an earlier object in this proposed list", fault.ErrInvalid, i, a.ID)
		}
		if removed[a.ID] {
			return nil, fmt.Errorf("%w: assets[%d].id %q is both replaced and removed", fault.ErrInvalid, i, a.ID)
		}
		isNew := a.ID == ""
		if a.ID == "" {
			a.ID = identity.New("asset")
		}
		used[a.ID] = true
		a.SourceIDs = unique(a.SourceIDs)
		for _, id := range a.SourceIDs {
			if !validSource[id] {
				return nil, fmt.Errorf("%w: assets[%d].sourceIds contains invalid reference %q", fault.ErrInvalid, i, id)
			}
		}
		a.Aliases = unique(a.Aliases)
		if isNew {
			added = append(added, a)
		} else {
			updates[a.ID] = a
		}
	}
	// 未列出不等于删除；全部命令验证成功后才替换当前目录。
	// 身份来自已有 ID，不能按名称合并或静默丢弃对象。
	out := make([]Asset, 0, len(current)+len(added))
	for _, a := range current {
		if removed[a.ID] {
			continue
		}
		if update, ok := updates[a.ID]; ok {
			a = update
		}
		out = append(out, a)
	}
	out = append(out, added...)
	return out, nil
}

func unique(xs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
