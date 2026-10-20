package project

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	uploadapp "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/uploadfiles"
)

func p5Response(name string, value any) ports.Response {
	a := action(name, value)
	return ports.Response{Actions: []d.Action{a}, Raw: json.RawMessage(`{"freeFixture":true}`), FinishReason: "tool_calls", Usage: billing.Usage{Known: true}}
}
func p5Range(span, text string) d.SourceRange {
	return d.SourceRange{Start: d.SourcePoint{SpanID: span}, End: d.SourcePoint{SpanID: span, Offset: len([]rune(text))}}
}
func p5Service(t *testing.T) (*Service, *fakeModel) {
	t.Helper()
	m := &fakeModel{}
	s := &Service{Store: newMemory(), Models: &modelregistry.Registry{Model: m, ModelPrices: map[string]billing.TokenPricing{}}, Agent: einoagent.New(), Importer: scriptimport.Importer{}, Authorize: freeAuthorization}
	return s, m
}
func p5Prepared(t *testing.T) (*Service, *fakeModel, d.Project) {
	t.Helper()
	s, m := p5Service(t)
	source := []d.SourceSpan{{ID: "s1", Text: "上集😀。林晚放下铜钥匙，对陈默说：你自己决定。"}, {ID: "s2", Text: "下集。陈默站在庭院，点头。"}}
	p := d.Project{ID: "p5", Revision: 1, ScriptSource: d.ScriptSource{Version: 1, Spans: source}, Assets: []d.Asset{{ID: "c", Name: "林晚", Kind: "character", Description: "灰外套", Looks: []d.Look{{ID: "l", Name: "雨衣", Description: "雨衣形象"}}}, {ID: "c2", Name: "陈默", Kind: "character", Description: "另一人"}, {ID: "loc", Name: "客厅", Kind: "location"}, {ID: "prop", Name: "铜钥匙", Kind: "prop"}, {ID: "unused", Name: "铜钥匙", Kind: "prop", Description: "另一把同名钥匙"}}, Storyboard: d.NewStoryboard(1), Runs: []d.Run{{ID: "index", Contract: d.EpisodeIndexContract, SourceVersion: 1, Status: d.RunCompleted}, {ID: "catalog", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: d.RunCompleted}}}
	mode := "episodes"
	p.Storyboard.Index.Segmentation = &mode
	for i, span := range source {
		p.Storyboard.Index.Episodes = append(p.Storyboard.Index.Episodes, d.Episode{ID: []string{"e1", "e2"}[i], EpisodeValue: d.EpisodeValue{Title: []string{"上集", "下集"}[i], SourceRanges: []d.SourceRange{p5Range(span.ID, span.Text)}}, SceneIDs: []string{}, Origin: "model"})
	}
	for _, a := range p.Assets {
		targets := []d.VisualTarget{{AssetID: a.ID}}
		for _, l := range a.Looks {
			targets = append(targets, d.VisualTarget{AssetID: a.ID, LookID: l.ID})
		}
		for _, target := range targets {
			v := d.VisualPrompt{VisualPromptInput: d.VisualPromptInput{Target: target, Text: "有效完整外观"}, SourceVersion: 1, Origin: "user"}
			if target.LookID != "" {
				v.BasePrompt = &d.ActionRef{}
			}
			p.VisualPrompts = append(p.VisualPrompts, v)
		}
	}
	if e := s.Store.Create(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	return s, m, p
}
func p5Scene(ep d.Episode) map[string]any {
	return map[string]any{"title": "客厅交谈", "sourceRanges": ep.SourceRanges, "locationAssetId": "loc", "time": "日", "summary": "林晚放下钥匙并说话"}
}
func p5Shot(ep d.Episode) map[string]any {
	return map[string]any{"sourceRanges": ep.SourceRanges, "action": "林晚将铜钥匙放到桌上，松手停顿", "dialogues": []any{map[string]any{"speakerAssetId": "c", "text": "你自己决定。"}}, "camera": d.Camera{Size: "中景", Angle: "平视", Movement: "随手下移"}, "durationSeconds": nil, "designNotes": "结束停在钥匙与未伸出的手之间", "selectedAssets": []d.VisualTarget{{AssetID: "c"}, {AssetID: "c2"}, {AssetID: "loc"}, {AssetID: "prop"}}, "videoPrompt": map[string]any{"parts": p5Parts(false)}}
}
func p5Parts(bad bool) []d.PromptPart {
	end := "松开手，说：你自己决定。镜头停在钥匙与陈默未伸出的手之间。"
	if bad {
		end = "松开手，镜头停在钥匙上。"
	}
	return []d.PromptPart{{Type: "asset", AssetID: "loc"}, {Type: "text", Text: "内，中景平视。"}, {Type: "asset", AssetID: "c"}, {Type: "text", Text: "站在桌边，对"}, {Type: "asset", AssetID: "c2"}, {Type: "text", Text: "说话，将"}, {Type: "asset", AssetID: "prop"}, {Type: "text", Text: "放在桌上，" + end}}
}
func p5Change(t *testing.T, s *Service, p d.Project, ops ...any) (d.Project, d.StoryboardReceipt) {
	t.Helper()
	receipt, e := s.ChangeStoryboard(context.Background(), p.ID, mustJSON(map[string]any{"revision": p.Revision, "operations": ops}))
	if e != nil {
		t.Fatal(e)
	}
	p, e = s.Project(context.Background(), p.ID)
	if e != nil {
		t.Fatal(e)
	}
	return p, receipt
}

func TestManualSceneNeedsOnlyNameAndSummaryFromTheUser(t *testing.T) {
	s, _, p := p5Prepared(t)
	ep := *p.Storyboard.Episode("e1")
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": ep.ID,
		"value": d.SceneValue{Title: "雨夜归来", Summary: "她停在门口。", SourceRanges: ep.SourceRanges}})
	scene := p.Storyboard.Scenes[0]
	if scene.Title != "雨夜归来" || scene.Summary != "她停在门口。" || scene.Time != "" || scene.LocationAssetID != nil || scene.Origin != "user" || !reflect.DeepEqual(scene.SourceRanges, ep.SourceRanges) {
		t.Fatal("manual scene invented or lost metadata", scene)
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": scene.ID,
		"value": map[string]any{"sourceRanges": scene.SourceRanges, "action": "", "dialogues": []any{},
			"camera": d.Camera{}, "durationSeconds": nil, "designNotes": "",
			"selectedAssets": []d.VisualTarget{}, "videoPrompt": nil}})
	shot := p.Storyboard.Shots[0]
	if len(shot.SelectedAssets) != 0 || shot.VideoPrompt != nil || len(p.Runs) != 2 || p.UsedMicroCNY() != 0 {
		t.Fatal("manual creation selected assets, generated content or created charges", shot)
	}
}

