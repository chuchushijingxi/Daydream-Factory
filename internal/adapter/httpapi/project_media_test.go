package httpapi_test

import (
	"bytes"
	"context"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/imagethumbnail"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/adapter/stylecatalog"
	"drama-agent/internal/adapter/taskruntime"
	"drama-agent/internal/application/ports"
	project "drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	td "drama-agent/internal/domain/task"
	"drama-agent/internal/testsupport/modelregistry"
)

type mediaHTTPFreePrices struct{}

func (mediaHTTPFreePrices) CreditPrice(context.Context, string) (c.PriceRule, uint64, string, error) {
	return c.PriceRule{UnitPrice: 0}, 1, "mock", nil
}

type mediaHTTPResolver struct{}

func (mediaHTTPResolver) ResolveTask(_ context.Context, id, op string) (td.Binding, error) {
	if !(id == md.MockImageModelID && op == "image.generate" || id == md.MockVideoModelID && op == "video.generate") {
		return td.Binding{}, fault.ErrModelUnavailable
	}
	frozen, _ := json.Marshal(md.MockExecution{VideoOptions: id == md.MockVideoModelID})
	return td.Binding{ProviderConfig: frozen, ConnectionID: "mock", Protocol: md.MockProtocol, ModelID: id, RequestModelID: id, Free: true}, nil
}
func TestProjectMediaHTTPStrictStartReceiptAdoptionRangeAndScope(t *testing.T) {
	path := os.Getenv("DRAMA_MEDIA_TEST_CONFIG")
	if path == "" {
		t.Skip("isolated media PG/S3 opt-in")
	}
	cfg, e := appconfig.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	db := os.Getenv("DRAMA_MEDIA_TEST_DB")
	if !strings.HasPrefix(db, "drama_parse_test_") {
		t.Fatal("isolated DB required")
	}
	dsn, _ := url.Parse(cfg.Database.URL)
	dsn.Path = "/" + db
	cfg.Database.URL = dsn.String()
	cfg.Storage.Bucket = "drama-media-tests-20261016"
	cfg.NSQ.Topic = "drama_media_http_" + strings.TrimPrefix(identity.New("q"), "q_")
	cfg.Tasks.PollInterval = 20 * time.Millisecond
	ctx := context.Background()
	store, e := postgres.Open(ctx, cfg.Database.URL)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(store.Close)
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	redisruntime.AttachURL(t, store, cfg.Database.URL)
	objects, e := s3objects.New(cfg.Storage)
	if e != nil {
		t.Fatal(e)
	}
	if e = objects.EnsureBucket(ctx); e != nil {
		t.Fatal(e)
	}
	files := &uploadapp.Service{Store: store, Objects: objects, Thumbnails: imagethumbnail.FFmpeg{Settings: cfg.Media}, CommitCheckTimeout: cfg.Uploads.CommitCheckTimeout, CleanupTimeout: cfg.Uploads.CleanupTimeout}
	rt, q, e := taskruntime.New(cfg, store, objects, mediaHTTPResolver{}, redisruntime.ConnectDatabase(t, cfg.Database.URL), files)
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	p := d.Project{ID: identity.New("media_http"), SpaceID: "http-fixture", CreatorID: "http-fixture", OwnerID: "http-fixture", Revision: 1, Title: "隔离媒体 HTTP", ScriptSource: d.ScriptSource{Version: 1}, Assets: []d.Asset{{ID: "asset", Kind: "prop", Name: "铜钥匙"}}, Runs: []d.Run{{ID: "catalog", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: d.RunCompleted}}, VisualPrompts: []d.VisualPrompt{{VisualPromptInput: d.VisualPromptInput{Target: d.VisualTarget{AssetID: "asset"}, Text: "铜制钥匙，完整测试外观"}, SourceVersion: 1, Origin: "user"}}, Storyboard: &d.Storyboard{SourceVersion: 1}}
	seedMediaHTTPStoryboard(&p)
	p.Assets = append(p.Assets, d.Asset{ID: "batch_asset", Kind: "prop", Name: "玻璃杯"})
	p.VisualPrompts = append(p.VisualPrompts, d.VisualPrompt{VisualPromptInput: d.VisualPromptInput{Target: d.VisualTarget{AssetID: "batch_asset"}, Text: "透明玻璃杯，完整测试外观"}, SourceVersion: 1, Origin: "user"})
	if e = store.Create(ctx, p); e != nil {
		t.Fatal(e)
	}
	rt.Service.Permit = &project.MediaCreditPermit{Ledger: store, Tasks: rt.Service.Store}
	service := &project.Service{Credits: store, Prices: mediaHTTPFreePrices{}, Store: store, Files: files, MediaTasks: rt.Service, Models: &modelregistry.Registry{OptionsValue: ports.ModelOptions{Image: []ports.ModelOption{{ID: md.MockImageModelID, Name: "Mock Image"}}, Video: []ports.ModelOption{{ID: md.MockVideoModelID, Name: "Mock Video"}}}}}
	service.Styles, e = stylecatalog.LoadStyles("../../../demo")
	if e != nil {
		t.Fatal(e)
	}
	host, raw := httpapi.New(configuredFixture(service), nil, "../../../demo", 0)
	host.Files = files
	handler := authenticatedFixture(host, raw)
	if path := os.Getenv("DRAMA_MEDIA_UI_URL_FILE"); path != "" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Test-only public shell. Production deliberately never serves __* fixtures.
			if strings.HasPrefix(r.URL.Path, "/demo/__media-live-fixture/") {
				http.StripPrefix("/demo/", http.FileServer(http.Dir("../../../demo"))).ServeHTTP(w, r)
				return
			}
			handler.ServeHTTP(w, r)
		}))
		defer server.Close()
		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- rt.Run(workerCtx) }()
		if e = os.WriteFile(path, []byte(server.URL+"/demo/__media-live-fixture/tests/media-live.html#/playlet/"+p.ID+"/production"), 0600); e != nil {
			t.Fatal(e)
		}
		for {
			if _, err := os.Stat(path + ".stop"); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		cancel()
		if e = <-done; e != nil {
			t.Fatal(e)
		}
		return
	}

	call := func(method, path string, body any) *httptest.ResponseRecorder {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		if method == "GET" && strings.HasSuffix(path, "/file") {
			req.Header.Set("Range", "bytes=0-31")
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	base := "/api/projects/" + p.ID + "/media-tasks"
	command := project.MediaCommand{CommandID: identity.New("command"), Revision: 1, ModelID: md.MockImageModelID, Target: project.MediaTarget{AssetID: "asset"}}
	quoteResponse := call("POST", "/api/projects/"+p.ID+"/quote", project.QuoteInput{Revision: 1, Operation: "media", ModelID: command.ModelID, Target: &command.Target})
	var mediaQuote project.ChargeQuote
	if quoteResponse.Code != 200 || json.Unmarshal(quoteResponse.Body.Bytes(), &mediaQuote) != nil {
		t.Fatal("media quote", quoteResponse.Code, quoteResponse.Body.String())
	}
	command.BillingBasis = mediaQuote.Basis
	bad := call("POST", base, map[string]any{"commandId": "bad", "revision": 1, "modelId": md.MockImageModelID, "target": map[string]any{"assetId": "asset", "prompt": "injected override"}})
	if bad.Code != 400 {
		t.Fatal("schema bypass", bad.Code, bad.Body.String())
	}
	out := call("POST", base, command)
	var v project.MediaView
	if out.Code != 202 || json.Unmarshal(out.Body.Bytes(), &v) != nil {
		t.Fatal(out.Code, out.Body.String())
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if e = rt.Service.Drive(ctx, v.ID); e != nil {
			t.Fatal(e)
		}
		out = call("GET", base+"/"+v.ID, nil)
		if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &v) != nil {
			t.Fatal(out.Code, out.Body.String())
		}
		if v.Status.Terminal() {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if v.Status != td.Succeeded || v.Result == nil || len(v.Result.Files) != 1 {
		t.Fatal("not complete", v.Status, v.Error)
	}
	f := v.Result.Files[0]
	if f.ThumbnailURL == "" || f.FileID == "" || f.ContentURL != base+"/"+v.ID+"/outputs/"+f.ID+"/file" {
		t.Fatal("file projection")
	}
	file := call("GET", f.ContentURL, nil)
	if file.Code != http.StatusPartialContent || file.Body.Len() != 32 || file.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatal("range file", file.Code)
	}
	out = call("POST", base+"/"+v.ID+"/adopt", map[string]any{"revision": 1, "outputId": f.ID})
	if out.Code != 200 || !strings.Contains(out.Body.String(), f.FileID) {
		t.Fatal("adoption", out.Code, out.Body.String())
	}
	out = call("POST", base, command)
	if out.Code != 202 || !strings.Contains(out.Body.String(), v.ID) {
		t.Fatal("replay after revision")
	}
	out = call("POST", base+"/"+v.ID+"/adopt", map[string]any{"revision": 1, "outputId": f.ID})
	if out.Code != 409 {
		t.Fatal("stale CAS accepted")
	}
	out = call("GET", "/api/projects/foreign/media-tasks/"+v.ID, nil)
	if out.Code != 404 {
		t.Fatal("cross-project leak")
	}
	out = call("GET", base, nil)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"adoptedOutputId":"`+f.ID+`"`) {
		t.Fatal("history read")
	}
	if strings.Contains(out.Body.String(), "objectKey") || strings.Contains(out.Body.String(), "credential") || strings.Contains(out.Body.String(), "inputDigest") {
		t.Fatal("private fields leaked")
	}
	videoCommand := project.MediaCommand{CommandID: identity.New("cover"), Revision: 2, ModelID: md.MockVideoModelID, Target: project.MediaTarget{EpisodeID: "episode", ShotID: "shot"}, Options: &md.MediaOptions{Resolution: "1080p", DurationSeconds: 1}}
	quoted := call("POST", "/api/projects/"+p.ID+"/quote", project.QuoteInput{Revision: 2, Operation: "media", ModelID: videoCommand.ModelID, Target: &videoCommand.Target, Options: videoCommand.Options})
	if quoted.Code != 200 || json.Unmarshal(quoted.Body.Bytes(), &mediaQuote) != nil {
		t.Fatal("video quote", quoted.Code, quoted.Body.String())
	}
	videoCommand.BillingBasis = mediaQuote.Basis
	started := call("POST", base, videoCommand)
	var video project.MediaView
	if started.Code != 202 || json.Unmarshal(started.Body.Bytes(), &video) != nil {
		t.Fatal("video start", started.Code, started.Body.String())
	}
	readEpisode := func() project.EpisodeView {
		t.Helper()
		response := call("GET", "/api/projects/"+p.ID+"/storyboard/episodes/episode", nil)
		var view project.EpisodeView
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &view) != nil || len(view.Shots) != 1 {
			t.Fatal("episode read", response.Code, response.Body.String())
		}
		return view
	}
	queuedEpisode := readEpisode()
	if queuedEpisode.Shots[0].VideoTask == nil || queuedEpisode.Shots[0].VideoTask.ID != video.ID || queuedEpisode.Shots[0].VideoTask.Status != td.Queued || queuedEpisode.Shots[0].ThumbnailURL != "" {
		t.Fatal("accepted video missing from shot list", queuedEpisode.Shots)
	}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		if e = rt.Service.Drive(ctx, video.ID); e != nil {
			t.Fatal(e)
		}
		response := call("GET", base+"/"+video.ID, nil)
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &video) != nil {
			t.Fatal("video read", response.Code)
		}
		if video.Status.Terminal() {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if video.Status != td.Succeeded || video.Result == nil {
		t.Fatal("video failed", video.Status)
	}
	preview := video.Result.Files[0].ThumbnailURL
	cover := call("GET", preview, nil)
	if preview == "" || cover.Code != 200 || http.DetectContentType(cover.Body.Bytes()) != "image/webp" || cover.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("protected still", cover.Code, cover.Body.String())
	}
	episodeResponse := call("GET", "/api/projects/"+p.ID+"/storyboard/episodes/episode", nil)
	var episode project.EpisodeView
	if episodeResponse.Code != 200 || json.Unmarshal(episodeResponse.Body.Bytes(), &episode) != nil || episode.Shots[0].ThumbnailURL != preview {
		t.Fatal("shot cover projection", episodeResponse.Code, episodeResponse.Body.String())
	}
	if episode.Shots[0].VideoTask == nil || episode.Shots[0].VideoTask.ID != video.ID || episode.Shots[0].VideoTask.Status != td.Succeeded || episode.Revision != queuedEpisode.Revision {
		t.Fatal("task completion must update shot list without a project write", episode.Shots, episode.Revision, queuedEpisode.Revision)
	}
	for _, path := range []string{strings.Replace(preview, p.ID, "foreign", 1), preview + "-missing"} {
		if response := call("GET", path, nil); response.Code != 404 {
			t.Fatal("foreign/missing thumbnail", response.Code)
		}
	}
	anonymous := httptest.NewRecorder()
	anonymousRequest := httptest.NewRequest("GET", "http://127.0.0.1:8787"+preview, nil)
	anonymousRequest.Header.Set("X-Drama-Audience", "workspace")
	raw.ServeHTTP(anonymous, anonymousRequest)
	if anonymous.Code != 401 {
		t.Fatal("anonymous thumbnail", anonymous.Code)
	}
	batchCommand := project.MediaCommand{CommandID: identity.New("batch"), Revision: 2, ModelID: md.MockImageModelID, Target: project.MediaTarget{AssetID: "batch_asset"}, Options: &md.MediaOptions{Resolution: "1K"}}
	batchQuote := call("POST", "/api/projects/"+p.ID+"/quote", project.QuoteInput{Revision: 2, Operation: "media", ModelID: batchCommand.ModelID, Target: &batchCommand.Target, Options: batchCommand.Options})
	if batchQuote.Code != 200 || json.Unmarshal(batchQuote.Body.Bytes(), &mediaQuote) != nil {
		t.Fatal("batch quote", batchQuote.Code)
	}
	batchCommand.BillingBasis = mediaQuote.Basis
	missingCommand := project.MediaCommand{CommandID: identity.New("missing-batch-target"), Revision: 2, ModelID: md.MockImageModelID, Target: project.MediaTarget{AssetID: "missing"}}
	batchResponse := call("POST", base+"/batch-images", map[string]any{"commands": []project.MediaCommand{batchCommand, missingCommand}})
	var batch struct {
		Items []struct {
			CommandID string             `json:"commandId"`
			Task      *project.MediaView `json:"task"`
			Error     any                `json:"error"`
		} `json:"items"`
	}
	if batchResponse.Code != 202 || json.Unmarshal(batchResponse.Body.Bytes(), &batch) != nil || len(batch.Items) != 2 || batch.Items[0].Task == nil || !batch.Items[0].Task.FillMissingImage || batch.Items[0].Error != nil || batch.Items[1].Task != nil || batch.Items[1].Error == nil {
		t.Fatal("real HTTP batch receipt", batchResponse.Code, batchResponse.Body.String())
	}
	acceptedID := batch.Items[0].Task.ID
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if e = rt.Service.Drive(ctx, acceptedID); e != nil {
			t.Fatal(e)
		}
		value, e := service.Media(ctx, p.ID, acceptedID)
		if e != nil {
			t.Fatal(e)
		}
		if value.Status.Terminal() {
			if value.Status != td.Succeeded {
				t.Fatal(value.Status)
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if e = service.RecoverImageFills(ctx); e != nil {
		t.Fatal(e)
	}
	projectRead := call("GET", "/api/projects/"+p.ID, nil)
	var projectDTO struct {
		Revision uint64              `json:"revision"`
		Assets   []httpapi.AssetView `json:"assets"`
	}
	generated, e := service.Media(ctx, p.ID, acceptedID)
	if e != nil || generated.Result == nil {
		t.Fatal("batch output not durable", e)
	}
	if projectRead.Code != 200 || json.Unmarshal(projectRead.Body.Bytes(), &projectDTO) != nil || projectDTO.Revision != 3 {
		t.Fatal("HTTP project image writeback", projectRead.Code, projectRead.Body.String())
	}
	filled := false
	for _, asset := range projectDTO.Assets {
		if asset.ID == "batch_asset" && asset.Image != nil && asset.Image.FileID == generated.Result.Files[0].FileID {
			filled = true
		}
	}
	if !filled {
		t.Fatal("HTTP asset did not point to its generated file")
	}
	imageList := call("GET", base+"?kind=image", nil)
	if imageList.Code != 200 || !strings.Contains(imageList.Body.String(), acceptedID) || strings.Contains(imageList.Body.String(), video.ID) {
		t.Fatal("image list filter", imageList.Code, imageList.Body.String())
	}
	if invalidKind := call("GET", base+"?kind=invalid", nil); invalidKind.Code != 400 {
		t.Fatal("unknown media kind accepted", invalidKind.Code)
	}
	if foreign := call("POST", "/api/projects/foreign/media-tasks/batch-images", map[string]any{"commands": []project.MediaCommand{batchCommand}}); foreign.Code != 404 {
		t.Fatal("cross-project batch accepted", foreign.Code)
	}
	replayed := call("POST", base+"/batch-images", map[string]any{"commands": []project.MediaCommand{batchCommand}})
	if replayed.Code != 202 || !strings.Contains(replayed.Body.String(), acceptedID) {
		t.Fatal("batch replay after filling", replayed.Code, replayed.Body.String())
	}
	for _, invalid := range []any{
		map[string]any{"commands": []project.MediaCommand{batchCommand, batchCommand}},
		map[string]any{"commands": []project.MediaCommand{videoCommand}},
		map[string]any{"commands": []project.MediaCommand{batchCommand}, "ownerId": "foreign"},
	} {
		if response := call("POST", base+"/batch-images", invalid); response.Code != 400 {
			t.Fatal("batch schema bypass", response.Code, response.Body.String())
		}
	}
	host.ReadOnly = true
	out = call("POST", base, command)
	if out.Code != 403 {
		t.Fatal("readonly accepted")
	}
	if response := call("POST", base+"/batch-images", map[string]any{"commands": []project.MediaCommand{batchCommand}}); response.Code != 403 {
		t.Fatal("readonly batch accepted", response.Code)
	}
}

func seedMediaHTTPStoryboard(p *d.Project) {
	text := "第1集。林晚把铜钥匙放在桌上，镜头停在钥匙和空出的手之间。"
	p.ScriptSource.Filename = "免费媒体UI夹具.txt"
	p.ScriptSource.Spans = []d.SourceSpan{{ID: "span", Text: text}}
	ranges := []d.SourceRange{{Start: d.SourcePoint{SpanID: "span"}, End: d.SourcePoint{SpanID: "span", Offset: len([]rune(text))}}}
	p.Storyboard = d.NewStoryboard(1)
	mode := "episodes"
	p.Storyboard.Index.Segmentation = &mode
	number := 1
	ep := d.Episode{ID: "episode", EpisodeValue: d.EpisodeValue{Title: "第1集 · 钥匙与决定", EpisodeNumber: &number, SourceRanges: ranges}, SceneIDs: []string{"scene"}, Origin: "user"}
	p.Storyboard.Index.Episodes = []d.Episode{ep}
	basis := p.EpisodeSceneBasis(&ep)
	sc := d.Scene{ID: "scene", EpisodeID: ep.ID, SceneValue: d.SceneValue{Title: "客厅 · 钥匙与决定", SourceRanges: ranges, Time: "傍晚", Summary: "将钥匙放回桌上"}, ShotIDs: []string{"shot"}, Basis: basis, Origin: "user"}
	p.Storyboard.Scenes = []d.Scene{sc}
	sh := d.Shot{ID: "shot", SceneID: sc.ID, ShotValue: d.ShotValue{Action: "将铜钥匙放在桌上", Dialogues: []d.Dialogue{}, SourceRanges: ranges, SelectedAssets: []d.VisualTarget{{AssetID: "asset"}}}, Basis: d.ShotBasis{SceneBasis: basis, SceneRangeDigest: d.Digest(ranges)}, Origin: "user"}
	p.Storyboard.Shots = []d.Shot{sh}
	_, assetBasis, _ := p.AssetMaterial(d.VisualTarget{AssetID: "asset"})
	p.Storyboard.Shots[0].VideoPrompt = &d.VideoPrompt{VideoPromptInput: d.VideoPromptInput{Parts: []d.PromptPart{{Type: "text", Text: "客厅内，固定中景，暖色侧光。手将"}, {Type: "asset", AssetID: "asset"}, {Type: "text", Text: "轻放在木桌上，松手后停顿。镜头缓慢推近，结束时停在钥匙与空出的手之间。"}}}, Basis: d.VideoPromptBasis{SceneBasis: basis, ShotInputDigest: p.ShotInputDigest(&sh), Assets: []d.AssetBasis{assetBasis}, SupportRanges: []d.SourceRange{}}, Origin: "user"}
	p.ProjectSettings = &d.ProjectSettings{AspectRatio: "16:9", ImageModel: md.MockImageModelID, VideoModel: md.MockVideoModelID}
	p.Runs = append([]d.Run{{ID: "index", Contract: d.EpisodeIndexContract, SourceVersion: 1, Status: d.RunCompleted}}, p.Runs...)
}

func (p mediaHTTPFreePrices) MediaPrice(ctx context.Context, model string) (ports.MediaPricing, error) {
	rule, version, connection, err := p.CreditPrice(ctx, model)
	profile := md.OfficialMediaProfile(connection, model)
	return ports.MediaPricing{Rule: rule, Version: version, ConnectionID: connection, Profile: profile, PriceDefaultResolution: profile.DefaultResolution}, err
}
