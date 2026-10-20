package httpapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	testconfig "drama-agent/internal/testsupport/config"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/imagethumbnail"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/adapter/stylecatalog"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
	d "drama-agent/internal/domain/project"
	ud "drama-agent/internal/domain/upload"
	"drama-agent/internal/testsupport/modelregistry"
	"github.com/jackc/pgx/v5"
)

func isolatedOriginalStorage(t *testing.T) *s3objects.Store {
	t.Helper()
	filename := os.Getenv("DRAMA_TEST_CONFIG_FILE")
	if filename == "" {
		t.Skip("explicit isolated S3 opt-in")
	}
	cfg, err := appconfig.Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.Storage.Bucket, "drama-script-test") || !strings.HasPrefix(cfg.Storage.Endpoint, "http://127.0.0.1:") {
		t.Fatal("isolated loopback S3 configuration required")
	}
	s, err := s3objects.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = s.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func originalUploadService(t *testing.T) (*app.Service, *s3objects.Store) {
	t.Helper()
	store := openProjectContentPG(t)
	files := isolatedOriginalStorage(t)
	styles, err := stylecatalog.LoadStyles("../../../demo")
	if err != nil {
		t.Fatal(err)
	}
	registry := &modelregistry.Registry{Models: map[string]ports.Model{"chosen-analysis": interfaceOnlyModel{}}, OptionsValue: ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "chosen-analysis", Name: "虚构所选分析"}}, Image: []ports.ModelOption{{ID: "chosen-image", Name: "虚构所选图片"}}, Video: []ports.ModelOption{{ID: "chosen-video", Name: "虚构所选视频"}}}}
	return &app.Service{Credits: store, Prices: &frozenFixturePrices{version: 1}, Store: interfaceNoExecutionStore{store}, OriginalScripts: files, Files: &uploadapp.Service{Store: store, Objects: files, Thumbnails: imagethumbnail.FFmpeg{Settings: testconfig.Example().Media}}, Importer: scriptimport.Importer{}, Styles: styles, Models: registry, Agent: interfaceOnlyAgent{}, Authorize: app.UserInitiatedExecution}, files
}