func TestStoryboardInsertShotUsesOneCommitAndRejectsWrongAnchor(t *testing.T) {
	s, _, p := p5Prepared(t)
	p, _ = p5Change(t, s, p,
		map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))},
		map[string]any{"op": "createScene", "episodeId": "e2", "value": p5Scene(*p.Storyboard.Episode("e2"))})
	sceneID, otherSceneID := p.Storyboard.Scenes[0].ID, p.Storyboard.Scenes[1].ID
	value := p5Shot(*p.Storyboard.Episode("e1"))
	p, _ = p5Change(t, s, p,
		map[string]any{"op": "createShot", "sceneId": sceneID, "value": value},
		map[string]any{"op": "createShot", "sceneId": sceneID, "value": value},
		map[string]any{"op": "createShot", "sceneId": otherSceneID, "value": p5Shot(*p.Storyboard.Episode("e2"))})
	beforeRevision, beforeShots := p.Revision, append([]d.Shot{}, p.Storyboard.Shots...)
	first, second := p.Storyboard.Scene(sceneID).ShotIDs[0], p.Storyboard.Scene(sceneID).ShotIDs[1]
	p, receipt := p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": sceneID, "afterShotId": first, "value": value})
	ids := p.Storyboard.Scene(sceneID).ShotIDs
	if p.Revision != beforeRevision+1 || len(ids) != 3 || ids[0] != first || ids[2] != second || receipt.Revision != p.Revision {
		t.Fatalf("insert did not commit directly between existing shots: revision=%d order=%v", p.Revision, ids)
	}
	for _, shot := range beforeShots {
		if !reflect.DeepEqual(*p.Storyboard.Shot(shot.ID), shot) {
			t.Fatal("insertion rewrote an existing shot")
		}
	}
	beforeRevision = p.Revision
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": sceneID, "beforeShotId": first, "value": value})
	ids = p.Storyboard.Scene(sceneID).ShotIDs
	if p.Revision != beforeRevision+1 || len(ids) != 4 || ids[1] != first || ids[3] != second {
		t.Fatal("insertion before the first shot did not commit at the beginning", ids)
	}
	beforeRevision = p.Revision
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": sceneID, "afterShotId": second, "value": value})
	ids = p.Storyboard.Scene(sceneID).ShotIDs
	if p.Revision != beforeRevision+1 || len(ids) != 5 || ids[3] != second {
		t.Fatal("insertion after the last shot did not commit at the end", ids)
	}
	baseline := p
	foreign := p.Storyboard.Scene(otherSceneID).ShotIDs[0]
	for _, anchor := range []map[string]any{
		{"afterShotId": "missing"}, {"beforeShotId": "missing"},
		{"afterShotId": foreign}, {"beforeShotId": foreign},
		{"beforeShotId": first, "afterShotId": first},
	} {
		create := map[string]any{"op": "createShot", "sceneId": sceneID, "value": value}
		for key, value := range anchor {
			create[key] = value
		}
		_, err := s.ChangeStoryboard(context.Background(), p.ID, mustJSON(map[string]any{"revision": p.Revision, "operations": []any{
			map[string]any{"op": "updateShot", "shotId": first, "patch": map[string]any{"action": "must roll back"}},
			create,
		}}))
		if err == nil {
			t.Fatal("accepted unavailable, cross-scene or conflicting anchors")
		}
		got, _ := s.Project(context.Background(), p.ID)
		if !reflect.DeepEqual(got, baseline) {
			t.Fatal("rejected insertion left a partial change")
		}
	}
	_, err := s.ChangeStoryboard(context.Background(), p.ID, mustJSON(map[string]any{"revision": beforeRevision, "operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "afterShotId": first, "value": value}}}))
	if err == nil {
		t.Fatal("accepted an insertion based on a stale revision")
	}
	got, _ := s.Project(context.Background(), p.ID)
	if !reflect.DeepEqual(got, baseline) {
		t.Fatal("stale insertion changed the project")
	}
}

func TestP5NaturalEinoBreakdownIndependentReviewActuallyRepairsAndPreserves(t *testing.T) {
	s, m, p := p5Prepared(t)
	ctx := context.Background()
	var e error
	p, e = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	var sceneID, shotID string
	var correct d.ShotValue
	m.generate = func(req ports.Request) (ports.Response, error) {
		calls++
		current, _ := s.Project(ctx, p.ID)
		r := current.LatestRun()
		if !strings.Contains(req.Input, "complete_episode") || !strings.Contains(req.Input, "unused") || !strings.Contains(req.Input, "你自己决定") {
			t.Fatal("incomplete target or candidate material")
		}
		if strings.Contains(req.Input, "风格要求") {
			t.Fatal("style leaked")
		}
		if r.Stage == "storyboardReview" {
			if strings.Contains(req.System, "独立复核") == false {
				t.Fatal("review context missing")
			}
			if len(req.Dialogue) == 0 {
				if strings.Contains(req.Input, "GENERATOR_SELF_ASSESSMENT") {
					t.Fatal("self assessment inherited")
				}
				return p5Response("read_assets", map[string]any{"targets": []d.VisualTarget{{AssetID: "c"}, {AssetID: "c2"}, {AssetID: "loc"}, {AssetID: "prop"}}}), nil
			}
			if len(req.Dialogue) == 2 {
				if strings.Contains(req.CurrentState, "松开手，镜头停在钥匙上") {
					return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "updateShot", "shotId": shotID, "patch": map[string]any{"videoPrompt": map[string]any{"parts": p5Parts(false)}}}}}), nil
				}
				t.Fatal("review did not see defective saved prompt")
			}
			return p5Response("finish_storyboard", map[string]any{}), nil
		}
		switch calls {
		case 1:
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*current.Storyboard.Episode("e1"))}}}), nil
		case 2:
			sceneID = current.Storyboard.Scenes[0].ID
			return p5Response("read_assets", map[string]any{"targets": []d.VisualTarget{{AssetID: "c"}, {AssetID: "c2"}, {AssetID: "loc"}, {AssetID: "prop"}}}), nil
		case 3:
			shot := p5Shot(*current.Storyboard.Episode("e1"))
			shot["videoPrompt"] = map[string]any{"parts": p5Parts(true)}
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createShot", "sceneId": sceneID, "value": shot}}}), nil
		case 4:
			shotID = current.Storyboard.Shots[0].ID
			correct = current.Storyboard.Shots[0].ShotValue
			out := p5Response("finish_storyboard", map[string]any{})
			out.Text = "GENERATOR_SELF_ASSESSMENT: 已经完美，无需改正"
			return out, nil
		}
		t.Fatal("unexpected call", calls, r.Stage)
		return ports.Response{}, errors.New("unexpected")
	}
	// IDs are always read from committed receipts, never invented by the provider.
	if e = s.Execute(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	got, _ := s.Project(ctx, p.ID)
	if got.LatestRun().Status != d.RunCompleted || calls != 7 {
		t.Fatalf("not complete: calls=%d run=%+v", calls, got.LatestRun())
	}
	sh := got.Storyboard.Shots[0]
	if !reflect.DeepEqual(correct, sh.ShotValue) || !strings.Contains(sh.VideoPrompt.Parts[len(sh.VideoPrompt.Parts)-1].Text, "你自己决定") || len(got.Storyboard.Episode("e2").SceneIDs) != 0 {
		t.Fatal("repair lost correct fields or changed another episode")
	}
	for _, c := range got.LatestRun().Calls {
		if c.Response == "" || len(c.Request) == 0 || len(c.Actions) != 1 || c.Actions[0].Result == "" {
			t.Fatal("missing actual evidence")
		}
	}
	p5Record(t, got)
	v, e := s.StoryboardEpisode(got, "e1")
	if e != nil || !v.Shots[0].VideoPrompt.Valid || !strings.Contains(v.Shots[0].VideoPrompt.PlainText, "@林晚") {
		t.Fatal("view missing valid structured content", e)
	}
}

