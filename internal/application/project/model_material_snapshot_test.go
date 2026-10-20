package project

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	d "drama-agent/internal/domain/project"
)

// 仅读取本地既存证据，在内存中比较同一快照的两种表示；不连接 PG 或执行模型。
func TestModelContentMaterialLocalSnapshotFootprint(t *testing.T) {
	path := os.Getenv("DRAMA_TEST_MODEL_CONTENT_SNAPSHOT")
	if path == "" {
		t.Skip("local snapshot comparison opt-in; no database or model execution")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []struct {
		Payload d.Project `json:"payload"`
	}
	if err = json.Unmarshal(raw, &snapshots); err != nil || len(snapshots) == 0 {
		t.Fatal("invalid local evidence", err)
	}
	s, _ := fixture(t, &fakeModel{})
	var measurements []map[string]any
	for _, snapshot := range snapshots {
		// 证据文件含缩进的 RawMessage；先在内存中统一 JSON 轮转基线。
		// 仅处理本地内存副本，原始证据文件、请求与数据库均不写回。
		p := d.Clone(snapshot.Payload)
		r := p.LatestRun()
		if r == nil {
			continue
		}
		before := d.Clone(p)
		req, _, err := s.request(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		input := req.Input
		if req.CurrentState != "" {
			input = req.CurrentState
		}
		if err = json.Unmarshal([]byte(input), &fields); err != nil {
			t.Fatal(err)
		}
		changed := "visualPrompts"
		if r.Contract == d.StoryboardContract {
			changed = "currentStoryboard"
			old, err := s.StoryboardEpisode(p, r.StoryboardScope.EpisodeID)
			if err != nil {
				t.Fatal(err)
			}
			for i := range old.Shots {
				if old.Shots[i].VideoPrompt != nil {
					issues := p.VideoPromptIssues(p.Storyboard.Shot(old.Shots[i].ID), r.ID)
					old.Shots[i].VideoPrompt.Valid = len(issues) == 0
					old.Shots[i].VideoPrompt.Issues = issues
				}
			}
			fields[changed] = mustJSON(old)
		} else if r.ExecutionContract() == d.VisualContract {
			var prompts []visualPromptMaterial
			if err := json.Unmarshal(fields[changed], &prompts); err != nil {
				t.Fatal(err)
			}
			old := []d.VisualPrompt{}
			for _, prompt := range prompts {
				old = append(old, *p.Prompt(prompt.Target))
			}
			fields[changed] = mustJSON(old)
		} else {
			continue
		}
		oldInput, err := sourceRefs(p.ScriptSource).rewrite(string(mustJSON(fields)), true)
		if err != nil {
			t.Fatal(err)
		}
		oldInput, err = orderModelMaterial(oldInput)
		if err != nil {
			t.Fatal(err)
		}
		var oldFields, newFields map[string]json.RawMessage
		json.Unmarshal([]byte(oldInput), &oldFields)
		json.Unmarshal([]byte(input), &newFields)
		delete(oldFields, changed)
		delete(newFields, changed)
		if len(oldInput) <= len(input) || !reflect.DeepEqual(oldFields, newFields) || !reflect.DeepEqual(before, p) {
			t.Fatal("snapshot comparison changed source, task scope or project instead of projection")
		}
		measurements = append(measurements, map[string]any{"projectId": p.ID, "field": changed, "beforeInputBytes": len(oldInput), "afterInputBytes": len(input), "savedBytes": len(oldInput) - len(input), "unit": "UTF-8 bytes of initial Input; same snapshot/source aliases; no model invocation"})
		t.Logf("%s: %d -> %d Input bytes", changed, len(oldInput), len(input))
	}
	if len(measurements) != len(snapshots) {
		t.Fatal("not all frozen snapshots exercised", len(measurements))
	}
	writeContentMaterialEvidence(t, "local-snapshot-footprint.json", measurements)
}