func TestS3AtomicUploadDownloadChoicesAndManualStartWithoutModels(t *testing.T) {
	s, files := originalUploadService(t)
	ctx := context.Background()
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	host.Files = s.Files.(*uploadapp.Service)
	defer host.Shutdown(ctx)
	server := httptest.NewServer(handler)
	defer server.Close()
	settings := d.ProjectSettings{AspectRatio: "16:9", LLMModel: "chosen-analysis", ImageModel: "chosen-image", VideoModel: "chosen-video"}
	style := s.Styles[0].ProjectStyle
	upload := func(filename string, raw []byte, settingsJSON string, version string, want int) d.Project {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)

		part, err := form.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(raw)
		_ = form.Close()
		fileRes, err := http.Post(server.URL+"/api/upload", form.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		var receipt httpapi.FileView
		if fileRes.StatusCode != 201 || json.NewDecoder(fileRes.Body).Decode(&receipt) != nil {
			t.Fatal("generic upload failed")
		}
		fileRes.Body.Close()
		file, _, err := s.Files.ReadOriginal(ctx, receipt.FileID)
		if err != nil {
			t.Fatal(err)
		}
		cleanUploadedFile(t, files, file)
		basis := ""
		if want == 201 {
			basis = quoteUploadedContent(t, server.URL, receipt.FileID, json.RawMessage(settingsJSON))
		}
		createJSON, _ := json.Marshal(map[string]any{"title": "S3 虚构上传", "fileId": receipt.FileID, "settings": json.RawMessage(settingsJSON), "styleId": style.ID, "promptVersion": version, "billingBasis": basis})
		res, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(createJSON))
		if err != nil {
			t.Fatal(err)
		}
		response, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("upload %d %s", res.StatusCode, response)
		}
		if want != 201 {
			return d.Project{}
		}
		var projection struct {
			ID       string
			Revision uint64
			Settings *d.ProjectSettings
			Style    *d.ProjectStyle
		}
		if err = json.Unmarshal(response, &projection); err != nil {
			t.Fatal(err)
		}
		p, err := s.Store.Snapshot(ctx, projection.ID)
		if err != nil {
			t.Fatal(err)
		}
		cleanProjectContentRow(t, p)

		if p.Revision != 1 || projection.Revision != 1 || !reflect.DeepEqual(projection.Settings, &settings) || !reflect.DeepEqual(projection.Style, &style) || len(p.Original) != 0 || p.SourceFileID != receipt.FileID || p.OriginalFile != nil || p.UploadedAt == nil || len(p.Runs) != 1 {
			t.Fatal("upload was partial, duplicated bytes or lost choices")
		}
		index := p.LatestRun()
		if index.Contract != d.EpisodeIndexContract || index.Status != d.RunRunning || len(index.Calls) != 0 || index.Model != settings.LLMModel || index.BudgetMicroCNY != 0 || index.Credit == nil || index.Credit.Amount != 0 {
			t.Fatal("automatic index intent lost model/quote or performed work", index)
		}
		return p
	}
	settingsJSON, _ := json.Marshal(settings)
	raw := []byte("完整虚构剧本\r\n虚构小竹带怀表进入车站。\r\n全部尾部空白。  \n")
	var docx bytes.Buffer
	archive := zip.NewWriter(&docx)
	part, _ := archive.Create("word/document.xml")
	_, _ = part.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>完整虚构 DOCX 正文</w:t></w:r></w:p></w:body></w:document>`))
	_ = archive.Close()
	for _, format := range []struct {
		name string
		raw  []byte
	}{{"fiction.txt", raw}, {"fiction.md", raw}, {"虚构剧本.docx", docx.Bytes()}} {
		p := upload(format.name, format.raw, string(settingsJSON), style.PromptVersion, 201)
		hash := sha256.Sum256(format.raw)
		if p.ScriptSource.SHA256 != hex.EncodeToString(hash[:]) {
			t.Fatal("source hash does not identify original bytes")
		}
		res, err := http.Get(server.URL + "/api/projects/" + p.ID + "/source/file")
		if err != nil {
			t.Fatal(err)
		}
		read, _ := io.ReadAll(res.Body)
		res.Body.Close()
		_, parameters, _ := mime.ParseMediaType(res.Header.Get("Content-Disposition"))
		if res.StatusCode != 200 || !bytes.Equal(read, format.raw) || parameters["filename"] != format.name {
			t.Fatal("original download lost bytes/name")
		}
		reopened := openProjectContentPG(t)
		saved, err := reopened.Snapshot(ctx, p.ID)
		if err != nil || !reflect.DeepEqual(saved, p) {
			t.Fatal("reopened PG changed source/choices", err)
		}
		uploaded, _, err := s.Files.ReadOriginal(ctx, p.SourceFileID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = files.ReadFile(ctx, "different-file", uploaded.Original); err == nil {
			t.Fatal("S3 allowed foreign owner")
		}
		// Creation already accepted one index intent; a second start cannot replace it.
		code, response := postProjectContent(t, server.URL, "/api/projects/"+p.ID+"/runs", map[string]any{"revision": p.Revision})
		if code != 409 {
			t.Fatalf("duplicate start %d %s", code, response)
		}
		started, err := s.Store.Snapshot(ctx, p.ID)
		if err != nil || !reflect.DeepEqual(started, p) || started.UsedMicroCNY() != 0 {
			t.Fatal("duplicate start changed intent or created costs", err)
		}
		code, _ = postProjectContent(t, server.URL, "/api/projects/"+p.ID+"/cancel", map[string]any{"revision": started.Revision})
		if code != 200 {
			t.Fatal("cancel failed")
		}
		started, _ = s.Store.Snapshot(ctx, p.ID)
		code, _ = postProjectContent(t, server.URL, "/api/projects/"+p.ID+"/delete", map[string]any{"revision": started.Revision})
		if code != 200 {
			t.Fatal("delete failed")
		}
		res, err = http.Get(server.URL + "/api/projects/" + p.ID + "/source/file")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatal("deleted original publicly readable")
		}
		_, read, err = s.Files.ReadOriginal(ctx, p.SourceFileID)
		if err != nil || !bytes.Equal(read, format.raw) {
			t.Fatal("deletion removed archival bytes")
		}
	}
	t.Run("manual-generation-zero-budget-projection", func(t *testing.T) {
		// An existing pre-index project still supports its original explicit start.
		manual, err := s.ForActor(legacyHTTPActor).CreateWithChoices(ctx, "Existing project", "manual.txt", raw, 0, app.CreationChoices{Settings: &settings, StyleID: style.ID, PromptVersion: style.PromptVersion})
		if err != nil {
			t.Fatal(err)
		}
		cleanProjectContentRow(t, manual)
		basis := quoteProjectContent(t, server.URL, manual.ID, app.QuoteInput{Revision: manual.Revision, Operation: "asset-preparation"})
		code, response := postProjectContent(t, server.URL, "/api/projects/"+manual.ID+"/runs", map[string]any{"revision": manual.Revision, "billingBasis": basis})
		var projected struct{ Run httpapi.RunView }
		if code != 202 || json.Unmarshal(response, &projected) != nil || projected.Run.BudgetMicroCNY == nil || *projected.Run.BudgetMicroCNY != 0 || projected.Run.Model != settings.LLMModel {
			t.Fatalf("manual zero ceiling was omitted or projected incorrectly: %d %s", code, response)
		}
		started, err := s.Store.Snapshot(ctx, manual.ID)
		if err != nil || started.LatestRun().Model != settings.LLMModel || started.LatestRun().BudgetMicroCNY != 0 || len(started.LatestRun().Calls) != 0 || started.UsedMicroCNY() != 0 {
			t.Fatal("manual start used a fallback or created model costs", err)
		}
		if code, body := postProjectContent(t, server.URL, "/api/projects/"+manual.ID+"/cancel", map[string]any{"revision": started.Revision}); code != 200 {
			t.Fatalf("manual cancellation: %d %s", code, body)
		}
	})
	before, _ := s.Store.ListSummaries(ctx)
	upload("invalid.txt", raw, `{"aspectRatio":"16:9","llmModel":"unknown","imageModel":"","videoModel":""}`, style.PromptVersion, 400)
	upload("invalid.txt", raw, string(settingsJSON), "stale-version", 409)
	upload("invalid.txt", raw, `{"llmModel":"chosen-analysis","extra":true}`, style.PromptVersion, 400)
	after, _ := s.Store.ListSummaries(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid choices partially created a project")
	}
	t.Log("TXT/MD/DOCX original bytes, SHA256, selected settings/style, one PG Create, restart readback, automatic index intent/duplicate-start rejection, cancel and deletion verified; Model/Agent execution forbidden")
}

func TestS3GeneralUploadThumbnailAndSourceReferenceRestart(t *testing.T) {
	s, objects := originalUploadService(t)
	ctx := context.Background()
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	host.Files = s.Files.(*uploadapp.Service)
	defer host.Shutdown(ctx)
	server := httptest.NewServer(handler)
	defer server.Close()
	upload := func(name string, raw []byte, fields map[string]string) ud.File {
		t.Helper()
		body, kind := multipartFile(t, name, raw, fields)
		response, err := http.Post(server.URL+"/api/upload", kind, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var receipt httpapi.FileView
		if response.StatusCode != 201 || json.NewDecoder(response.Body).Decode(&receipt) != nil {
			t.Fatal("upload receipt failed", response.StatusCode)
		}
		file, read, err := host.Files.ReadOriginal(ctx, receipt.FileID)
		if err != nil || !bytes.Equal(read, raw) {
			t.Fatal("uploaded S3 original lost bytes", err)
		}
		cleanUploadedFile(t, objects, file)
		freshPG := openProjectContentPG(t)
		cfg, err := appconfig.Load(os.Getenv("DRAMA_TEST_CONFIG_FILE"))
		if err != nil {
			t.Fatal(err)
		}
		freshS3, err := s3objects.New(cfg.Storage)
		if err != nil {
			t.Fatal(err)
		}
		reopened := &uploadapp.Service{Store: freshPG, Objects: freshS3}
		metadata, bodyRead, err := reopened.ReadOriginal(ctx, file.ID)
		if err != nil || !reflect.DeepEqual(metadata, file) || !bytes.Equal(bodyRead, raw) {
			t.Fatal("restart lost file bytes/metadata", err)
		}
		if file.Thumbnail != nil {
			_, thumb, err := reopened.Read(ctx, file.ID, true)
			if err != nil || http.DetectContentType(thumb) != "image/webp" || receipt.ThumbnailURL == nil {
				t.Fatal("restart lost S3 thumbnail", err)
			}
			res, err := http.Get(server.URL + *receipt.ThumbnailURL)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 || !bytes.Equal(actual, thumb) {
				t.Fatal("thumbnail API differs from S3", err)
			}
		} else if receipt.ThumbnailURL != nil {
			t.Fatal("thumbnail invented")
		}
		return file
	}
	im := image.NewNRGBA(image.Rect(0, 0, 640, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 640; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 200, G: 30, B: 10, A: 120})
		}
	}
	var img bytes.Buffer
	if err := png.Encode(&img, im); err != nil {
		t.Fatal(err)
	}
	preview := upload("参考图.png", img.Bytes(), map[string]string{"thumbnail": "true", "thumbnailMaxWidth": "180", "thumbnailMaxHeight": "120"})
	if preview.Thumbnail.Width != 180 || preview.Thumbnail.Height != 112 {
		t.Fatal("actual thumbnail dimensions", preview.Thumbnail)
	}
	_ = upload("opaque.arbitrary", []byte{0xff, 0, 3, 4, 0x90}, nil)
	first := upload("complete.txt", []byte("完整虚构剧本\r\n保留尾部空白。  \n"), nil)
	code, body := postProjectContent(t, server.URL, "/api/projects", map[string]any{"fileId": first.ID, "title": "通用上传引用", "billingBasis": quoteUploadedContent(t, server.URL, first.ID, d.ProjectSettings{})})
	if code != 201 {
		t.Fatalf("create %d %s", code, body)
	}
	var pView struct{ ID string }
	if err := json.Unmarshal(body, &pView); err != nil {
		t.Fatal(err)
	}
	p, err := s.Store.Snapshot(ctx, pView.ID)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	before := d.Clone(p)
	second := upload("replacement.md", []byte("完整虚构换源正文。\n"), nil)
	code, body = postProjectContent(t, server.URL, "/api/projects/"+p.ID+"/source", map[string]any{"fileId": second.ID, "revision": p.Revision, "billingBasis": quoteProjectContent(t, server.URL, p.ID, app.QuoteInput{Revision: p.Revision, Operation: "replace-source", FileID: second.ID})})
	if code != 200 {
		t.Fatalf("replace %d %s", code, body)
	}
	p, err = s.Store.Snapshot(ctx, p.ID)
	if err != nil || p.ScriptSource.Version != 2 || p.SourceFileID != second.ID || len(p.Original) != 0 || p.OriginalFile != nil || !p.UploadedAt.Equal(*before.UploadedAt) {
		t.Fatal("source reference adoption changed unrelated state", err)
	}
	code, _ = postProjectContent(t, server.URL, "/api/projects/"+p.ID+"/source", map[string]any{"fileId": first.ID, "revision": before.Revision})
	if code != 409 {
		t.Fatal("stale replace accepted")
	}
	if _, read, err := host.Files.ReadOriginal(ctx, first.ID); err != nil || len(read) == 0 {
		t.Fatal("source replace deleted original upload", err)
	}
	code, _ = postProjectContent(t, server.URL, "/api/projects", map[string]any{"fileId": preview.ID, "title": "image cannot be a script"})
	if code != 400 {
		t.Fatal("script business accepted image")
	}
	legacy, kind := multipartFile(t, "old.txt", []byte("old request"), nil)
	res, err := http.Post(server.URL+"/api/projects", kind, bytes.NewReader(legacy))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatal("obsolete multipart project contract still accepted")
	}
	if p.UsedMicroCNY() != 0 || len(p.Runs) != 2 {
		t.Fatal("source adoption lost index intents or created costs")
	}
	for _, run := range p.Runs {
		if run.Contract != d.EpisodeIndexContract || len(run.Calls) != 0 {
			t.Fatal("source adoption performed model work", run)
		}
	}
	t.Log("real PG/S3 raw file, WebP, API exact-byte readback, fresh adapter readback, source reference/replace/stale rejection and image/script boundaries verified; no model calls")
}

// Temporary browser uses production HTTP/PG/S3 paths. Execution acquisition is
// unavailable and both Model.Generate and Agent.Run panic if accidentally used.
func TestS3UploadBrowserWithoutModels(t *testing.T) {
	if os.Getenv("DRAMA_S3_UI_ADDR") == "" {
		t.Skip("browser fixture opt-in")
	}
	if os.Getenv("DRAMA_S3_UI_ADDR") != "127.0.0.1:8793" {
		t.Fatal("isolated loopback required")
	}
	dir := os.Getenv("DRAMA_S3_UI_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("evidence directory required")
	}
	s, files := originalUploadService(t)
	ctx := context.Background()
	p, err := s.ForActor(legacyHTTPActor).Create(ctx, "S3 接口 · 虚构原剧本", "fiction.txt", []byte("完整虚构剧本。\n小竹在车站拾起怀表。\n"), 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	p.Assets = pgAssets()
	p.Runs = []d.Run{{ID: "fixture-p3", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "completed"}}
	p.Revision++
	if err = s.Store.Commit(ctx, p, p.Revision-1); err != nil {
		t.Fatal(err)
	}
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	host.Files = s.Files.(*uploadapp.Service)
	defer host.Shutdown(ctx)
	server := &http.Server{Addr: "127.0.0.1:8793", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() {
		if e := server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			t.Error(e)
		}
	}()
	metadata, _ := json.Marshal(map[string]string{"url": "http://127.0.0.1:8793/demo/app/#/projects", "projectId": p.ID})
	if err = os.WriteFile(filepath.Join(dir, "browser-fixture.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("isolated PG/S3 browser ready; no Model/Agent execution possible")
	deadline := time.Now().Add(12 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err = os.Stat(filepath.Join(dir, "browser-stop")); err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err = host.WaitUntilIdle(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Store.ListSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range rows {
		row, err := s.Store.Snapshot(ctx, summary.ID)
		if err != nil {
			t.Fatal(err)
		}
		cleanProjectContentRow(t, row)
		if row.SourceFileID != "" {
			file, _, err := s.Files.ReadOriginal(ctx, row.SourceFileID)
			if err != nil {
				t.Error(err)
			} else {
				cleanUploadedFile(t, files, file)
			}
		}

		for _, run := range row.Runs {
			if len(run.Calls) > 0 {
				t.Error("browser created a model call")
			}
		}
	}
}

// Cleanup is limited to the precise records and objects created by this test.
func cleanUploadedFile(t *testing.T, objects *s3objects.Store, file ud.File) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, os.Getenv("DRAMA_TEST_DATABASE_URL"))
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(ctx)
		if _, err = conn.Exec(ctx, "DELETE FROM drama_catalog_v1.uploaded_files WHERE id=$1", file.ID); err != nil {
			t.Error(err)
			return
		}
		if err = objects.RemoveFile(ctx, file.ID, file.Original); err != nil {
			t.Error(err)
		}
		if file.Thumbnail != nil {
			if err = objects.RemoveFile(ctx, file.ID, file.Thumbnail.Blob); err != nil {
				t.Error(err)
			}
		}
	})
}