func TestP5ChangesReferencesAndDirectInvalidation(t *testing.T) {
	s, m, p := p5Prepared(t)
	ep := *p.Storyboard.Episode("e1")
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(ep)})
	sceneID := p.Storyboard.Scenes[0].ID
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": sceneID, "value": p5Shot(ep)})
	id := p.Storyboard.Shots[0].ID
	before := d.Clone(p)
	// Invalid image files still fail; valid post-breakdown images are covered by media integration tests.
	if _, e := s.SaveImage(context.Background(), p.ID, p.Revision, d.VisualTarget{AssetID: "unused"}, "file"); e == nil {
		t.Fatal("invalid image file accepted")
	}
	for _, ops := range [][]any{{map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"selectedAssets": []d.VisualTarget{{AssetID: "c"}, {AssetID: "c2"}, {AssetID: "loc"}}}}}, {map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"action": "would change"}}, map[string]any{"op": "updateShot", "shotId": "foreign", "patch": map[string]any{"action": "bad"}}}, {map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"videoPrompt": map[string]any{"parts": []any{map[string]any{"type": "asset", "assetId": "unused"}}}}}}} {
		_, e := s.ChangeStoryboard(context.Background(), p.ID, mustJSON(map[string]any{"revision": p.Revision, "operations": ops}))
		if e == nil {
			t.Fatal("illegal atomic command accepted")
		}
		got, _ := s.Project(context.Background(), p.ID)
		if !reflect.DeepEqual(got, before) {
			t.Fatal("rejection partially saved")
		}
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"action": "用户修订起手位置，完整保留对白"}})
	sh := p.Storyboard.Shot(id)
	if sh.VideoPrompt == nil || len(p.VideoPromptIssues(sh, "")) == 0 || sh.Dialogues[0].Text != "你自己决定。" {
		t.Fatal("direct invalidation lost text or correct fields")
	}
	oldParts := p.Storyboard.Shot(id).VideoPrompt.Parts
	refs := append(append([]d.VisualTarget{}, p.Storyboard.Shot(id).SelectedAssets...), d.VisualTarget{AssetID: "unused"})
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"selectedAssets": refs}})
	if !reflect.DeepEqual(oldParts, p.Storyboard.Shot(id).VideoPrompt.Parts) || len(p.VideoPromptIssues(p.Storyboard.Shot(id), "")) == 0 {
		t.Fatal("adding selection erased or silently refreshed old prompt")
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "deleteScene", "sceneId": sceneID})
	if len(p.Storyboard.Shots) != 0 || m.count != 0 {
		t.Fatal("ordinary edit lost graph deletion or used model")
	}
}

func TestP5PromptPartsReplacementDoesNotReuseOldUnionFields(t *testing.T) {
	s, _, p := p5Prepared(t)
	ep := *p.Storyboard.Episode("e1")
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": ep.ID, "value": p5Scene(ep)})
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Scenes[0].ID, "value": p5Shot(ep)})
	id := p.Storyboard.Shots[0].ID
	original := d.Clone(p)
	parts := append([]d.PromptPart{{Type: "text", Text: "保持原始剧情，"}}, p5Parts(false)...)
	checkParts := func(got, want []d.PromptPart) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatal("replacement did not replace all old parts")
		}
		for i, part := range got {
			// labelAtSave is derived by the domain and is not writable input.
			if part.Type != want[i].Type || part.Text != want[i].Text || part.AssetID != want[i].AssetID || part.LookID != want[i].LookID {
				t.Fatalf("part %d retained old union fields: %#v", i, part)
			}
		}
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"videoPrompt": map[string]any{"parts": parts}}})
	checkParts(p.Storyboard.Shot(id).VideoPrompt.Parts, parts)
	if p.Storyboard.Shot(id).Action != original.Storyboard.Shot(id).Action || !reflect.DeepEqual(p.Storyboard.Shot(id).SelectedAssets, original.Storyboard.Shot(id).SelectedAssets) {
		t.Fatal("prompt edit changed action, selection or asset lock")
	}
	// The reverse shift exercises text -> asset, and a shorter replacement must
	// discard every old element rather than merge optional Look IDs or text.
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"videoPrompt": map[string]any{"parts": p5Parts(false)}}})
	checkParts(p.Storyboard.Shot(id).VideoPrompt.Parts, p5Parts(false))
	before := d.Clone(p)
	_, err := s.ChangeStoryboard(context.Background(), p.ID, mustJSON(map[string]any{"revision": p.Revision, "operations": []any{map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"videoPrompt": map[string]any{"parts": []d.PromptPart{{Type: "asset", AssetID: "unused"}}}}}}}))
	if err == nil {
		t.Fatal("unselected reference accepted")
	}
	after, err := s.Project(context.Background(), p.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected reference changed saved content")
	}
}

func TestP5SelectionCanSwitchFromLookBackToBaseWithoutChangingIdentity(t *testing.T) {
	s, _, p := p5Prepared(t)
	ep := *p.Storyboard.Episode("e1")
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": ep.ID, "value": p5Scene(ep)})
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Scenes[0].ID, "value": p5Shot(ep)})
	id := p.Storyboard.Shots[0].ID
	base := d.Clone(p).Storyboard.Shot(id)
	lookRefs := append([]d.VisualTarget{}, base.SelectedAssets...)
	lookParts := p5Parts(false)
	for i := range lookRefs {
		if lookRefs[i].AssetID == "c" {
			lookRefs[i].LookID = "l"
		}
	}
	for i := range lookParts {
		if lookParts[i].AssetID == "c" {
			lookParts[i].LookID = "l"
		}
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"selectedAssets": lookRefs, "videoPrompt": map[string]any{"parts": lookParts}}})
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"selectedAssets": base.SelectedAssets, "videoPrompt": map[string]any{"parts": p5Parts(false)}}})
	after := p.Storyboard.Shot(id)
	if !reflect.DeepEqual(after.SelectedAssets, base.SelectedAssets) || after.Action != base.Action || !reflect.DeepEqual(after.Dialogues, base.Dialogues) || after.Camera != base.Camera {
		t.Fatal("Look-to-base selection retained a Look or changed unedited fields")
	}
	for _, part := range after.VideoPrompt.Parts {
		if part.AssetID == "c" && part.LookID != "" {
			t.Fatal("base reference retained old Look")
		}
	}
}

