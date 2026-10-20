package project

import (
	"reflect"

	d "drama-agent/internal/domain/project"
)

// 回执表示真实存储差异，携带程序分配的 ID 和完整前后对象。
// 这里只描述采用结果，不对剧情理解作语义裁决。
type catalogChange struct {
	Before  *d.Asset `json:"before,omitempty"`
	After   *d.Asset `json:"after,omitempty"`
	AfterID string   `json:"afterId,omitempty"`
}

func catalogChanges(before, after []d.Asset) []catalogChange {
	changes := []catalogChange{}
	old, current := map[string]d.Asset{}, map[string]d.Asset{}
	for _, a := range before {
		old[a.ID] = a
	}
	for _, a := range after {
		current[a.ID] = a
	}
	for _, a := range before {
		b, exists := current[a.ID]
		if !exists {
			changes = append(changes, catalogChange{Before: &a})
		} else if !reflect.DeepEqual(a, b) {
			changes = append(changes, catalogChange{Before: &a, After: &b})
		}
	}
	for _, a := range after {
		if _, exists := old[a.ID]; !exists {
			changes = append(changes, catalogChange{After: &a})
		}
	}
	return changes
}
