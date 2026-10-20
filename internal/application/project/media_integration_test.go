package project

import (
	"bytes"
	"context"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/imagethumbnail"
	"drama-agent/internal/adapter/mockmedia"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/adapter/taskruntime"
	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	uploadapp "drama-agent/internal/application/upload"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
)

type mediaFreePrices struct{}

func (mediaFreePrices) CreditPrice(context.Context, string) (c.PriceRule, uint64, string, error) {
	return c.PriceRule{UnitPrice: 0}, 1, "mock", nil
}

type mediaResolver struct{}

type acceptedSampleResolver struct{ mediaResolver }

func (r acceptedSampleResolver) ResolveTask(ctx context.Context, id, op string) (td.Binding, error) {
	b, err := r.mediaResolver.ResolveTask(ctx, id, op)
	b.ProviderConfig = json.RawMessage(`{"delaySeconds":0,"failurePercent":0,"fail":false}`)
	return b, err
}

func (mediaResolver) ResolveTask(_ context.Context, id, op string) (td.Binding, error) {
	actual := md.MockImageModelID
	if op == "video.generate" {
		actual = md.MockVideoModelID
	}
	if id != actual {
		return td.Binding{}, fault.ErrModelUnavailable
	}
	frozen, _ := json.Marshal(md.MockExecution{VideoOptions: actual == md.MockVideoModelID})
	return td.Binding{ProviderConfig: frozen, ConnectionID: "mock", Protocol: md.MockProtocol, ModelID: id, RequestModelID: actual, Free: true}, nil
}
func mediaFixture(t *testing.T) (*Service, *taskapp.Runtime, d.Project, appconfig.Config) {
	t.Helper()
	path := os.Getenv("DRAMA_MEDIA_TEST_CONFIG")
	if path == "" {
		t.Skip("isolated media PG/S3 opt-in")
	}
	cfg, err := appconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	db := os.Getenv("DRAMA_MEDIA_TEST_DB")
	if !strings.HasPrefix(db, "drama_parse_test_") {
		t.Fatal("use existing isolated parse test database")
	}
	u, err := url.Parse(cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + db
	cfg.Database.URL = u.String()
	cfg.Storage.Bucket = "drama-media-tests-20261016"
	cfg.NSQ.Topic = "drama_media_test_" + strings.TrimPrefix(identity.New("q"), "q_")
	cfg.NSQ.Channel = "free_media"
	cfg.Tasks.PollInterval = 20 * time.Millisecond
	cfg.Tasks.ScanInterval = 50 * time.Millisecond
	ctx := context.Background()
	store, err := postgres.Open(ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// 与正式启动顺序一致：迁移完成才启动运行事件投影；DDL不能与投影持锁交错。
	redisruntime.AttachURL(t, store, cfg.Database.URL)
	objects, err := s3objects.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	if err = objects.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	files := &uploadapp.Service{Store: store, Objects: objects, Thumbnails: imagethumbnail.FFmpeg{Settings: cfg.Media}, CommitCheckTimeout: cfg.Uploads.CommitCheckTimeout, CleanupTimeout: cfg.Uploads.CleanupTimeout}
	rt, q, err := taskruntime.New(cfg, store, objects, mediaResolver{}, redisruntime.ConnectDatabase(t, cfg.Database.URL), files)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { q.Close() })
	s, _, p := p5Prepared(t)
	p.ID = identity.New("media_free")
	p.Title = "免费媒体制作隔离验证"
	p.OwnerID = "media-isolated-fixture"
	p.Style = &d.ProjectStyle{ID: "free-style", Prompts: d.StylePrompts{ZH: "固定测试风格，仅追加一次"}}
	p.ProjectSettings = &d.ProjectSettings{AspectRatio: "9:16", ImageModel: md.MockImageModelID, VideoModel: md.MockVideoModelID}
	s.Store = store
	s.Files = files
	s.MediaTasks = rt.Service
	s.Prices = mediaFreePrices{}
	s.Credits = store
	rt.Service.Permit = &MediaCreditPermit{Ledger: store, Tasks: rt.Service.Store}
	if err = store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	// Create a real saved graph through the same aggregate command used by the agent.
	p, _ = p5Change(t, s, p, map[string]any{"op": "createScene", "episodeId": "e1", "value": p5Scene(p.Storyboard.Index.Episodes[0])})
	sc := p.Storyboard.Scenes[0]
	p, _ = p5Change(t, s, p, map[string]any{"op": "createShot", "sceneId": sc.ID, "value": p5Shot(p.Storyboard.Index.Episodes[0])})
	return s, rt, p, cfg
}
func mediaFinish(t *testing.T, s *Service, v MediaView) MediaView {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if e := s.MediaTasks.Drive(context.Background(), v.ID); e != nil {
			t.Fatal(e)
		}
		now, e := s.Media(context.Background(), v.ProjectID, v.ID)
		if e != nil {
			t.Fatal(e)
		}
		if now.Status.Terminal() {
			if now.Status != td.Succeeded {
				t.Fatal(now.Status, now.Error)
			}
			return now
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("media task timed out")
	return v
}
func TestProjectMediaAcceptedSampleSurvivesNewDefaultsAndRestart(t *testing.T) {
	s, rt, p, cfg := mediaFixture(t)
	ctx := context.Background()
	for _, explicit := range []bool{false, true} {
		target := MediaTarget{EpisodeID: "e1", ShotID: p.Storyboard.Shots[0].ID}
		_, prompt, digest, err := mediaInput(&p, MediaCommand{Target: target})
		if err != nil {
			t.Fatal(err)
		}
		command := MediaCommand{CommandID: identity.New("accepted-sample"), Revision: p.Revision, ModelID: md.MockVideoModelID, Target: target, BillingBasis: "original-accepted-basis"}
		input := mockmedia.TaskInput{Prompt: prompt}
		if explicit {
			command.Options = &md.MediaOptions{Resolution: "1080p", DurationSeconds: md.MockVideoDurationSeconds}
			input.Resolution, input.DurationSeconds = "1080p", md.MockVideoDurationSeconds
		}
		charge := &c.Charge{ID: mediaChargeID(p.OwnerID, p.ID, command.CommandID), SourceVersion: p.ScriptSource.Version, InputRevision: p.Revision, Unit: "second", ModelID: md.MockVideoModelID, ConnectionID: "mock", Scope: "media-video", ScopeDigest: identity.New("old-basis"), Quantity: md.MockVideoBillingSeconds, PriceVersion: 1, Rule: c.PriceRule{}, AcceptedAt: time.Now().UTC()}
		meta, _ := json.Marshal(MediaContext{Request: command, Credit: charge, SourceVersion: p.ScriptSource.Version, InputDigest: digest})
		raw, _ := json.Marshal(input)
		// This guarded fixture represents an already accepted original binding;
		// public new commands cannot select the retired fractional sample spec.
		rt.Service.Resolver = acceptedSampleResolver{}
		accepted, err := rt.Service.ForOwner(p.OwnerID).Submit(ctx, taskapp.Command{RequirePermit: true, ScopeID: mediaScope(p.ID), CommandID: command.CommandID, Operation: "video.generate", ModelID: md.MockVideoModelID, Input: raw, CallerContext: meta, ActiveKey: d.Digest(target)})
		if err != nil {
			t.Fatal(err)
		}
		before, err := rt.Service.Input(ctx, mediaScope(p.ID), accepted.ID)
		if err != nil {
			t.Fatal(err)
		}
		objects, err := s3objects.New(cfg.Storage)
		if err != nil {
			t.Fatal(err)
		}
		restarted, q, err := taskruntime.New(cfg, s.Store.(*postgres.Store), objects, mediaResolver{}, redisruntime.ConnectDatabase(t, cfg.Database.URL), s.Files.(ports.GeneratedFiles))
		if err != nil {
			t.Fatal(err)
		}
		defer q.Close()
		restarted.Service.Permit = &MediaCreditPermit{Ledger: s.Credits, Tasks: restarted.Service.Store}
		s.MediaTasks = restarted.Service
		view, err := s.Media(ctx, p.ID, accepted.ID)
		if err != nil {
			t.Fatal(err)
		}
		view = mediaFinish(t, s, view)
		file := view.Result.Files[0]
		if file.Width != 1920 || file.Height != 1080 || file.DurationSeconds == nil || *file.DurationSeconds != md.MockVideoDurationSeconds || view.Credit.Basis.Quantity != 11 || view.Credit.Charged != 0 {
			t.Fatal("accepted sample output/charge changed", view)
		}
		after, err := restarted.Service.Input(ctx, mediaScope(p.ID), accepted.ID)
		if err != nil || !bytes.Equal(before.Input, after.Input) || !bytes.Equal(before.CallerContext, after.CallerContext) {
			t.Fatal("old snapshot rewritten", err)
		}
		replay, err := s.StartMedia(ctx, p.ID, command)
		if err != nil || replay.ID != accepted.ID {
			t.Fatal("old original command revalidated against new profile", err)
		}
		command.CommandID = identity.New("new-sample-rejected")
		command.Options = &md.MediaOptions{Resolution: "1080p", DurationSeconds: md.MockVideoDurationSeconds}
		if _, err = s.StartMedia(ctx, p.ID, command); err == nil {
			t.Fatal("new command accepted retired sample options")
		}
	}
	t.Log("old omitted/explicit video input and original 11-second Charge survive new resolver/runtime; snapshots and command replay unchanged")
}

func TestProjectMediaNaturalImageVideoPreservesPromptAndBindsByID(t *testing.T) {
	s, rt, p, _ := mediaFixture(t)
	ctx := context.Background()
	before := d.Clone(p)
	in := MediaCommand{CommandID: identity.New("image_command"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: MediaTarget{AssetID: "c"}}
	in = quotedMediaCommand(t, s, p, in)
	v, e := s.StartMedia(ctx, p.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	same, e := s.StartMedia(ctx, p.ID, in)
	if e != nil || same.ID != v.ID {
		t.Fatal("duplicate start", e)
	}
	if _, e = s.StartMedia(ctx, p.ID, quotedMediaCommand(t, s, p, MediaCommand{CommandID: identity.New("duplicate"), Revision: p.Revision, ModelID: in.ModelID, Target: in.Target})); e == nil {
		t.Fatal("active target accepted")
	}
	v = mediaFinish(t, s, v)
	f := v.Result.Files[0]
	if f.FileID == "" || f.ThumbnailURL == "" {
		t.Fatal("generated image lacks stable file/thumbnail")
	}
	input, e := s.MediaTasks.Input(ctx, mediaScope(p.ID), v.ID)
	if e != nil || strings.Count(string(input.Input), p.Style.Prompts.ZH) != 1 {
		t.Fatal("style append count", e)
	}
	if v.AdoptedOutputID != "" {
		t.Fatal("implicitly adopted")
	}
	p, e = s.AdoptMedia(ctx, p.ID, v.ID, f.ID, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if p.Asset("c").ImageFileID != f.FileID || p.Asset("unused").ImageFileID != "" {
		t.Fatal("wrong asset identity")
	}
	original, raw, e := s.Files.ReadOriginal(ctx, f.FileID)
	if e != nil || !bytes.HasPrefix(raw, []byte{0xff, 0xd8}) || original.Thumbnail == nil {
		t.Fatal("image file missing", e)
	}
	if d.Digest(before.Storyboard) != d.Digest(p.Storyboard) || d.Digest(before.VisualPrompts) != d.Digest(p.VisualPrompts) {
		t.Fatal("image changed storyboard/text")
	}
	if dir := os.Getenv("DRAMA_MEDIA_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "image.jpg"), raw, 0644)
		_, thumb, err := s.Files.(*uploadapp.Service).Read(ctx, f.FileID, true)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "image-thumb.webp"), thumb, 0644)
	}
	sh := p.Storyboard.Shots[0]
	videoIn := MediaCommand{CommandID: identity.New("video_command"), Revision: p.Revision, ModelID: md.MockVideoModelID, Target: MediaTarget{EpisodeID: "e1", ShotID: sh.ID}}
	videoIn = quotedMediaCommand(t, s, p, videoIn)
	video, e := s.StartMedia(ctx, p.ID, videoIn)
	if e != nil {
		t.Fatal(e)
	}
	if video.Options != (md.MediaOptions{Resolution: "1080p", DurationSeconds: 10}) {
		t.Fatal("server-resolved defaults missing from accepted task view", video.Options)
	}
	video = mediaFinish(t, s, video)
	beforeCover, err := s.MediaTasks.Store.Task(ctx, video.ID)
	if err != nil {
		t.Fatal(err)
	}
	cover, err := s.MediaThumbnail(ctx, p.ID, video.ID, video.Result.Files[0].ID)
	if err != nil || http.DetectContentType(cover) != "image/webp" {
		t.Fatal("saved video first frame", err)
	}
	results := rt.Service.Results.(*s3objects.TaskResults)
	encoder := results.Thumbnails
	results.Thumbnails = nil
	results.ReadOnly = true
	cached, err := s.MediaThumbnail(ctx, p.ID, video.ID, video.Result.Files[0].ID)
	if err != nil || !bytes.Equal(cover, cached) {
		t.Fatal("cover cache required encoder or write", err)
	}
	results.Thumbnails = encoder
	results.ReadOnly = false
	afterCover, _ := s.MediaTasks.Store.Task(ctx, video.ID)
	if d.Digest(beforeCover) != d.Digest(afterCover) {
		t.Fatal("cover read changed task or billing")
	}
	if _, err = s.MediaThumbnail(ctx, "foreign", video.ID, video.Result.Files[0].ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("cross-project cover", err)
	}
	episode, err := s.StoryboardEpisode(p, "e1")
	if err != nil {
		t.Fatal(err)
	}
	episode, err = s.EpisodePreviews(ctx, p, episode)
	if err != nil || episode.Shots[0].ThumbnailURL != mediaThumbnailURL(p.ID, video.ID, video.Result.Files[0].ID) {
		t.Fatal("latest succeeded cover projection", episode.Shots, err)
	}
	vf := video.Result.Files[0]
	if vf.DurationSeconds == nil || *vf.DurationSeconds <= 0 || vf.Width != 1920 {
		t.Fatal("unverified video")
	}
	snap, _ := s.MediaTasks.Input(ctx, mediaScope(p.ID), video.ID)
	var request mockmedia.TaskInput
	if json.Unmarshal(snap.Input, &request) != nil || !strings.Contains(request.Prompt, "你自己决定。") || !strings.Contains(request.Prompt, "@林晚") {
		t.Fatal("complete prompt missing")
	}
	p, e = s.AdoptMedia(ctx, p.ID, video.ID, vf.ID, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	video, e = s.Media(ctx, p.ID, video.ID)
	if e != nil || video.AdoptedOutputID != vf.ID {
		t.Fatal("adoption readback", e)
	}
	data, meta, e := s.MediaFile(ctx, p.ID, video.ID, vf.ID)
	if e != nil || len(data) == 0 || meta.SHA256 != vf.SHA256 {
		t.Fatal("video bytes", e)
	}
	if dir := os.Getenv("DRAMA_MEDIA_EVIDENCE_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "video.mp4"), data, 0644)
		b, _ := json.MarshalIndent(map[string]any{"imageTask": v, "videoTask": video, "imageInput": json.RawMessage(input.Input), "videoInput": json.RawMessage(snap.Input), "projectId": p.ID}, "", "  ")
		os.WriteFile(filepath.Join(dir, "natural-flow.json"), b, 0644)
	}
	// Ordinary project revision changes do not erase accepted commands or resubmit.
	replay, e := s.StartMedia(ctx, p.ID, videoIn)
	if e != nil || replay.ID != video.ID {
		t.Fatal("replay revision", e)
	}
	fresh := *s
	fresh.MediaTasks = &taskapp.Service{Store: s.MediaTasks.Store, Results: s.MediaTasks.Results}
	read, e := fresh.Media(ctx, p.ID, video.ID)
	if e != nil || read.AdoptedOutputID != vf.ID {
		t.Fatal("restart read", e)
	}
}
func TestProjectMediaStaleIsolationCASCancelAndImageAfterBreakdown(t *testing.T) {
	s, _, p, _ := mediaFixture(t)
	ctx := context.Background()
	sh := p.Storyboard.Shots[0]
	in := MediaCommand{CommandID: identity.New("video"), Revision: p.Revision, ModelID: md.MockVideoModelID, Target: MediaTarget{EpisodeID: "e1", ShotID: sh.ID}}
	in = quotedMediaCommand(t, s, p, in)
	v, e := s.StartMedia(ctx, p.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	v = mediaFinish(t, s, v)
	if _, e = s.Media(ctx, "foreign", v.ID); !errors.Is(e, fault.ErrNotFound) {
		t.Fatal("project leak", e)
	}
	bad := in
	bad.CommandID = identity.New("bad_episode")
	bad.Target.EpisodeID = "e2"
	if _, e = s.StartMedia(ctx, p.ID, bad); !errors.Is(e, fault.ErrNotFound) {
		t.Fatal("episode leak", e)
	}
	if _, e = s.AdoptMedia(ctx, p.ID, v.ID, v.Result.Files[0].ID, p.Revision-1); !errors.Is(e, fault.ErrConflict) {
		t.Fatal("CAS accepted", e)
	}
	// Mock video consumes only the prompt: later asset images are presentation, not input.
	image, e := s.StartMedia(ctx, p.ID, quotedMediaCommand(t, s, p, MediaCommand{CommandID: identity.New("image"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: MediaTarget{AssetID: "c"}}))
	if e != nil {
		t.Fatal(e)
	}
	image = mediaFinish(t, s, image)
	p, e = s.AdoptMedia(ctx, p.ID, image.ID, image.Result.Files[0].ID, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	read, e := s.Media(ctx, p.ID, v.ID)
	if e != nil || read.Stale {
		t.Fatal("picture incorrectly invalidated prompt-only video", e)
	}
	parts := append(append([]d.PromptPart{}, p.Storyboard.Shots[0].VideoPrompt.Parts...), d.PromptPart{Type: "text", Text: "镜头结束后保持静止。"})
	for i := range parts {
		parts[i].LabelAtSave = ""
	}
	p, _ = p5Change(t, s, p, map[string]any{"op": "updateShot", "shotId": sh.ID, "patch": map[string]any{"videoPrompt": map[string]any{"parts": parts}}})
	if _, e = s.AdoptMedia(ctx, p.ID, v.ID, v.Result.Files[0].ID, p.Revision); e == nil {
		t.Fatal("stale prompt adopted")
	}
	read, e = s.Media(ctx, p.ID, v.ID)
	if e != nil || !read.Stale {
		t.Fatal("stale prompt view", e)
	}

	cancel, e := s.StartMedia(ctx, p.ID, quotedMediaCommand(t, s, p, MediaCommand{CommandID: identity.New("cancel"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: MediaTarget{AssetID: "c2"}}))
	if e != nil {
		t.Fatal(e)
	}
	cancel, e = s.CancelMedia(ctx, p.ID, cancel.ID, cancel.Revision)
	if e != nil || cancel.Status != td.Cancelled {
		t.Fatal(e)
	}
	if e = s.MediaTasks.Drive(ctx, cancel.ID); e != nil {
		t.Fatal(e)
	}
	cancel, e = s.Media(ctx, p.ID, cancel.ID)
	if e != nil || cancel.Status != td.Cancelled || cancel.Result != nil {
		t.Fatal("cancel revived", e)
	}
	state, _ := s.MediaTasks.Store.Task(ctx, v.ID)
	if len(state.State.Submissions) != 1 {
		t.Fatal("repeated submission")
	}
	if p.Asset("unused").ImageFileID != "" {
		t.Fatal("same-name asset remapped")
	}
}

var _ ports.TaskResolver = mediaResolver{}

type controlledMediaProvider struct {
	ports.TaskProvider
	entered chan struct{}
	release chan struct{}
	calls   int
	unknown bool
}

func (p *controlledMediaProvider) Submit(ctx context.Context, b td.Binding, id string, raw json.RawMessage) (td.Receipt, error) {
	p.calls++
	if p.entered != nil {
		close(p.entered)
		<-p.release
	}
	if p.unknown {
		return td.Receipt{Outcome: "unknown", Raw: json.RawMessage(`{"freeFixture":"unknown"}`), Billing: td.Billing{Status: "unknown"}}, nil
	}
	return p.TaskProvider.Submit(ctx, b, id, raw)
}
func TestProjectMediaUnknownNeverResubmitsAndCancelledLateCannotAdopt(t *testing.T) {
	s, _, p, _ := mediaFixture(t)
	ctx := context.Background()
	provider := &controlledMediaProvider{TaskProvider: s.MediaTasks.Provider, unknown: true}
	s.MediaTasks.Provider = provider
	in := MediaCommand{CommandID: identity.New("unknown"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: MediaTarget{AssetID: "c"}}
	in = quotedMediaCommand(t, s, p, in)
	v, e := s.StartMedia(ctx, p.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MediaTasks.Drive(ctx, v.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.MediaTasks.Drive(ctx, v.ID); e != nil {
		t.Fatal(e)
	}
	read, e := s.Media(ctx, p.ID, v.ID)
	if e != nil || read.Status != td.Unknown || provider.calls != 1 {
		t.Fatal("unknown resubmitted", e, provider.calls)
	}
	replay, e := s.StartMedia(ctx, p.ID, in)
	if e != nil || replay.ID != v.ID || provider.calls != 1 {
		t.Fatal("unknown replay", e)
	}
	// Explicit cancellation is a terminal fact; a concurrent provider receipt is only evidence.
	late := &controlledMediaProvider{TaskProvider: mockmedia.TaskProvider{}, entered: make(chan struct{}), release: make(chan struct{})}
	s.MediaTasks.Provider = late
	in.CommandID = identity.New("late")
	in.Target = MediaTarget{AssetID: "c2"}
	in = quotedMediaCommand(t, s, p, in)
	v, e = s.StartMedia(ctx, p.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- s.MediaTasks.Drive(ctx, v.ID) }()
	<-late.entered
	current, e := s.Media(ctx, p.ID, v.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.CancelMedia(ctx, p.ID, v.ID, current.Revision)
	if e != nil {
		t.Fatal(e)
	}
	close(late.release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	current, e = s.Media(ctx, p.ID, v.ID)
	if e != nil || current.Status != td.Cancelled || current.Result != nil {
		t.Fatal("late revived", e)
	}
	saved, e := s.MediaTasks.Store.Task(ctx, v.ID)
	if e != nil || len(saved.State.Submissions) != 1 || saved.State.Submissions[0].Receipt == nil {
		t.Fatal("late receipt lost", e)
	}
	after, _ := s.Project(ctx, p.ID)
	if after.Asset("c2").ImageFileID != "" || after.Revision != p.Revision {
		t.Fatal("late adopted")
	}
}

func TestProjectMediaQueuedTaskRecoveredByNewNSQRuntime(t *testing.T) {
	s, old, p, cfg := mediaFixture(t)
	ctx := context.Background()
	in := MediaCommand{CommandID: identity.New("restart"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: MediaTarget{AssetID: "c"}}
	in = quotedMediaCommand(t, s, p, in)
	v, e := s.StartMedia(ctx, p.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	// No worker consumed the original queue. A new runtime reads the same PG intent.
	objects := old.Service.Results.(*s3objects.TaskResults).Objects
	restarted, q, e := taskruntime.New(cfg, s.Store.(*postgres.Store), objects, mediaResolver{}, redisruntime.ConnectDatabase(t, cfg.Database.URL), s.Files.(*uploadapp.Service))
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	s.MediaTasks = restarted.Service
	restarted.Service.Permit = &MediaCreditPermit{Ledger: s.Credits, Tasks: restarted.Service.Store}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- restarted.Run(workerCtx) }()
	var got MediaView
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		got, e = s.Media(ctx, p.ID, v.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Status.Terminal() {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	cancel()
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if got.Status != td.Succeeded || got.Result == nil {
		t.Fatal("queued task not recovered", got.Status, got.Error)
	}
	stored, e := s.MediaTasks.Store.Task(ctx, v.ID)
	if e != nil || len(stored.State.Submissions) != 1 {
		t.Fatal("recovery duplicated submission", e)
	}
	same, e := s.StartMedia(ctx, p.ID, in)
	if e != nil || same.ID != got.ID {
		t.Fatal("recovered command identity", e)
	}
}

func TestProjectMediaAllAssetKindsAndLookStayIndependentAfterBreakdown(t *testing.T) {
	s, _, p, _ := mediaFixture(t)
	ctx := context.Background()
	targets := []MediaTarget{{AssetID: "c"}, {AssetID: "c", LookID: "l"}, {AssetID: "loc"}, {AssetID: "prop"}, {AssetID: "unused"}}
	views := []MediaView{}
	for _, target := range targets {
		v, e := s.StartMedia(ctx, p.ID, quotedMediaCommand(t, s, p, MediaCommand{CommandID: identity.New("all_kinds"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: target}))
		if e != nil {
			t.Fatal(target, e)
		}
		if e = s.MediaTasks.Drive(ctx, v.ID); e != nil {
			t.Fatal(e)
		}
		views = append(views, v)
	}
	files := map[string]bool{}
	for _, v := range views {
		v = mediaFinish(t, s, v)
		var e error
		p, e = s.AdoptMedia(ctx, p.ID, v.ID, v.Result.Files[0].ID, p.Revision)
		if e != nil {
			t.Fatal(e)
		}
		file := v.Result.Files[0].FileID
		if files[file] {
			t.Fatal("different target shared identity")
		}
		files[file] = true
	}
	if p.Asset("c").ImageFileID == "" || p.Asset("c").Looks[0].ImageFileID == "" || p.Asset("c").ImageFileID == p.Asset("c").Looks[0].ImageFileID || p.Asset("prop").ImageFileID == p.Asset("unused").ImageFileID {
		t.Fatal("wrong asset or look binding")
	}
}

func TestProjectMediaCapacityFailureKeepsFullInputAndCreatesNoTask(t *testing.T) {
	s, _, p, _ := mediaFixture(t)
	ctx := context.Background()
	original := strings.Repeat("完整不可截断。", 4000)
	p.Prompt(d.VisualTarget{AssetID: "c"}).Text = original
	var e error
	p, e = s.commit(ctx, p, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	in := MediaCommand{CommandID: identity.New("capacity"), Revision: p.Revision, ModelID: md.MockImageModelID, Target: MediaTarget{AssetID: "c"}}
	in = quotedMediaCommand(t, s, p, in)
	if _, e = s.StartMedia(ctx, p.ID, in); !errors.Is(e, fault.ErrInvalid) {
		t.Fatal("oversized input accepted", e)
	}
	if _, e = s.MediaTasks.GetByCommand(ctx, mediaScope(p.ID), in.CommandID); !errors.Is(e, fault.ErrNotFound) {
		t.Fatal("rejected input created task", e)
	}
	after, e := s.Project(ctx, p.ID)
	if e != nil || after.Revision != p.Revision || after.Prompt(d.VisualTarget{AssetID: "c"}).Text != original {
		t.Fatal("capacity failure truncated source", e)
	}
}

// The production consumer now always obtains the same authoritative quote even
// for zero-price fixtures; tests must not preserve an anonymous billing bypass.
func quotedMediaCommand(t *testing.T, s *Service, p d.Project, in MediaCommand) MediaCommand {
	t.Helper()
	q, e := s.Quote(context.Background(), p.ID, QuoteInput{Revision: in.Revision, Operation: "media", ModelID: in.ModelID, Target: &in.Target, Options: in.Options})
	if e != nil {
		t.Fatal(e)
	}
	in.BillingBasis = q.Basis
	return in
}

func (p mediaFreePrices) MediaPrice(ctx context.Context, model string) (ports.MediaPricing, error) {
	rule, version, connection, err := p.CreditPrice(ctx, model)
	profile := md.OfficialMediaProfile(connection, model)
	return ports.MediaPricing{Rule: rule, Version: version, ConnectionID: connection, Profile: profile, PriceDefaultResolution: profile.DefaultResolution}, err
}