func TestP5AutomaticIndexAtomicCreateAndEinoReviewUnicodeCounts(t *testing.T) {
	s, m := p5Service(t)
	files := uploadfiles.New()
	s.Files = files
	ctx := context.Background()
	raw := []byte("说明😀\n上集\n林晚说：你自己决定。\n下集\n陈默点头。")
	f, e := files.Upload(ctx, uploadapp.Input{Filename: "fictional.txt", Data: raw})
	if e != nil {
		t.Fatal(e)
	}
	if m.count != 0 {
		t.Fatal("general upload ran model")
	}
	p, e := s.CreateFromFile(ctx, "无数字上下集", f.ID, 0, CreationChoices{})
	if e != nil {
		t.Fatal(e)
	}
	if p.Storyboard == nil || len(p.Runs) != 1 || p.LatestRun().Status != d.RunRunning || p.Revision != 1 || m.count != 0 {
		t.Fatal("source and first run not atomically saved", p)
	}
	m.generate = func(req ports.Request) (ports.Response, error) {
		current, _ := s.Project(ctx, p.ID)
		r := current.LatestRun()
		if strings.HasSuffix(r.Stage, "Review") {
			if len(req.Dialogue) != 0 {
				t.Fatal("review inherited dialogue")
			}
			return p5Response("finish_episode_index", map[string]any{}), nil
		}
		if m.count == 1 {
			sp := current.ScriptSource.Spans
			if len(sp) != 5 || sp[0].Text != "说明😀" {
				t.Fatal("source changed")
			}
			ranges := []d.SourceRange{p5Range(sp[0].ID, sp[0].Text)}
			a := d.SourceRange{Start: d.SourcePoint{SpanID: sp[1].ID}, End: d.SourcePoint{SpanID: sp[2].ID, Offset: len([]rune(sp[2].Text))}}
			b := d.SourceRange{Start: d.SourcePoint{SpanID: sp[3].ID}, End: d.SourcePoint{SpanID: sp[4].ID, Offset: len([]rune(sp[4].Text))}}
			return p5Response("save_episode_ranges", map[string]any{"operations": []any{map[string]any{"op": "setSegmentation", "segmentation": "episodes"}, map[string]any{"op": "setSupportRanges", "ranges": ranges}, map[string]any{"op": "createEpisode", "value": d.EpisodeValue{Title: "上集", SourceRanges: []d.SourceRange{a}}}, map[string]any{"op": "createEpisode", "value": d.EpisodeValue{Title: "下集", SourceRanges: []d.SourceRange{b}}}}}), nil
		}
		v := s.StoryboardIndex(current)
		if v.EpisodeCount != nil {
			t.Fatal("partial index advertised count")
		}
		return p5Response("finish_episode_index", map[string]any{}), nil
	}
	if e = s.Execute(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Project(ctx, p.ID)
	v := s.StoryboardIndex(p)
	if v.EpisodeCount == nil || *v.EpisodeCount != 2 || !v.Index.Valid || len(p.Assets) != 0 || m.count != 3 {
		t.Fatal("bad index or automatic asset generation", v)
	}
	source, e := s.StoryboardSource(p, p.Storyboard.Index.Episodes[0].ID)
	if e != nil || len(source.Fragments) != 2 || source.Fragments[1].Text != "林晚说：你自己决定。" {
		t.Fatal("incomplete episode source", e)
	}
	if _, e = s.RetryEpisodeIndex(ctx, p.ID, p.Revision); e == nil {
		t.Fatal("completed index retried")
	}
	before := m.count
	_, _ = s.ReadStoryboard(ctx, p.ID)
	if m.count != before {
		t.Fatal("GET duplicated call")
	}
	if len(p.Runs) != 2 || !p.Runs[0].ContinueAssetPreparation || p.LatestRun().Contract != d.CatalogExtractionContract || p.LatestRun().Status != d.RunRunning {
		t.Fatal("index completion did not atomically persist asset continuation")
	}
	// 模拟索引提交后、旧 worker 退出前重启。只恢复已保存的下一 Run。
	m.catalogVisual = true
	m.generate = func(req ports.Request) (ports.Response, error) {
		current, _ := s.Project(ctx, p.ID)
		if req.System == ExtractPrompt+modelSourceInstruction {
			return response(`{"assets":[{"kind":"character","name":"林晚","description":"错误灰衣","sourceIds":["` + current.ScriptSource.Spans[2].ID + `"]}]}`), nil
		}
		if req.System != CorrectPrompt+modelSourceInstruction {
			t.Fatal("index was repeated or wrong continuation", req.System)
		}
		a := current.Assets[0]
		a.Description = "原文未描述衣服"
		raw, _ := json.Marshal(CatalogChangeSet{Assets: catalogInputs([]d.Asset{a})})
		return response(string(raw)), nil
	}
	workers := NewWorkers(s)
	if e = workers.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	if e = workers.WaitUntilIdle(ctx); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Project(ctx, p.ID)
	if len(p.Runs) != 2 || len(p.Runs[0].Calls) != 3 || p.LatestRun().Status != d.RunCompleted || len(p.PendingVisualTargets()) != 0 || p.Assets[0].Description != "原文未描述衣服" {
		t.Fatal("one-start continuation/recovery did not complete corrected assets and prompts", p.LatestRun())
	}
	before = m.count
	if e = workers.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	if e = workers.WaitUntilIdle(ctx); e != nil || m.count != before {
		t.Fatal("terminal recovery resent model", e)
	}
}

func TestNewProjectParsingCancelStopsContinuationAndRetryKeepsIntent(t *testing.T) {
	s, m := p5Service(t)
	files := uploadfiles.New()
	s.Files = files
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f, e := files.Upload(ctx, uploadapp.Input{Filename: "fiction.txt", Data: []byte("林晚在门边。")})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.CreateFromFile(ctx, "免费取消夹具", f.ID, 0, CreationChoices{})
	if e != nil {
		t.Fatal(e)
	}
	p, e = s.Cancel(ctx, p.ID, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	w := NewWorkers(s)
	if e = w.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	if e = w.WaitUntilIdle(ctx); e != nil || m.count != 0 {
		t.Fatal("cancelled index resumed", e)
	}
	p, e = s.RetryEpisodeIndex(ctx, p.ID, p.Revision)
	if e != nil || !p.LatestRun().ContinueAssetPreparation {
		t.Fatal("explicit retry lost continuation intent", e)
	}
	m.generate = func(req ports.Request) (ports.Response, error) {
		if m.count == 1 {
			span := p.ScriptSource.Spans[0]
			return p5Response("save_episode_ranges", map[string]any{"operations": []any{map[string]any{"op": "setSegmentation", "segmentation": "whole"}, map[string]any{"op": "createEpisode", "value": d.EpisodeValue{Title: "整篇", SourceRanges: []d.SourceRange{p5Range(span.ID, span.Text)}}}}}), nil
		}
		return p5Response("finish_episode_index", map[string]any{}), nil
	}
	if e = s.Execute(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Project(ctx, p.ID)
	if len(p.Runs) != 3 || p.LatestRun().Contract != d.CatalogExtractionContract || !p.IndexReady() {
		t.Fatal("retry did not save next run")
	}
	index := d.Clone(p).Storyboard
	p, e = s.Cancel(ctx, p.ID, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	if e = w.WaitUntilIdle(ctx); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Project(ctx, p.ID)
	if m.count != 3 || len(p.Runs) != 3 || p.LatestRun().Status != d.RunCancelled || !reflect.DeepEqual(index, p.Storyboard) {
		t.Fatal("cancelled handoff ran or removed saved index")
	}
}
func TestP5StrictSchemaAndPromptOnlyScope(t *testing.T) {
	var numeric EpisodeRunInput
	if e := DecodeStoryboard([]byte(`{"revision":1e0,"operation":"breakdown"}`), "EpisodeRunInput", &numeric); e != nil || numeric.Revision != 1 {
		t.Fatal("schema integer and DTO disagree", e)
	}
	bad := []string{`{"revision":1,"operation":"fillPrompts","shotIds":["s"]}`, `{"revision":1,"operation":"regeneratePrompts","shotIds":[]}`, `{"revision":1,"operation":"breakdown","approved":true}`, `{"revision":1,"revision":2,"operation":"breakdown"}`, `{"revision":null,"operation":"breakdown"}`}
	for _, raw := range bad {
		var v EpisodeRunInput
		if DecodeStoryboard([]byte(raw), "EpisodeRunInput", &v) == nil {
			t.Fatal("bad HTTP union accepted", raw)
		}
	}
	for _, patch := range []string{`{"videoPrompt":null}`, `{"action":"bad","videoPrompt":{"parts":[{"type":"text","text":"body"}]}}`, `{"videoPrompt":{"parts":[{"type":"asset","assetId":"c","lookId":null}]}}`, `{"videoPrompt":{"parts":[{"type":"asset","assetId":"c","label":"same name"}]}}`} {
		raw := []byte(`{"operations":[{"op":"updateShot","shotId":"s","patch":` + patch + `}]}`)
		var v any
		if DecodeStoryboard(raw, "SavePrompts", &v) == nil {
			t.Fatal("bad prompt schema accepted", patch)
		}
	}
	s, _, p := p5Prepared(t)
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))})
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Scenes[0].ID, "value": p5Shot(*p.Storyboard.Episode("e1"))})
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateShot", "shotId": p.Storyboard.Shots[0].ID, "patch": map[string]any{"videoPrompt": nil}})
	p, e := s.StartEpisode(context.Background(), p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "fillPrompts"})
	if e != nil {
		t.Fatal(e)
	}
	tools := runTaskTools(p.LatestRun())
	for _, tool := range tools {
		if tool.Name == "save_storyboard" && (strings.Contains(string(tool.Parameters), `"action"`) || strings.Contains(string(tool.Parameters), `"selectedAssets"`)) {
			t.Fatal("prompt-only wire schema exposes content writes")
		}
	}
	sh := p.Storyboard.Shots[0]
	old := d.Clone(p)
	op := d.StoryboardOperation{Op: "updateShot", ShotID: sh.ID, Patch: json.RawMessage(`{"action":"越权改剧情"}`)}
	_, e = p.ApplyStoryboard([]d.StoryboardOperation{op}, d.StoryboardWrite{Scope: p.LatestRun().StoryboardScope, Ref: &d.ActionRef{RunID: p.LatestRun().ID}})
	if e == nil || !reflect.DeepEqual(old, p) {
		t.Fatal("scope bypass or partial adoption")
	}
}

