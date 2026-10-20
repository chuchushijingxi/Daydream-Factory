package project

import "drama-agent/internal/domain/fault"

// MediaSelection 是镜头当前采用的媒体选择；任务历史保留在任务模块。
type MediaSelection struct {
	ShotID      string `json:"shotId"`
	TaskID      string `json:"taskId"`
	OutputID    string `json:"outputId"`
	InputDigest string `json:"inputDigest"`
}

// AdoptMediaSelection 替换一个镜头当前的媒体选择；每个镜头至多一条。
// 采用是用户选择：不改资产、不受分集锁定约束，输入新鲜度摘要由调用方在采用前核对。
func (p *Project) AdoptMediaSelection(selection MediaSelection) error {
	if selection.ShotID == "" || selection.TaskID == "" || selection.OutputID == "" || selection.InputDigest == "" {
		return fault.ErrInvalid
	}
	if p.Storyboard == nil || p.Storyboard.Shot(selection.ShotID) == nil {
		return fault.ErrNotFound
	}
	kept := make([]MediaSelection, 0, len(p.MediaSelections)+1)
	for _, old := range p.MediaSelections {
		if old.ShotID != selection.ShotID {
			kept = append(kept, old)
		}
	}
	p.MediaSelections = append(kept, selection)
	return nil
}
