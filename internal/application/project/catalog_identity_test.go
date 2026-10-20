package project

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// 个体/群体判断由免费脚本给定；这里只验证实际请求、拆分采用与正确内容保留。
func TestCatalogIdentityEinoSeparatesConcretePeopleAndPreservesCrowdContext(t *testing.T) {
	for _, repair := range []bool{false, true} {
		name := "natural-individuals"
		if repair {
			name = "repair-merged-individuals"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			m := &fakeModel{}
			s, p := fixture(t, m)
			s.Agent = einoagent.New()
			p.ScriptSource.Spans = []d.SourceSpan{
				{ID: "s1", Text: "顾宁带着编号C9的箱子来到码头。"},
				{ID: "s2", Text: "两名搬运工一起接住箱子，将它放上推车，两人没有说话。"},
				{ID: "s3", Text: "候船区约有六十名旅客。广播响起，人群一起望向闸口。"},
				{ID: "s4", Text: "先前的两名搬运工随顾宁来到检票口，仍共同扶住箱子。"},
			}
			keepers := []d.Asset{
				{ID: "named", Kind: "character", Name: "顾宁", Aliases: []string{"小顾"}, Description: "带编号C9的箱子到码头，随后来到检票口。", SourceIDs: []string{"s1", "s4"}},
				{ID: "case", Kind: "prop", Name: "编号C9的箱子", Description: "顾宁带来的箱子，由两名搬运工共同搬运并扶住。", SourceIDs: []string{"s1", "s2", "s4"}},
			}
			p.Assets = keepers
			var err error
			p, err = s.commit(ctx, p, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			source := p.ScriptSource
			person := CatalogAssetInput{Kind: "character", Name: "搬运工甲", Description: "两名共同搬运箱子的人员之一，与另一人接箱、放上推车，并随顾宁到检票口共同扶箱；原文未分别命名。", SourceIDs: []string{"s2", "s4"}}
			other := person
			other.Name = "搬运工乙"
			primaryID, secondaryID := "", ""
			extractCalls, correctCalls := 0, 0
			reply := func(tool string, args any) ports.Response {
				return ports.Response{Actions: []d.Action{action(tool, args)}, Raw: mustJSON(map[string]any{"freeFixture": true, "tool": tool, "arguments": args}), FinishReason: "tool_calls", Usage: billing.Usage{Known: true}}
			}
			assertIndividuals := func(current d.Project) {
				t.Helper()
				if len(current.Assets) != 4 || !reflect.DeepEqual(current.Assets[:2], keepers) {
					t.Fatal("split changed correct assets or mechanically created crowd members")
				}
				first, second := current.Assets[2], current.Assets[3]
				if first.ID != primaryID || second.ID == "" || second.ID == primaryID || first.Name != person.Name || second.Name != other.Name || first.Description != person.Description || second.Description != other.Description || !reflect.DeepEqual(first.SourceIDs, person.SourceIDs) || !reflect.DeepEqual(second.SourceIDs, other.SourceIDs) {
					t.Fatal("individual identities, common actions or repeated-source references were lost")
				}
				if secondaryID != "" && secondaryID != second.ID {
					t.Fatal("repeated appearance replaced a saved identity")
				}
				secondaryID = second.ID
			}
			m.generate = func(req ports.Request) (ports.Response, error) {
				current, e := s.Project(ctx, p.ID)
				if e != nil {
					t.Fatal(e)
				}
				var input struct {
					Source  []d.SourceSpan `json:"source"`
					Targets []string       `json:"targetSpanIds"`
					Assets  []d.Asset      `json:"assets"`
				}
				if json.Unmarshal([]byte(req.Input), &input) != nil || !reflect.DeepEqual(input.Source, modelSpans(source)) || len(input.Targets) != len(source.Spans) || len(input.Assets) != len(current.Assets) {
					t.Fatal("actual request lost complete source, crowd context or saved catalog")
				}
				if !strings.Contains(req.System, factRules) || len(req.Tools) != 2 || req.Tools[0].Name != "save_catalog" || req.Tools[1].Name != "finish_batch" {
					t.Fatal("identity instructions or existing tool permissions missing")
				}
				if current.LatestRun().Stage == "extract" {
					extractCalls++
					if req.System != ExtractPrompt+modelSourceInstruction {
						t.Fatal("extraction instruction not sent")
					}
					if extractCalls == 1 {
						assets := []CatalogAssetInput{person, other}
						if repair {
							// 注入结构合法的误合卡，纠错脚本须真正拆分，不能只改说明。
							merged := person
							merged.Name, merged.Description = "搬运人员（两人）", "两名未命名人员共同接箱、推车并扶箱。"
							assets = []CatalogAssetInput{merged}
						}
						return reply("save_catalog", CatalogChangeSet{Assets: assets}), nil
					}
					primaryID = current.Assets[2].ID
					if len(req.Dialogue) != 2 || !strings.Contains(req.Dialogue[1].Content, primaryID) {
						t.Fatal("saved identity was not delivered in the actual tool receipt")
					}
					return reply("finish_batch", struct{}{}), nil
				}
				correctCalls++
				if current.LatestRun().Stage != "correct" || req.System != CorrectPrompt+modelSourceInstruction {
					t.Fatal("wrong correction stage or instruction")
				}
				if correctCalls == 1 {
					if len(req.Dialogue) != 0 {
						t.Fatal("correction inherited completed extraction dialogue")
					}
					if repair {
						first := person
						first.ID = primaryID
						return reply("save_catalog", CatalogChangeSet{Assets: []CatalogAssetInput{first, other}}), nil
					}
				}
				assertIndividuals(current)
				if repair && (len(req.Dialogue) != 2 || !strings.Contains(req.Dialogue[1].Content, secondaryID) || !strings.Contains(req.Dialogue[1].Content, "before")) {
					t.Fatal("correction did not receive real split changes and both saved identities")
				}
				return reply("finish_batch", struct{}{}), nil
			}
			if err = s.Execute(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
			got, err := s.Project(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertIndividuals(got)
			wantCorrect := 1
			if repair {
				wantCorrect = 2
			}
			if extractCalls != 2 || correctCalls != wantCorrect || got.LatestRun().Status != d.RunCompleted || len(got.Runs) != 1 || !reflect.DeepEqual(source, got.ScriptSource) || got.UsedMicroCNY() != 0 {
				t.Fatal("identity fixture failed, changed source or incurred nonzero fixture costs")
			}
			for _, id := range []string{primaryID, secondaryID} {
				target := d.VisualTarget{AssetID: id}
				if !got.ValidPrompt(got.Prompt(target), "") {
					t.Fatal("separate identity did not reach its own downstream visual target")
				}
			}
			if dir := os.Getenv("DRAMA_TEST_CATALOG_EVIDENCE_DIR"); dir != "" {
				if err = os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, name+".json"), mustJSON(got), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCatalogIdentityInstructionsCountTowardFullRequestCapacity(t *testing.T) {
	for _, stage := range []string{"extract", "correct"} {
		t.Run(stage, func(t *testing.T) {
			m := &fakeModel{}
			s, p := fixture(t, m)
			p.ScriptSource.Spans = []d.SourceSpan{{ID: "s1", Text: "两名维修人员一同抬起木板，另一边的观众若干。"}}
			p.LatestRun().Stage = stage
			full, _, err := s.request(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			needed := inputSize(full) + len(mustJSON(full.Tools))
			m.window, m.output = needed+1200, 1200
			bounded, _, err := s.request(context.Background(), p)
			if err != nil || bounded.MaxOutputTokens != 1200 || full.System != bounded.System || full.Input != bounded.Input || !reflect.DeepEqual(full.Tools, bounded.Tools) {
				t.Fatal("request capacity omitted instructions or discarded full material", err)
			}
			m.window = needed
			if _, _, err = s.request(context.Background(), p); !errors.Is(err, fault.ErrCapacity) || m.count != 0 {
				t.Fatal("exhausted capacity did not reject before model call", err)
			}
			t.Logf("%s: system=%d bytes, input+tools=%d conservative units; 1200 output fits, exact input boundary rejected", stage, len(full.System), needed)
		})
	}
}