func TestP5UnicodeRangesIndexShapesAndCounts(t *testing.T) {
	blank := d.ScriptSource{Spans: []d.SourceSpan{{ID: "a", Text: "甲"}, {ID: "blank", Text: ""}, {ID: "b", Text: "乙"}}}
	fr, e := blank.Fragments([]d.SourceRange{{Start: d.SourcePoint{SpanID: "a"}, End: d.SourcePoint{SpanID: "b", Offset: 1}}})
	if e != nil || len(fr) != 3 || fr[1].SpanID != "blank" || fr[1].Text != "" {
		t.Fatal("source range dropped an empty original paragraph", e)
	}

	s, _, p := p5Prepared(t)
	span := p.ScriptSource.Spans[0]
	r := d.SourceRange{Start: d.SourcePoint{SpanID: span.ID, Offset: 2}, End: d.SourcePoint{SpanID: span.ID, Offset: 4}}
	fragments, e := p.ScriptSource.Fragments([]d.SourceRange{r})
	if e != nil || fragments[0].Text != "😀。" {
		t.Fatal("offset was not Unicode code points", fragments, e)
	}
	for _, numbers := range [][]*int{{nil, nil}, {intp(1), intp(8)}, {intp(3), intp(3)}} {
		q := d.Clone(p)
		for i, n := range numbers {
			q.Storyboard.Index.Episodes[i].EpisodeNumber = n
		}
		view := s.StoryboardIndex(q)
		if view.EpisodeCount == nil || *view.EpisodeCount != 2 {
			t.Fatal("count used max number or deduplicated numbers")
		}
	}
	whole := d.Clone(p)
	mode := "whole"
	whole.Storyboard.Index.Segmentation = &mode
	ep := whole.Storyboard.Index.Episodes[0]
	ep.SourceRanges = []d.SourceRange{{Start: d.SourcePoint{SpanID: "s1"}, End: d.SourcePoint{SpanID: "s2", Offset: len([]rune(whole.ScriptSource.Spans[1].Text))}}}
	whole.Storyboard.Index.Episodes = []d.Episode{ep}
	if whole.ValidateEpisodeIndex(true) != nil || s.StoryboardIndex(whole).EpisodeCount != nil {
		t.Fatal("whole is not an unnumbered work")
	}
	whole.Storyboard.Index.Episodes[0].EpisodeNumber = intp(1)
	if whole.ValidateEpisodeIndex(true) == nil {
		t.Fatal("whole forged episode one")
	}
	for name, mutate := range map[string]func(*d.Project){"gap": func(q *d.Project) { q.Storyboard.Index.Episodes[0].SourceRanges[0].Start.Offset = 1 }, "overlap": func(q *d.Project) {
		q.Storyboard.Index.SupportRanges = append(q.Storyboard.Index.SupportRanges, q.Storyboard.Index.Episodes[0].SourceRanges...)
	}, "foreign span": func(q *d.Project) { q.Storyboard.Index.Episodes[0].SourceRanges[0].End.SpanID = "other-project" }, "offset overflow": func(q *d.Project) { q.Storyboard.Index.Episodes[0].SourceRanges[0].End.Offset = 10000 }} {
		t.Run(name, func(t *testing.T) {
			q := d.Clone(p)
			mutate(&q)
			if q.ValidateEpisodeIndex(true) == nil {
				t.Fatal("invalid index completed")
			}
		})
	}
}
func intp(n int) *int { return &n }

func TestStoryboardEpisodeCountsUseDistinctCharacterIDsInThatEpisode(t *testing.T) {
	s, _, p := p5Prepared(t)
	// 同名角色是不同身份，同一角色的基础和 Look 仍只有一个身份。
	p.Assets[1].Name = p.Assets[0].Name
	p.Assets = append(p.Assets, d.Asset{ID: "offscreen", Kind: "character", Name: "未选角色"})
	p.Storyboard.Index.Episodes[0].SceneIDs = []string{"scene-a", "scene-b"}
	p.Storyboard.Index.Episodes[1].SceneIDs = []string{"scene-c"}
	p.Storyboard.Scenes = []d.Scene{
		{ID: "scene-a", EpisodeID: "e1", ShotIDs: []string{"a", "b"}},
		{ID: "scene-b", EpisodeID: "e1", ShotIDs: []string{"c"}},
		{ID: "scene-c", EpisodeID: "e2", ShotIDs: []string{"d"}},
	}
	p.Storyboard.Shots = []d.Shot{
		{ID: "a", SceneID: "scene-a", ShotValue: d.ShotValue{SelectedAssets: []d.VisualTarget{{AssetID: "c"}, {AssetID: "c", LookID: "l"}, {AssetID: "loc"}, {AssetID: "missing"}}}},
		{ID: "b", SceneID: "scene-a", ShotValue: d.ShotValue{SelectedAssets: []d.VisualTarget{{AssetID: "c"}, {AssetID: "prop"}}}},
		{ID: "c", SceneID: "scene-b", ShotValue: d.ShotValue{SelectedAssets: []d.VisualTarget{{AssetID: "c2"}}}},
		{ID: "d", SceneID: "scene-c", ShotValue: d.ShotValue{SelectedAssets: []d.VisualTarget{{AssetID: "offscreen"}}}},
	}
	view := s.StoryboardIndex(p)
	first, second := view.Index.Episodes[0], view.Index.Episodes[1]
	if first.CharacterCount != 2 || first.SceneCount != 2 || first.ShotCount != 3 {
		t.Fatal("first episode counts", first)
	}
	if second.CharacterCount != 1 || second.SceneCount != 1 || second.ShotCount != 1 {
		t.Fatal("other episode leaked into counts", second)
	}
}

