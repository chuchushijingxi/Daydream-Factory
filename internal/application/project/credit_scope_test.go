package project

import (
	"context"
	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
	"errors"
	"strings"
	"testing"
	"time"
)

type scopeFixturePrices struct{}

func (scopeFixturePrices) CreditPrice(context.Context, string) (c.PriceRule, uint64, string, error) {
	return c.PriceRule{UnitPrice: 2}, 7, "connection", nil
}
func TestCreditTargetScopesExcludeSupportProtocolAndOutput(t *testing.T) {
	p := d.Project{Revision: 8, ScriptSource: d.ScriptSource{Version: 2, Spans: []d.SourceSpan{{ID: "one", Text: "本集 A1。"}, {ID: "two", Text: "其他集不可收费"}}}, ProjectSettings: &d.ProjectSettings{LLMModel: "model"}, Assets: []d.Asset{{ID: "asset", Kind: "character", Name: "林", Aliases: []string{"小林"}, Description: "修理师", Character: &d.Character{RoleType: "主角", Age: "20", Gender: "女", Biography: "完整经历", Appearance: "灰衣", Personality: "冷静"}, Looks: []d.Look{{ID: "look", Name: "雨衣", Description: "蓝色雨衣"}}}}}
	p.Storyboard = d.NewStoryboard(2)
	p.Storyboard.Index.Episodes = []d.Episode{{ID: "ep", EpisodeValue: d.EpisodeValue{SourceRanges: []d.SourceRange{{Start: d.SourcePoint{SpanID: "one"}, End: d.SourcePoint{SpanID: "one", Offset: len([]rune(p.ScriptSource.Spans[0].Text))}}}}}}
	text, scope, e := chargeText(p, &d.Run{Contract: d.StoryboardContract, StoryboardScope: &d.StoryboardScope{EpisodeID: "ep", Operation: "breakdown"}})
	if e != nil || scope != "episode" || text != "本集 A1。" {
		t.Fatal(text, scope, e)
	}
	target := d.Run{Contract: d.VisualContract, Targets: []d.VisualTarget{{AssetID: "asset"}, {AssetID: "asset", LookID: "look"}}}
	text, scope, e = chargeText(p, &target)
	if e != nil || scope != "asset-material" || !strings.Contains(text, "完整经历") || !strings.Contains(text, "蓝色雨衣") || strings.Contains(text, "其他集") || strings.Contains(text, "asset") || strings.Count(text, "修理师") != 1 {
		t.Fatal(text, scope, e)
	}
	service := &Service{Prices: scopeFixturePrices{}}
	q, _, e := service.quote(context.Background(), p, &target, time.Now())
	if e != nil || q.Quantity != c.Count(text) || q.InputRevision != 8 || q.SourceVersion != 2 {
		t.Fatal(q, e)
	}
	p.Revision++
	changed, _, e := service.quote(context.Background(), p, &target, time.Now())
	if e != nil || q.Basis == changed.Basis || q.Amount != changed.Amount {
		t.Fatal("version change not detected", q, changed, e)
	}
}

type absentMediaTasks struct{ ports.TaskStore }

func (s absentMediaTasks) ForTaskScope(ports.SpaceScope) ports.TaskStore { return s }

func (absentMediaTasks) TaskByCommand(context.Context, string, string) (td.Task, error) {
	return td.Task{}, fault.ErrNotFound
}
func TestOwnedMediaCannotStartWithoutBillingResources(t *testing.T) {
	s, _, p := p5Prepared(t)
	p.SpaceID, p.OwnerID, p.CreatorID = "fixture-owner", "fixture-owner", "fixture-owner"
	var e error
	p, e = s.commit(context.Background(), p, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	s.MediaTasks = &taskapp.Service{Store: absentMediaTasks{}}
	input := MediaCommand{CommandID: "cmd", Revision: p.Revision, ModelID: "mock-image", Target: MediaTarget{AssetID: "c"}}
	_, e = s.StartMedia(context.Background(), p.ID, input)
	var problem *fault.Error
	if !errors.As(e, &problem) || problem.Code != "MODEL_PRICE_UNAVAILABLE" {
		t.Fatal("missing price treated free", e)
	}
	s.Prices = scopeFixturePrices{}
	_, e = s.StartMedia(context.Background(), p.ID, input)
	if !errors.As(e, &problem) || problem.Code != "MODEL_PRICE_UNAVAILABLE" {
		t.Fatal("missing ledger treated free", e)
	}
	after, _ := s.Project(context.Background(), p.ID)
	if after.Revision != p.Revision {
		t.Fatal("refusal changed project")
	}
}
