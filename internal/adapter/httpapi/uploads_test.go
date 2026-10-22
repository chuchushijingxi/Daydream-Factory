package httpapi_test

import (
	"bytes"
	"context"
	testconfig "drama-agent/internal/testsupport/config"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/imagethumbnail"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	pd "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/uploadfiles"
)

type fileProjectStore struct {
	ports.ProjectStore
	project pd.Project
}

func (s *fileProjectStore) Create(ctx context.Context, p pd.Project) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.project = pd.Clone(p)
	return nil
}
func (s *fileProjectStore) Snapshot(_ context.Context, id string) (pd.Project, error) {
	if s.project.ID != id {
		return pd.Project{}, fault.ErrNotFound
	}
	return pd.Clone(s.project), nil
}
func (s *fileProjectStore) Commit(ctx context.Context, p pd.Project, revision uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.project.ID != p.ID || s.project.Revision != revision {
		return fault.ErrConflict
	}
	s.project = pd.Clone(p)
	return nil
}

func TestScriptHTTPUsesFileIDWithOptionalPreferencesAndRejectsOldMultipart(t *testing.T) {
	files := uploadfiles.New()
	store := &fileProjectStore{}
	service := &app.Service{Store: store, Files: files, Importer: scriptimport.Importer{}}
	host, handler := httpapi.New(configuredFixture(service), nil, "../../../demo", 0)
	handler = authenticatedFixture(host, handler)
	host.Files = files
	defer host.Shutdown(context.Background())
	body, kind := multipartFile(t, "完整剧本.txt", []byte("完整虚构原文。\r\n首尾不丢。  \n"), nil)
	r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/upload", bytes.NewReader(body))
	r.Header.Set("Content-Type", kind)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var uploaded httpapi.FileView
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &uploaded) != nil {
		t.Fatal("upload failed", w.Code, w.Body.String())
	}
	input, _ := json.Marshal(map[string]any{"fileId": uploaded.FileID, "title": "only required creation fields"})
	r = httptest.NewRequest("POST", "http://127.0.0.1:8787/api/projects", bytes.NewReader(input))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 201 || store.project.SourceFileID != uploaded.FileID || len(store.project.Original) != 0 || store.project.Style != nil || store.project.ProjectSettings != nil || len(store.project.Runs) != 1 || store.project.Runs[0].Contract != d.EpisodeIndexContract || store.project.Runs[0].Status != d.RunFailed {
		t.Fatal("optional preferences became required or project duplicated original", w.Code, w.Body.String())
	}
	input, _ = json.Marshal(map[string]any{"fileId": uploaded.FileID, "revision": 1})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1:8787/api/projects/"+store.project.ID+"/source", bytes.NewReader(input)))
	if w.Code != 200 || store.project.ScriptSource.Version != 2 {
		t.Fatal("JSON source replacement failed", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "http://127.0.0.1:8787/api/projects", bytes.NewReader(body))
	r.Header.Set("Content-Type", kind)
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("obsolete project multipart accepted")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1:8787/api/projects", strings.NewReader(`{"title":"missing ID"}`)))
	if w.Code != 400 {
		t.Fatal("missing file ID accepted")
	}
}

func multipartFile(t *testing.T, name string, raw []byte, fields map[string]string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := form.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), form.FormDataContentType()
}

func TestGeneralFileHTTPAnyFormatRangeAndPrivateAddresses(t *testing.T) {
	files := uploadfiles.New()
	host, handler := httpapi.New(&app.Service{}, nil, "../../../demo", 0)
	handler = authenticatedFixture(host, handler)
	host.Files = files
	defer host.Shutdown(context.Background())
	for _, item := range []struct {
		name string
		raw  []byte
	}{{"zero.empty", nil}, {"opaque.custom", []byte{0xff, 0, 0x11, 0x90}}, {"完整正文.txt", []byte("Complete source\r\n  \n")}, {"page.html", []byte("<!DOCTYPE html><script>window.executed=true</script>")}, {"drawing.svg", []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>")}} {
		body, kind := multipartFile(t, item.name, item.raw, nil)
		request := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/upload", bytes.NewReader(body))
		request.Header.Set("Content-Type", kind)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		var receipt httpapi.FileView
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.ThumbnailURL != nil || receipt.Filename != item.name || receipt.SizeBytes != int64(len(item.raw)) {
			t.Fatal("upload receipt", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "bucket") || strings.Contains(w.Body.String(), "objectKey") {
			t.Fatal("private coordinates exposed")
		}
		for _, suffix := range []string{"", "/original"} {
			read := httptest.NewRecorder()
			handler.ServeHTTP(read, httptest.NewRequest("GET", "http://127.0.0.1:8787/api/files/"+receipt.FileID+suffix, nil))
			if read.Code != 200 {
				t.Fatal("file read failed", read.Code, read.Body.String())
			}
			if suffix != "" {
				if !bytes.Equal(read.Body.Bytes(), item.raw) {
					t.Fatal("original bytes changed")
				}
				disposition, params, err := mime.ParseMediaType(read.Header().Get("Content-Disposition"))
				if err != nil || params["filename"] != item.name || disposition != "attachment" {
					t.Fatal("unsafe/unusable original disposition", read.Header())
				}
			}
		}
		if len(item.raw) > 3 {
			r := httptest.NewRequest("GET", "http://127.0.0.1:8787"+receipt.OriginalURL, nil)
			r.Header.Set("Range", "bytes=1-2")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), item.raw[1:3]) {
				t.Fatal("Range lost", w.Code)
			}
		}
		read := httptest.NewRecorder()
		handler.ServeHTTP(read, httptest.NewRequest("GET", "http://127.0.0.1:8787/api/files/"+receipt.FileID+"/thumbnail", nil))
		if read.Code != 404 {
			t.Fatal("missing thumbnail invented")
		}
	}
	for _, url := range []string{"/api/files/not-an-id", "/api/files/file_" + strings.Repeat("b", 32) + "/original"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8787"+url, nil))
		if w.Code != 404 {
			t.Fatal("invalid/missing file accepted")
		}
	}
}