func TestP5CapacityRejectionRollsBackSceneAndFirstAssetLock(t *testing.T) {
	s, m, p := p5Prepared(t)
	ctx := context.Background()
	var e error
	p, e = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
	if e != nil {
		t.Fatal(e)
	}
	m.generate = func(req ports.Request) (ports.Response, error) {
		if m.count == 1 {
			m.window = len(req.System) + len(req.Input) + len(mustJSON(req.Tools)) + 12000
			m.output = 1000
			value := p5Scene(*p.Storyboard.Episode("e1"))
			value["summary"] = strings.Repeat("完整但超过容量的说明", 5000)
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": "e1", "value": value}}}), nil
		}
		return ports.Response{Raw: json.RawMessage(`{"stopped":true}`), FinishReason: "stop", Usage: billing.Usage{Known: true}}, nil
	}
	_ = s.Execute(ctx, p.ID)
	got, _ := s.Project(ctx, p.ID)
	if len(got.Storyboard.Scenes) != 0 || got.LatestRun().Calls[0].Actions[0].Status != "rejected" || !strings.Contains(got.LatestRun().Calls[0].Actions[0].Result, "CAPACITY_EXCEEDED") {
		t.Fatal("capacity rejection left partial scene/lock", got.LatestRun())
	}
}
func TestP5PendingUnknownAndCancelledLateResponsesDoNotResendOrAdopt(t *testing.T) {
	for _, status := range []d.CallStatus{"pending", "unknown"} {
		t.Run(string(status), func(t *testing.T) {
			s, m, p := p5Prepared(t)
			ctx := context.Background()
			p, e := s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
			if e != nil {
				t.Fatal(e)
			}
			p.LatestRun().Calls = []d.Call{{ID: "unknown", Stage: "storyboard", Status: status, ReservedMicroCNY: 123}}
			p, e = s.commit(ctx, p, p.Revision)
			if e != nil {
				t.Fatal(e)
			}
			fresh := *s
			fresh.Agent = einoagent.New()
			if e = fresh.Execute(ctx, p.ID); e != nil {
				t.Fatal(e)
			}
			p, _ = s.Project(ctx, p.ID)
			if m.count != 0 || p.LatestRun().ErrorCode != "MODEL_RESULT_UNKNOWN" || p.UsedMicroCNY() != 123 || s.StoryboardIndex(p).Access.CanStart {
				t.Fatal("unknown response resent or reservation lost")
			}
			if _, e = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"}); e == nil {
				t.Fatal("unknown explicitly resent")
			}
		})
	}
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-output-cancel", true: "partial-cancel"}[partial], func(t *testing.T) {
			s, m, p := p5Prepared(t)
			ctx := context.Background()
			if partial {
				p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))})
			}
			p, e := s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
			if e != nil {
				t.Fatal(e)
			}
			started, release := make(chan struct{}), make(chan struct{})
			m.generate = func(req ports.Request) (ports.Response, error) {
				close(started)
				<-release
				return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))}}}), nil
			}
			done := make(chan error, 1)
			go func() { done <- s.Execute(ctx, p.ID) }()
			<-started
			current, _ := s.Project(ctx, p.ID)
			_, e = s.Cancel(ctx, p.ID, current.Revision)
			if e != nil {
				t.Fatal(e)
			}
			close(release)
			<-done
			got, _ := s.Project(ctx, p.ID)
			if got.LatestRun().Status != d.RunCancelled || len(got.Storyboard.Scenes) != len(p.Storyboard.Scenes) || got.LatestRun().Calls[0].Response == "" {
				t.Fatal("late response adopted or evidence lost")
			}
		})
	}
}
func TestP5ReceivedResponseRecoveryPreservesIDsAndNeverRepeatsAdoption(t *testing.T) {
	s, m, p := p5Prepared(t)
	ctx := context.Background()
	var e error
	p, e = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "breakdown"})
	if e != nil {
		t.Fatal(e)
	}
	m.generate = func(req ports.Request) (ports.Response, error) {
		if m.count == 1 {
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))}}}), nil
		}
		return ports.Response{Raw: json.RawMessage(`{"stop":true}`), FinishReason: "stop", Usage: billing.Usage{Known: true}}, nil
	}
	_, e = s.generate(ctx, p.ID, p.LatestRun().ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	pending, _ := s.Project(ctx, p.ID)
	if pending.LatestRun().Calls[0].Status != "received" {
		t.Fatal("response was adopted before restart")
	}
	fresh := *s
	fresh.Agent = einoagent.New()
	_ = fresh.Execute(ctx, p.ID)
	got, _ := s.Project(ctx, p.ID)
	if len(got.Storyboard.Scenes) != 1 || got.LatestRun().Calls[0].Actions[0].Status != "applied" || m.count != 2 {
		t.Fatal("recovery lost/adopted twice")
	}
	id := got.Storyboard.Scenes[0].ID
	_ = fresh.Execute(ctx, p.ID)
	again, _ := s.Project(ctx, p.ID)
	if len(again.Storyboard.Scenes) != 1 || again.Storyboard.Scenes[0].ID != id || m.count != 2 {
		t.Fatal("terminal recovery repeated output")
	}
}

