package project

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

type historyTaskStore struct {
	*previewTaskStore
	batchReads, singleReads int
}

func (s *historyTaskStore) TaskInput(context.Context, string) (td.Snapshot, error) {
	s.singleReads++
	return td.Snapshot{}, fault.ErrNotFound
}
func (s *historyTaskStore) TaskInputs(ctx context.Context, scope string, ids []string) (map[string]td.Snapshot, error) {
	s.batchReads++
	return s.previewTaskStore.TaskInputs(ctx, scope, ids)
}

func TestMediaHistoryBatchesFrozenInputsAndRetainsAdoptionAndStaleness(t *testing.T) {
	ctx := context.Background()
	p := d.Project{ID: "history", Revision: 1, ScriptSource: d.ScriptSource{Version: 1}, ProjectSettings: &d.ProjectSettings{AspectRatio: "16:9"}, Assets: []d.Asset{{ID: "prop", Kind: "prop", ImageFileID: "saved"}}, VisualPrompts: []d.VisualPrompt{{VisualPromptInput: d.VisualPromptInput{Target: d.VisualTarget{AssetID: "prop"}, Text: "铜钥匙"}, Origin: "user", SourceVersion: 1}}}
	command := MediaCommand{Target: MediaTarget{AssetID: "prop"}, Revision: 1}
	_, _, digest, err := mediaInput(&p, command)
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(MediaContext{Request: command, SourceVersion: 1, InputDigest: digest})
	store := &historyTaskStore{previewTaskStore: &previewTaskStore{rows: []td.Task{{ID: "task", ScopeID: mediaScope(p.ID), Status: td.Succeeded, CreatedAt: time.Now(), State: td.State{Operation: "image.generate", Result: &td.Result{Files: []td.Output{{ID: "output", FileID: "saved"}}}}}}, inputs: map[string]td.Snapshot{"task": {Input: json.RawMessage(`{"resolution":"4K","aspectRatio":"16:9"}`), CallerContext: meta}}}}
	projects := newMemory()
	if err := projects.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	s := &Service{Store: projects, MediaTasks: &taskapp.Service{Store: store}}
	list, err := s.ListMedia(ctx, p.ID, "", 100, "image")
	if err != nil || len(list.Items) != 1 {
		t.Fatal(list, err)
	}
	item := list.Items[0]
	if item.Stale || item.AdoptedOutputID != "output" || item.Options.Resolution != "4K" || store.batchReads != 1 || store.singleReads != 0 {
		t.Fatal(item, store.batchReads, store.singleReads)
	}
	delete(store.inputs, "task")
	if _, err = s.ListMedia(ctx, p.ID, "", 100, "image"); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("missing input hidden", err)
	}
}