func TestImageUploadActualOptionalThumbnailAndRejectedOptions(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("local image tools unavailable")
		}
	}
	files := uploadfiles.New()
	files.Thumbnails = imagethumbnail.FFmpeg{Settings: testconfig.Example().Media}
	host, handler := httpapi.New(&app.Service{}, nil, "../../../demo", 0)
	handler = authenticatedFixture(host, handler)
	host.Files = files
	defer host.Shutdown(context.Background())
	img := image.NewNRGBA(image.Rect(0, 0, 600, 300))
	img.SetNRGBA(0, 0, color.NRGBA{R: 200, A: 128})
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	body, kind := multipartFile(t, "reference.png", raw.Bytes(), map[string]string{"thumbnail": "true", "thumbnailMaxWidth": "180", "thumbnailMaxHeight": "100"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/upload", bytes.NewReader(body))
	r.Header.Set("Content-Type", kind)
	handler.ServeHTTP(w, r)
	var receipt httpapi.FileView
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.ThumbnailURL == nil {
		t.Fatal("thumbnail upload", w.Code, w.Body.String())
	}
	file, original, err := files.ReadOriginal(context.Background(), receipt.FileID)
	if err != nil || !bytes.Equal(original, raw.Bytes()) || file.Thumbnail.Width != 180 || file.Thumbnail.Height != 90 {
		t.Fatal("original/thumbnail metadata differs", err)
	}
	originalHTTP := httptest.NewRecorder()
	handler.ServeHTTP(originalHTTP, httptest.NewRequest("GET", "http://127.0.0.1:8787"+receipt.OriginalURL, nil))
	if originalHTTP.Code != 200 || originalHTTP.Header().Get("Content-Type") != "image/png" || !bytes.Equal(originalHTTP.Body.Bytes(), raw.Bytes()) {
		t.Fatal("original URL did not preserve the uploaded full image")
	}
	thumb := httptest.NewRecorder()
	handler.ServeHTTP(thumb, httptest.NewRequest("GET", "http://127.0.0.1:8787"+*receipt.ThumbnailURL, nil))
	if thumb.Code != 200 || http.DetectContentType(thumb.Body.Bytes()) != "image/webp" || thumb.Header().Get("Content-Type") != "image/webp" {
		t.Fatal("thumbnail access failed")
	}
	for _, fields := range []map[string]string{{"thumbnail": "yes"}, {"thumbnailMaxWidth": "320"}, {"thumbnail": "true", "thumbnailMaxWidth": "0"}, {"thumbnail": "true", "thumbnailMaxHeight": "4097"}, {"extra": "1"}, {"thumbnail": "true"}} {
		body, kind := multipartFile(t, "not-image.bin", []byte("opaque contents"), fields)
		r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/upload", bytes.NewReader(body))
		r.Header.Set("Content-Type", kind)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("invalid thumbnail options accepted", fields, w.Code, w.Body.String())
		}
	}
	mem := files.Store.(*uploadfiles.Memory)
	if len(mem.Files) != 1 || len(mem.Objects) != 2 {
		t.Fatal("rejected thumbnail wrote partial records")
	}
	for _, boundary := range []string{"readonly", "origin", "host"} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/upload", bytes.NewReader(body))
		r.Header.Set("Content-Type", kind)
		switch boundary {
		case "readonly":
			host.ReadOnly = true
		case "origin":
			r.Header.Set("Origin", "https://foreign.invalid")
		case "host":
			r.Host = "foreign.invalid"
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		host.ReadOnly = false
		if w.Code != 403 {
			t.Fatal("write boundary ignored", boundary, w.Code)
		}
	}
	if len(mem.Files) != 1 || len(mem.Objects) != 2 {
		t.Fatal("forbidden upload wrote data")
	}
}