func TestP5RegenerateFrozenPromptEinoRejectsOverreachAndPreservesOtherShots(t *testing.T) {
	s, m, p := p5Prepared(t)
	ctx := context.Background()
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))})
	scID := p.Storyboard.Scenes[0].ID
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": scID, "value": p5Shot(*p.Storyboard.Episode("e1"))}, map[string]any{"op": "createShot", "sceneId": scID, "value": p5Shot(*p.Storyboard.Episode("e1"))})
	first, second := p.Storyboard.Shots[0], p.Storyboard.Shots[1]
	beforeAssets := p.Assets
	var e error
	p, e = s.StartEpisode(ctx, p.ID, "e1", EpisodeRunInput{Revision: p.Revision, Operation: "regeneratePrompts", ShotIDs: []string{first.ID}})
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	refs := first.SelectedAssets
	m.generate = func(req ports.Request) (ports.Response, error) {
		n++
		current, _ := s.Project(ctx, p.ID)
		if strings.HasSuffix(current.LatestRun().Stage, "Review") {
			if len(req.Dialogue) == 0 {
				return p5Response("read_assets", map[string]any{"targets": refs}), nil
			}
			return p5Response("finish_storyboard", map[string]any{}), nil
		}
		if n == 1 {
			return p5Response("read_assets", map[string]any{"targets": refs}), nil
		}
		if n == 2 {
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "updateShot", "shotId": first.ID, "patch": map[string]any{"action": "越权改动作", "videoPrompt": map[string]any{"parts": p5Parts(false)}}}}}), nil
		}
		if n == 3 {
			if !strings.Contains(req.Dialogue[len(req.Dialogue)-1].Content, `"ok":false`) {
				t.Fatal("schema rejection not returned")
			}
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "updateShot", "shotId": second.ID, "patch": map[string]any{"videoPrompt": map[string]any{"parts": p5Parts(false)}}}}}), nil
		}
		if n == 4 {
			if !strings.Contains(req.Dialogue[len(req.Dialogue)-1].Content, "STORYBOARD_SCOPE_VIOLATION") {
				t.Fatal("target rejection not returned")
			}
			parts := p5Parts(false)
			parts[len(parts)-1].Text += "摄影保留中景。"
			return p5Response("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "updateShot", "shotId": first.ID, "patch": map[string]any{"videoPrompt": map[string]any{"parts": parts}}}}}), nil
		}
		return p5Response("finish_storyboard", map[string]any{}), nil
	}
	if e = s.Execute(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	got, _ := s.Project(ctx, p.ID)
	if got.LatestRun().Status != d.RunCompleted || !reflect.DeepEqual(second, *got.Storyboard.Shot(second.ID)) || !reflect.DeepEqual(first.ShotValue, got.Storyboard.Shot(first.ID).ShotValue) || first.Origin != got.Storyboard.Shot(first.ID).Origin || !reflect.DeepEqual(first.ActionRef, got.Storyboard.Shot(first.ID).ActionRef) || !reflect.DeepEqual(beforeAssets, got.Assets) {
		t.Fatal("prompt task damaged correct inputs or other shots")
	}
	p5Record(t, got)
	if len(got.VideoPromptIssues(got.Storyboard.Shot(first.ID), "")) > 0 || got.LatestRun().Calls[1].Actions[0].Status != "rejected" || got.LatestRun().Calls[2].Actions[0].Status != "rejected" {
		t.Fatal("rejection/adoption evidence incorrect")
	}
}
func TestP5FinalGraphOrderingRangeInvalidationNoOpAndCAS(t *testing.T) {
	s, _, p := p5Prepared(t)
	ctx := context.Background()
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(*p.Storyboard.Episode("e1"))})
	sc := p.Storyboard.Scenes[0]
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": sc.ID, "value": p5Shot(*p.Storyboard.Episode("e1"))}, map[string]any{"op": "createShot", "sceneId": sc.ID, "value": p5Shot(*p.Storyboard.Episode("e1"))})
	a, b := p.Storyboard.Shots[0].ID, p.Storyboard.Shots[1].ID
	// The final permutation is validated after all edits, even if the sort operation comes before deletion.
	p, _ = p5Change(t, s, p, map[string]any{"op": "setShotOrder", "sceneId": sc.ID, "shotIds": []string{b}}, map[string]any{"op": "deleteShot", "shotId": a})
	if !reflect.DeepEqual(p.Storyboard.Scene(sc.ID).ShotIDs, []string{b}) {
		t.Fatal("final graph sort not adopted")
	}
	before := p.Revision
	p, receipt := p5Change(t, s, p, map[string]any{"op": "updateScene", "sceneId": sc.ID, "patch": map[string]any{"title": sc.Title}})
	if p.Revision != before || len(receipt.Changes) != 0 {
		t.Fatal("identical user command committed")
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateScene", "sceneId": sc.ID, "patch": map[string]any{"title": "只改标题"}})
	if len(p.VideoPromptIssues(p.Storyboard.Shot(b), "")) != 0 {
		t.Fatal("display title invalidated prompt")
	}
	ep := *p.Storyboard.Episode("e1")
	ranges := append([]d.SourceRange{}, ep.SourceRanges...)
	ranges[0].Start.Offset = 1
	support := []d.SourceRange{{Start: d.SourcePoint{SpanID: "s1"}, End: d.SourcePoint{SpanID: "s1", Offset: 1}}}
	beforeShot := *p.Storyboard.Shot(b)
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateEpisode", "episodeId": "e1", "patch": map[string]any{"sourceRanges": ranges}}, map[string]any{"op": "setSupportRanges", "ranges": support})
	if !reflect.DeepEqual(beforeShot, *p.Storyboard.Shot(b)) || len(p.VideoPromptIssues(p.Storyboard.Shot(b), "")) == 0 {
		t.Fatal("range revision erased old content or refreshed its basis")
	}
	rev := p.Revision
	results := make(chan error, 2)
	for _, title := range []string{"并发A", "并发B"} {
		go func(title string) {
			_, e := s.ChangeStoryboard(ctx, p.ID, mustJSON(map[string]any{"revision": rev, "operations": []any{map[string]any{"op": "updateEpisode", "episodeId": "e1", "patch": map[string]any{"title": title}}}}))
			results <- e
		}(title)
	}
	wins, conflicts := 0, 0
	for range 2 {
		e := <-results
		if e == nil {
			wins++
		} else if errors.Is(e, fault.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("CAS did not select one winner", wins, conflicts)
	}
}
func TestP5IndexExplicitRetryPreservesSourceAndStableIDs(t *testing.T) {
	s, m, p := p5Prepared(t)
	ctx := context.Background()
	p.Runs = p.Runs[:1]
	p.LatestRun().Status = d.RunFailed
	p.LatestRun().Error = "明确失败"
	p, e := s.commit(ctx, p, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{p.Storyboard.Index.Episodes[0].ID, p.Storyboard.Index.Episodes[1].ID}
	source := p.ScriptSource
	p, e = s.RetryEpisodeIndex(ctx, p.ID, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	m.generate = func(req ports.Request) (ports.Response, error) {
		if m.count == 1 {
			return p5Response("save_episode_ranges", map[string]any{"operations": []any{map[string]any{"op": "updateEpisode", "episodeId": ids[0], "patch": map[string]any{"title": "上集"}}}}), nil
		}
		return p5Response("finish_episode_index", map[string]any{}), nil
	}
	if e = s.Execute(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Project(ctx, p.ID)
	if len(p.Runs) != 2 || p.Runs[0].Status != d.RunFailed || p.LatestRun().Status != d.RunCompleted || p.Storyboard.Index.Episodes[0].ID != ids[0] || p.Storyboard.Index.Episodes[1].ID != ids[1] || !reflect.DeepEqual(source, p.ScriptSource) {
		t.Fatal("retry rebuilt identity or changed old run/source")
	}
}

func p5Record(t *testing.T, p d.Project) {
	t.Helper()
	if dir := os.Getenv("DRAMA_TEST_P5_EVIDENCE_DIR"); dir != "" {
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		raw, e := json.MarshalIndent(p, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		name := filepath.Join(dir, t.Name()+"-"+p.LatestRun().ID+".json")
		if e = os.MkdirAll(filepath.Dir(name), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(name, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
func TestP5SchemaMatchesApprovedContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/contracts/storyboard-schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(storyboardSchemaJSON, &b) != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("approved model schema differs")
	}
}

func TestP5NewProjectAutomaticIndexAcrossAllImportFormats(t *testing.T) {
	text := "上集\n林晚说：你自己决定。\n下集\n陈默点头。"
	var docx bytes.Buffer
	writer := zip.NewWriter(&docx)
	part, e := writer.Create("word/document.xml")
	if e != nil {
		t.Fatal(e)
	}
	_, e = part.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>上集</w:t></w:r></w:p><w:p><w:r><w:t>林晚说：你自己决定。</w:t></w:r></w:p><w:p><w:r><w:t>下集</w:t></w:r></w:p><w:p><w:r><w:t>陈默点头。</w:t></w:r></w:p></w:body></w:document>`))
	if e != nil {
		t.Fatal(e)
	}
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	for _, f := range []struct {
		Name string
		Raw  []byte
	}{{"fictional.txt", []byte(text)}, {"fictional.md", []byte(text)}, {"fictional.docx", docx.Bytes()}} {
		t.Run(f.Name, func(t *testing.T) {
			s, m := p5Service(t)
			files := uploadfiles.New()
			s.Files = files
			ctx := context.Background()
			file, e := files.Upload(ctx, uploadapp.Input{Filename: f.Name, Data: f.Raw})
			if e != nil {
				t.Fatal(e)
			}
			p, e := s.CreateFromFile(ctx, "三格式免费夹具", file.ID, 0, CreationChoices{})
			if e != nil {
				t.Fatal(e)
			}
			if m.count != 0 || len(p.Runs) != 1 || p.Revision != 1 {
				t.Fatal("create did not atomically adopt source and run")
			}
			m.generate = func(req ports.Request) (ports.Response, error) {
				if m.count == 1 {
					sp := p.ScriptSource.Spans
					if len(sp) != 4 {
						t.Fatal("incomplete format source")
					}
					first := d.SourceRange{Start: d.SourcePoint{SpanID: sp[0].ID}, End: d.SourcePoint{SpanID: sp[1].ID, Offset: len([]rune(sp[1].Text))}}
					second := d.SourceRange{Start: d.SourcePoint{SpanID: sp[2].ID}, End: d.SourcePoint{SpanID: sp[3].ID, Offset: len([]rune(sp[3].Text))}}
					return p5Response("save_episode_ranges", map[string]any{"operations": []any{map[string]any{"op": "setSegmentation", "segmentation": "episodes"}, map[string]any{"op": "createEpisode", "value": d.EpisodeValue{Title: "上集", SourceRanges: []d.SourceRange{first}}}, map[string]any{"op": "createEpisode", "value": d.EpisodeValue{Title: "下集", SourceRanges: []d.SourceRange{second}}}}}), nil
				}
				return p5Response("finish_episode_index", map[string]any{}), nil
			}
			if e = s.Execute(ctx, p.ID); e != nil {
				t.Fatal(e)
			}
			p, _ = s.Project(ctx, p.ID)
			view := s.StoryboardIndex(p)
			_, original, e := files.ReadOriginal(ctx, p.SourceFileID)
			if e != nil || !bytes.Equal(original, f.Raw) || view.EpisodeCount == nil || *view.EpisodeCount != 2 || len(p.Assets) != 0 {
				t.Fatal("format source/index adoption incorrect", e)
			}
		})
	}
}

func TestStoryboardEpisodeSkipsDanglingReferencesWithoutRenderingPanic(t *testing.T) {
	// ApplyStoryboard 维护 SceneIDs/Shots 引用一致；该防御只针对异常数据，
	// 与目录视图的跳过口径一致，不能把悬挂引用渲染成 panic。
	p := d.Project{ID: "p", Revision: 1}
	p.Storyboard = d.NewStoryboard(1)
	p.Storyboard.Index.Episodes = []d.Episode{{EpisodeValue: d.EpisodeValue{Title: "唯一集"}, ID: "e1", SceneIDs: []string{"gone", "real"}}}
	p.Storyboard.Scenes = []d.Scene{{ID: "real", EpisodeID: "e1", ShotIDs: []string{"missing-shot"}}}
	v, err := (&Service{}).StoryboardEpisode(p, "e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Scenes) != 1 || v.Scenes[0].ID != "real" || len(v.Shots) != 0 {
		t.Fatal("dangling references must be skipped, not panic", v.Scenes, v.Shots)
	}
}

// 拆解不是资产的永久锁。普通编辑走同一 CAS，旧镜头、模型原答与费用保持，
// 上游资料改变或删除通过真实引用/依据计算失效，不自动改写用户剧情。
func TestAssetsRemainEditableAfterBreakdownAndDirectDependenciesInvalidate(t *testing.T) {
	ctx := context.Background()
	s, m, p := p5Prepared(t)
	ep := *p.Storyboard.Episode("e1")
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(ep)})
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": p.Storyboard.Scenes[0].ID, "value": p5Shot(ep)})
	id := p.Storyboard.Shots[0].ID
	before := d.Clone(p)
	asset := p.Asset("c")
	input := d.AssetInput{Kind: asset.Kind, Name: asset.Name, Aliases: []string{}, Description: asset.Description + "，用户调整衣领", Character: &d.Character{}}
	var err error
	p, err = s.SaveAsset(ctx, p.ID, p.Revision, "c", input)
	if err != nil {
		t.Fatal("post-breakdown edit rejected", err)
	}
	if !reflect.DeepEqual(p.Storyboard, before.Storyboard) || !reflect.DeepEqual(p.Runs, before.Runs) || len(p.VideoPromptIssues(p.Storyboard.Shot(id), "")) == 0 || m.count != 0 {
		t.Fatal("edit rewrote content/evidence, failed invalidation, or called model")
	}
	if _, err = s.SaveAsset(ctx, p.ID, before.Revision, "c", input); err == nil {
		t.Fatal("stale revision accepted")
	}
	p, err = s.SaveVisualPrompt(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "c"}, "用户修订的完整图片提示词")
	if err != nil {
		t.Fatal("post-breakdown prompt save rejected", err)
	}
	p, err = s.SaveLook(ctx, p.ID, p.Revision, "c", "l", d.LookInput{Name: "新雨衣", Description: "用户修订的雨衣"})
	if err != nil {
		t.Fatal("post-breakdown Look edit rejected", err)
	}
	p, err = s.DeleteLook(ctx, p.ID, p.Revision, "c", "l")
	if err != nil {
		t.Fatal("post-breakdown Look delete rejected", err)
	}
	p, err = s.SaveAsset(ctx, p.ID, p.Revision, "", d.AssetInput{Kind: "prop", Name: "新资产", Aliases: []string{}})
	if err != nil {
		t.Fatal("post-breakdown create rejected", err)
	}
	p, err = s.DeleteAsset(ctx, p.ID, p.Revision, "prop")
	if err != nil {
		t.Fatal("referenced asset deletion rejected", err)
	}
	sh := p.Storyboard.Shot(id)
	missing := false
	for _, issue := range p.VideoPromptIssues(sh, "") {
		if issue.Code == "ASSET_MISSING" && issue.AssetID == "prop" {
			missing = true
		}
	}
	if !missing || !reflect.DeepEqual(sh, before.Storyboard.Shot(id)) || !reflect.DeepEqual(p.Runs, before.Runs) || m.count != 0 {
		t.Fatal("deletion erased text/evidence or concealed missing reference")
	}
}

func TestLegacyAssetLockIsIgnoredWithoutRewritingHistory(t *testing.T) {
	_, _, p := p5Prepared(t)
	var legacy map[string]any
	if err := json.Unmarshal(mustJSON(p), &legacy); err != nil {
		t.Fatal(err)
	}
	legacy["storyboard"].(map[string]any)["assetsLocked"] = true
	var restored d.Project
	if err := json.Unmarshal(mustJSON(legacy), &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.UpdateAsset("unused", d.AssetInput{Kind: "prop", Name: "用户正常修订", Aliases: []string{}}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(mustJSON(restored), []byte(`"assetsLocked"`)) || !reflect.DeepEqual(restored.Runs, p.Runs) {
		t.Fatal("legacy lock persisted or historical calls changed")
	}
}
