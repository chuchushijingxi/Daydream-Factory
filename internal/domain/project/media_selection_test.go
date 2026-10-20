package project

import (
	"errors"
	"reflect"
	"testing"

	"drama-agent/internal/domain/fault"
)

func mediaSelectionFixture() Project {
	return Project{
		ScriptSource: ScriptSource{Version: 1},
		Storyboard:   &Storyboard{SourceVersion: 1, Shots: []Shot{{ID: "shot1", SceneID: "scene1"}, {ID: "shot2", SceneID: "scene1"}}},
		MediaSelections: []MediaSelection{
			{ShotID: "shot2", TaskID: "task-old", OutputID: "file-old", InputDigest: "d2"},
		},
	}
}

func TestAdoptMediaSelectionReplacesOnlySameShot(t *testing.T) {
	p := mediaSelectionFixture()
	before := Clone(p)
	if err := p.AdoptMediaSelection(MediaSelection{ShotID: "shot1", TaskID: "task-new", OutputID: "file-new", InputDigest: "d1"}); err != nil {
		t.Fatal("新镜头采用被拒绝", err)
	}
	if len(p.MediaSelections) != 2 || p.MediaSelections[0].ShotID != "shot2" || p.MediaSelections[1].ShotID != "shot1" {
		t.Fatal("其他镜头的既有选择被改变", p.MediaSelections)
	}
	if err := p.AdoptMediaSelection(MediaSelection{ShotID: "shot1", TaskID: "task-newer", OutputID: "file-newer", InputDigest: "d1b"}); err != nil {
		t.Fatal("同镜头重复采用被拒绝", err)
	}
	if len(p.MediaSelections) != 2 || p.MediaSelections[1].TaskID != "task-newer" {
		t.Fatal("同镜头旧选择未被替换", p.MediaSelections)
	}
	if !reflect.DeepEqual(before.Storyboard, p.Storyboard) {
		t.Fatal("采用不得改动分镜内容")
	}
}

func TestAdoptMediaSelectionRejectsUnknownShotAndBlankFields(t *testing.T) {
	cases := map[string]MediaSelection{
		"unknown shot": {ShotID: "missing", TaskID: "t", OutputID: "f", InputDigest: "d"},
		"blank shot":   {TaskID: "t", OutputID: "f", InputDigest: "d"},
		"blank task":   {ShotID: "shot1", OutputID: "f", InputDigest: "d"},
		"blank output": {ShotID: "shot1", TaskID: "t", InputDigest: "d"},
		"blank digest": {ShotID: "shot1", TaskID: "t", OutputID: "f"},
	}
	for name, selection := range cases {
		t.Run(name, func(t *testing.T) {
			p := mediaSelectionFixture()
			before := Clone(p)
			err := p.AdoptMediaSelection(selection)
			if err == nil || !reflect.DeepEqual(before, p) {
				t.Fatal("非法采用被接受", err)
			}
			if !errors.Is(err, fault.ErrNotFound) && !errors.Is(err, fault.ErrInvalid) {
				t.Fatal("拒绝未给出预期错误", err)
			}
		})
	}
	noBoard := mediaSelectionFixture()
	noBoard.Storyboard = nil
	if err := noBoard.AdoptMediaSelection(MediaSelection{ShotID: "shot1", TaskID: "t", OutputID: "f", InputDigest: "d"}); err == nil {
		t.Fatal("无分镜时采用被接受")
	}
}
