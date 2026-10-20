// Explicit isolated P5 browser harness. Uses the real HTTP/application/domain and
// Eino executor; only storage, files and model ports are free local test doubles.
// Never reads app.local.yaml, model credentials, production PG or S3.
package main

import (
	"context"
	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/adapter/stylecatalog"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/uploadfiles"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type store struct {
	mu       sync.Mutex
	projects map[string]d.Project
	locks    map[string]bool
	path     string
}

func (s *store) flush() error {
	raw, e := json.MarshalIndent(s.projects, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(s.path, raw, 0600)
}
func (s *store) Create(ctx context.Context, p d.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[p.ID]; ok {
		return fault.ErrConflict
	}
	s.projects[p.ID] = d.Clone(p)
	return s.flush()
}
func (s *store) Snapshot(ctx context.Context, id string) (d.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return p, fault.ErrNotFound
	}
	return d.Clone(p), ctx.Err()
}
func (s *store) Commit(ctx context.Context, p d.Project, expected uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.projects[p.ID].Revision != expected {
		return fault.ErrConflict
	}
	s.projects[p.ID] = d.Clone(p)
	return s.flush()
}
func (s *store) Acquire(ctx context.Context, id string) (ports.Lease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks[id] {
		return ports.Lease{}, false, nil
	}
	s.locks[id] = true
	return ports.Lease{Context: ctx, Release: func() { s.mu.Lock(); delete(s.locks, id); s.mu.Unlock() }}, true, ctx.Err()
}
func (s *store) ListSummaries(ctx context.Context) ([]ports.ProjectSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []ports.ProjectSummary{}
	for _, p := range s.projects {
		out = append(out, ports.ProjectSummary{ID: p.ID, Title: p.Title, Filename: p.ScriptSource.Filename, Revision: p.Revision})
	}
	return out, ctx.Err()
}
func (s *store) RunningProjectIDs(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for _, p := range s.projects {
		if p.Active() {
			out = append(out, p.ID)
		}
	}
	return out, ctx.Err()
}
func (s *store) ModelReferences(ctx context.Context) ([]ports.ProjectModelReference, error) {
	return []ports.ProjectModelReference{}, ctx.Err()
}

type model struct{ count atomic.Int64 }

func (m *model) Capacity() (int, int) { return 1000000, 100000 }
func (m *model) ContainsToolReceipt(raw json.RawMessage, a d.Action, _ d.Call) bool {
	var r ports.Request
	if json.Unmarshal(raw, &r) != nil {
		return false
	}
	for _, v := range r.Dialogue {
		if v.Role == "tool" && v.ToolCallID == a.ID && v.Content == a.Result {
			return true
		}
	}
	return false
}
func (m *model) Generate(ctx context.Context, r ports.Request, observe func(json.RawMessage) error) (ports.Response, error) {
	if len(r.Dialogue) > 64 {
		return ports.Response{}, errors.New("free fixture exceeded 32 feedback rounds")
	}
	raw, _ := json.Marshal(r)
	if e := observe(raw); e != nil {
		return ports.Response{}, e
	}
	// Slow enough to exercise running and cancellation without any external call.
	select {
	case <-ctx.Done():
		return ports.Response{}, ctx.Err()
	case <-time.After(180 * time.Millisecond):
	}
	var material struct {
		Source       []d.SourceSpan    `json:"source"`
		TaskScope    d.StoryboardScope `json:"taskScope"`
		CurrentIndex d.EpisodeIndex    `json:"currentIndex"`
		TargetSource struct {
			EpisodeID    string          `json:"episodeId"`
			SourceRanges []d.SourceRange `json:"sourceRanges"`
		} `json:"targetSource"`
		Current app.EpisodeView `json:"currentStoryboard"`
	}
	if e := json.Unmarshal([]byte(r.Input), &material); e != nil {
		return ports.Response{}, e
	}
	respond := func(name string, value any) (ports.Response, error) {
		args, _ := json.Marshal(value)
		return ports.Response{Raw: json.RawMessage(`{"isolatedFreeModel":true}`), FinishReason: "tool_calls", Usage: billing.Usage{Known: true}, Actions: []d.Action{{ID: fmt.Sprint("free_", m.count.Add(1)), Name: name, Arguments: string(args), Status: "pending"}}}, nil
	}
	if material.TaskScope.Operation == "index" {
		if len(material.CurrentIndex.Episodes) == 0 {
			ranges := []d.SourceRange{}
			for _, s := range material.Source {
				ranges = append(ranges, d.SourceRange{Start: d.SourcePoint{SpanID: s.ID}, End: d.SourcePoint{SpanID: s.ID, Offset: len([]rune(s.Text))}})
			}
			return respond("save_episode_ranges", map[string]any{"operations": []any{map[string]any{"op": "setSegmentation", "segmentation": "whole"}, map[string]any{"op": "createEpisode", "value": map[string]any{"episodeNumber": nil, "title": "免费夹具整篇", "sourceRanges": ranges}}}})
		}
		return respond("finish_episode_index", map[string]any{})
	}
	read, saved := false, false
	for _, message := range r.Dialogue {
		for _, a := range message.Actions {
			if a.Name == "read_assets" {
				read = true
			}
			if a.Name == "save_storyboard" {
				saved = true
			}
		}
	}
	refs := []d.VisualTarget{{AssetID: "c", LookID: "rain"}, {AssetID: "c2"}, {AssetID: "loc"}, {AssetID: "key"}}
	if !read {
		return respond("read_assets", map[string]any{"targets": refs})
	}
	if len(material.Current.Scenes) == 0 {
		return respond("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createScene", "episodeId": material.TargetSource.EpisodeID, "value": map[string]any{"title": "客厅 · 决定", "sourceRanges": material.TargetSource.SourceRanges, "locationAssetId": "loc", "time": "傍晚", "summary": "林晚放下钥匙，并说出完整对白。"}}}})
	}
	parts := []d.PromptPart{{Type: "asset", AssetID: "loc"}, {Type: "text", Text: "内，"}, {Type: "asset", AssetID: "c", LookID: "rain"}, {Type: "text", Text: "站在桌边，把"}, {Type: "asset", AssetID: "key"}, {Type: "text", Text: "放到桌上。\n镜头随"}, {Type: "asset", AssetID: "c", LookID: "rain"}, {Type: "text", Text: "的手缓慢下移，她对"}, {Type: "asset", AssetID: "c2"}, {Type: "text", Text: "说：你自己决定。镜头停在钥匙与尚未伸出的手之间。"}}
	if len(material.Current.Shots) == 0 {
		return respond("save_storyboard", map[string]any{"operations": []any{map[string]any{"op": "createShot", "sceneId": material.Current.Scenes[0].ID, "value": map[string]any{"sourceRanges": material.TargetSource.SourceRanges, "action": "林晚将铜钥匙放在桌上，松手并停顿。", "dialogues": []any{map[string]any{"speakerAssetId": "c", "text": "你自己决定。"}}, "camera": d.Camera{Size: "中景", Angle: "平视", Movement: "随手下移"}, "durationSeconds": nil, "designNotes": "停在钥匙与尚未伸出的手之间。", "selectedAssets": refs, "videoPrompt": map[string]any{"parts": parts}}}}})
	}
	if !saved && material.TaskScope.Operation != "breakdown" && !strings.HasPrefix(r.System, "这是独立复核上下文") {
		ops := []any{}
		for _, id := range material.TaskScope.ShotIDs {
			ops = append(ops, map[string]any{"op": "updateShot", "shotId": id, "patch": map[string]any{"videoPrompt": map[string]any{"parts": parts}}})
		}
		return respond("save_storyboard", map[string]any{"operations": ops})
	}
	return respond("finish_storyboard", map[string]any{})
}
func seed(s *app.Service) error {
	text1, text2 := "上集 重逢🌧️。林晚将铜钥匙放在桌上，对另一位同名林晚说：你自己决定。", "下集 归途。林晚站在车站。"
	source := d.ScriptSource{Filename: "isolated-free.txt", Version: 1, Spans: []d.SourceSpan{{ID: "span_1", Text: text1}, {ID: "span_2", Text: text2}}}
	p := d.Project{ID: "p5_real_free", Title: "真实 Go 接口 · 免费隔离数据", Revision: 1, ScriptSource: source, ProjectSettings: &d.ProjectSettings{AspectRatio: "16:9", LLMModel: "free-model"}, Assets: []d.Asset{{ID: "c", Name: "林晚", Kind: "character", Description: "工程师，深灰外套", Looks: []d.Look{{ID: "rain", Name: "雨衣", Description: "深灰雨衣"}}}, {ID: "c2", Name: "林晚", Kind: "character", Description: "医生，同名的另一人"}, {ID: "loc", Name: "客厅", Kind: "location", Description: "窗边有一张桌子"}, {ID: "key", Name: "铜钥匙", Kind: "prop", Description: "单枚铜钥匙"}, {ID: "unused", Name: "未选怀表", Kind: "prop", Description: "验证全项目只读"}}, Storyboard: d.NewStoryboard(1), Runs: []d.Run{{ID: "index", Contract: d.EpisodeIndexContract, SourceVersion: 1, Status: d.RunCompleted}, {ID: "catalog", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: d.RunCompleted}}}
	mode := "episodes"
	p.Storyboard.Index.Segmentation = &mode
	for i, span := range source.Spans {
		p.Storyboard.Index.Episodes = append(p.Storyboard.Index.Episodes, d.Episode{ID: []string{"e1", "e2"}[i], EpisodeValue: d.EpisodeValue{Title: []string{"上集 重逢", "下集 归途"}[i], SourceRanges: []d.SourceRange{{Start: d.SourcePoint{SpanID: span.ID}, End: d.SourcePoint{SpanID: span.ID, Offset: len([]rune(span.Text))}}}}, SceneIDs: []string{}, Origin: "model"})
	}
	for _, a := range p.Assets {
		targets := []d.VisualTarget{{AssetID: a.ID}}
		for _, l := range a.Looks {
			targets = append(targets, d.VisualTarget{AssetID: a.ID, LookID: l.ID})
		}
		for _, t := range targets {
			v := d.VisualPrompt{VisualPromptInput: d.VisualPromptInput{Target: t, Text: "免费夹具完整外观"}, Origin: "user", SourceVersion: 1}
			if t.LookID != "" {
				v.BasePrompt = &d.ActionRef{}
			}
			p.VisualPrompts = append(p.VisualPrompts, v)
		}
	}
	return s.Store.Create(context.Background(), p)
}
func main() {
	check := flag.String("check", "", "read-only command validation file")
	state := flag.String("state", "", "explicit /private/tmp state JSON")
	demo := flag.String("demo", "", "compiled demo directory")
	flag.Parse()
	if !strings.HasPrefix(*state, "/private/tmp/") || !filepath.IsAbs(*demo) {
		panic("isolated temp state and absolute demo path required")
	}
	if e := os.MkdirAll(filepath.Dir(*state), 0700); e != nil {
		panic(e)
	}
	st := &store{projects: map[string]d.Project{}, locks: map[string]bool{}, path: *state}
	if raw, e := os.ReadFile(*state); e == nil {
		if e = json.Unmarshal(raw, &st.projects); e != nil {
			panic(e)
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		panic(e)
	}
	styles, e := stylecatalog.LoadStyles(*demo)
	if e != nil {
		panic(e)
	}
	if *check != "" {
		raw, e := os.ReadFile(*check)
		if e != nil {
			panic(e)
		}
		var in app.StoryboardChangeInput
		e = app.DecodeStoryboard(raw, "ChangeInput", &in)
		fmt.Println("schema", e)
		if e == nil {
			p := st.projects["p5_real_free"]
			_, e = p.ApplyStoryboard(in.Operations, d.StoryboardWrite{})
			fmt.Println("domain", e)
			probe := d.Clone(p).Storyboard.Shots[0].VideoPrompt
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(in.Operations[0].Patch, &fields)
			_ = json.Unmarshal(fields["videoPrompt"], &probe)
			for i, part := range probe.Parts {
				if part.Type == "asset" && part.Text != "" {
					fmt.Println("reused union field", i, "type", part.Type, "text", part.Text, "assetId", part.AssetID)
				}
			}
		}
		return
	}
	m := &model{}
	files := uploadfiles.New()
	s := &app.Service{Styles: styles, Store: st, Importer: scriptimport.Importer{}, Agent: einoagent.New(), Files: files, Authorize: func(context.Context, d.Project, d.Run) (int64, error) { return 1000000, nil }, Models: &modelregistry.Registry{Models: map[string]ports.Model{"free-model": m}, DefaultModel: "free-model", ModelPrices: map[string]billing.TokenPricing{}, OptionsValue: ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "free-model", Name: "隔离免费模型"}}}}}
	if len(st.projects) == 0 {
		if e := seed(s); e != nil {
			panic(e)
		}
	}
	h, handler := httpapi.New(s, nil, *demo, 1000000)
	h.Files = files
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		panic(e)
	}
	fmt.Printf("{\"url\":\"http://%s/demo/app/#/playlet/p5_real_free/production\",\"state\":%q,\"model\":\"local free fixture\"}\n", listener.Addr(), *state)
	if e = http.Serve(listener, handler); e != nil {
		panic(e)
	}
}
