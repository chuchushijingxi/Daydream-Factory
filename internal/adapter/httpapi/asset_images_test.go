package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"drama-agent/internal/adapter/httpapi"
	app "drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
	d "drama-agent/internal/domain/project"
)

func TestAssetImagesPGS3RoundtripWithoutModels(t *testing.T) {
	s, objects := originalUploadService(t)
	ctx := context.Background()
	p := createProjectContent(t, s, pgAssets())
	before := d.Clone(p)
	other := createProjectContent(t, s, pgAssets())
	host, handler := httpapi.New(s, nil, "../../../demo", 0)
	handler = withPostgresSession(t, host, handler, "workspace")
	host.Files = s.Files.(*uploadapp.Service)
	defer host.Shutdown(ctx)
	server := httptest.NewServer(handler)
	defer server.Close()
	img := image.NewNRGBA(image.Rect(0, 0, 480, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 480; x++ {
			img.Set(x, y, color.NRGBA{R: 100, G: uint8(x), B: 150, A: 200})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	uploaded := []httpapi.FileView{}
	for _, suffix := range []string{"/assets/c/image", "/assets/c/looks/l/image", "/assets/loc/image", "/assets/prop/image"} {
		body, kind := multipartFile(t, "虚构图片.png", encoded.Bytes(), map[string]string{"thumbnail": "true"})
		response, err := http.Post(server.URL+"/api/upload", kind, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var receipt httpapi.FileView
		if response.StatusCode != 201 || json.NewDecoder(response.Body).Decode(&receipt) != nil {
			t.Fatal("upload failed", response.StatusCode)
		}
		response.Body.Close()
		file, err := s.Files.File(ctx, receipt.FileID)
		if err != nil {
			t.Fatal(err)
		}
		cleanUploadedFile(t, objects, file)
		uploaded = append(uploaded, receipt)
		code, raw := postProjectContent(t, server.URL, "/api/projects/"+p.ID+suffix, map[string]any{"revision": p.Revision, "fileId": receipt.FileID})
		if code != 200 {
			t.Fatal(code, string(raw))
		}
		p, err = s.Project(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		var view struct {
			Revision uint64 `json:"revision"`
		}
		if err = json.Unmarshal(raw, &view); err != nil {
			t.Fatal(err)
		}
		if view.Revision != p.Revision {
			t.Fatal("response revision disagrees with PG")
		}
		response, err = http.Get(server.URL + receipt.OriginalURL)
		if err != nil {
			t.Fatal(err)
		}
		original, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if !bytes.Equal(original, encoded.Bytes()) {
			t.Fatal("original bytes changed")
		}
		response, err = http.Get(server.URL + *receipt.ThumbnailURL)
		if err != nil {
			t.Fatal(err)
		}
		thumb, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || response.Header.Get("Content-Type") != "image/webp" || len(thumb) < 12 || !bytes.Equal(thumb[8:12], []byte("WEBP")) {
			t.Fatal("thumbnail is not real WebP")
		}
	}
	reopened := openProjectContentPG(t)
	fresh := &app.Service{Store: reopened}
	restored, err := fresh.Project(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Asset("c").ImageFileID != uploaded[0].FileID || restored.Asset("c").Looks[0].ImageFileID != uploaded[1].FileID || restored.Asset("loc").ImageFileID != uploaded[2].FileID || restored.Asset("prop").ImageFileID != uploaded[3].FileID {
		t.Fatal("fresh PG read lost association")
	}
	unchanged := d.Clone(restored)
	unchanged.Revision = before.Revision
	unchanged.CatalogEditedByUser = before.CatalogEditedByUser
	for i := range unchanged.Assets {
		unchanged.Assets[i].ImageFileID = ""
		for j := range unchanged.Assets[i].Looks {
			unchanged.Assets[i].Looks[j].ImageFileID = ""
		}
	}
	if !reflect.DeepEqual(unchanged, before) {
		t.Fatal("image write changed facts, task history or unknown fees")
	}
	foreign, _ := fresh.Project(ctx, other.ID)
	if !reflect.DeepEqual(foreign, other) {
		t.Fatal("other project changed")
	}
	path := "/api/projects/" + p.ID + "/assets/c/image"
	for _, bad := range []struct {
		suffix string
		body   any
		code   int
	}{
		{path, map[string]any{"revision": before.Revision, "fileId": uploaded[0].FileID}, 409},
		{path, map[string]any{"revision": p.Revision, "fileId": uploaded[0].FileID, "imageUrl": "foreign"}, 400},
		{"/api/projects/" + p.ID + "/assets/loc/looks/l/image", map[string]any{"revision": p.Revision, "fileId": uploaded[0].FileID}, 400},
		{"/api/projects/" + p.ID + "/assets/c/looks/missing/image", map[string]any{"revision": p.Revision, "fileId": uploaded[0].FileID}, 404},
	} {
		code, raw := postProjectContent(t, server.URL, bad.suffix, bad.body)
		if code != bad.code {
			t.Fatal(code, string(raw))
		}
	}
	host.ReadOnly = true
	code, _ := postProjectContent(t, server.URL, path, map[string]any{"revision": p.Revision, "fileId": uploaded[0].FileID})
	if code != 403 {
		t.Fatal("readonly bypassed")
	}
	after, _ := fresh.Project(ctx, p.ID)
	if !reflect.DeepEqual(after, restored) {
		t.Fatal("rejected writes changed PG")
	}
	t.Log("4 target IDs, real original/WebP bytes, fresh PG read, revision/read-only/isolation and unchanged text/history/unknown reserves verified; zero model executions")
}
