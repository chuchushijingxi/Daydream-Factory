package s3objects

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/domain/identity"
	pd "drama-agent/internal/domain/project"
	d "drama-agent/internal/domain/upload"
)

// Standard S3 wire fixture, independent of any vendor implementation.
func TestS3ProtocolIntegrityAndUploadIsolation(t *testing.T) {
	var mu sync.Mutex
	objects := map[string][]byte{}
	corrupt, rejectPut := false, false
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") || !strings.Contains(r.Header.Get("Authorization"), "/test-region/s3/aws4_request") || r.Header.Get("X-Amz-Security-Token") != "fictional-session" {
			t.Error("request is not standard signed S3")
		}
		switch r.Method {
		case "HEAD":
			w.WriteHeader(200)
		case "PUT":
			raw, _ := io.ReadAll(r.Body)
			hash := md5.Sum(raw)
			if r.Header.Get("Content-MD5") != base64.StdEncoding.EncodeToString(hash[:]) {
				t.Error("upload checksum missing")
			}
			objects[r.URL.Path] = raw
			if rejectPut {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`<Error><Code>AccessDenied</Code></Error>`))
				return
			}
			w.Header().Set("ETag", fmt.Sprintf(`"%x"`, hash))
			w.WriteHeader(200)
		case "GET":
			raw, ok := objects[r.URL.Path]
			if !ok {
				w.WriteHeader(404)
				return
			}
			raw = append([]byte(nil), raw...)
			if corrupt {
				raw[0] ^= 1
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
			w.Header().Set("Last-Modified", "Wed, 14 Oct 2026 00:00:00 GMT")
			w.Header().Set("ETag", `"fictional-etag"`)
			_, _ = w.Write(raw)
		case "DELETE":
			delete(objects, r.URL.Path)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected S3 operation %s", r.Method)
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	cfg := appconfig.Storage{Endpoint: server.URL, Region: "test-region", Bucket: "test-bucket", AccessKey: "fictional-access", SecretKey: "fictional-secret", SessionToken: "fictional-session", PathStyle: true}
	store, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = store.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	raw := []byte("完整虚构原文\r\n保留空白与结尾。  \n")
	id := identity.New("file")
	file, err := store.PutFile(ctx, id, "original", "text/markdown", raw)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh adapter can read the same immutable coordinates after restart.
	reopened, _ := New(cfg)
	read, err := reopened.ReadFile(ctx, id, file)
	if err != nil || !bytes.Equal(read, raw) {
		t.Fatal("saved bytes differ", err)
	}
	mu.Lock()
	before := requests
	mu.Unlock()
	for _, foreign := range []d.Blob{{Bucket: "another", ObjectKey: file.ObjectKey, SizeBytes: file.SizeBytes}, {Bucket: file.Bucket, ObjectKey: "uploads/" + id + "/../other", SizeBytes: file.SizeBytes}} {
		if _, err = store.ReadFile(ctx, id, foreign); err == nil {
			t.Fatal("foreign coordinates accepted")
		}
	}
	if _, err = store.ReadFile(ctx, identity.New("file"), file); err == nil {
		t.Fatal("cross-project access accepted")
	}
	mu.Lock()
	if requests != before {
		t.Error("invalid coordinates contacted S3")
	}
	corrupt = true
	mu.Unlock()
	failedID := identity.New("file")
	failed, err := store.PutFile(ctx, failedID, "original", "text/plain", raw)
	if err == nil {
		t.Fatal("corrupt upload accepted")
	}
	if err = store.RemoveFile(ctx, failedID, failed); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(objects) != 1 {
		t.Error("failed upload left an orphan or removed a saved object")
	}
	corrupt = false
	rejectPut = true
	mu.Unlock()
	failedID = identity.New("file")
	failed, err = store.PutFile(ctx, failedID, "original", "text/plain", raw)
	if err == nil {
		t.Fatal("failed PUT accepted")
	}
	if err = store.RemoveFile(ctx, failedID, failed); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(objects) != 1 {
		t.Error("failed PUT cleanup is not isolated")
	}
	rejectPut = false
	mu.Unlock()
	// Preserved historical project coordinates still read without migration.
	historical := pd.OriginalFile{Bucket: cfg.Bucket, ObjectKey: "projects/project_old/scripts/1/original_fixture", SizeBytes: int64(len(raw)), ContentType: "text/plain"}
	mu.Lock()
	objects["/"+cfg.Bucket+"/"+historical.ObjectKey] = raw
	mu.Unlock()
	if read, err = store.Read(ctx, "project_old", historical); err != nil || !bytes.Equal(read, raw) {
		t.Fatal("historical original lost", err)
	}
	if _, err = store.Read(ctx, "foreign", historical); err == nil {
		t.Fatal("historical owner check lost")
	}
	if err = store.RemoveFile(ctx, id, file); err != nil {
		t.Fatal(err)
	}
}

func TestS3EndpointAndBounds(t *testing.T) {
	cfg := appconfig.Storage{Endpoint: "https://s3.example.com", Region: "us-east-1", Bucket: "scripts", AccessKey: "fictional", SecretKey: "fictional"}
	store, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PutFile(context.Background(), "../other", "original", "text/plain", []byte("x")); err == nil {
		t.Fatal("invalid project accepted")
	}
	if _, err = store.PutFile(context.Background(), identity.New("file"), "original", "text/plain", make([]byte, d.MaxFileBytes+1)); err == nil {
		t.Fatal("oversize upload accepted")
	}
	for _, endpoint := range []string{"ftp://example.com", "https://key:secret@example.com", "https://example.com/private", "https://example.com?key=secret"} {
		cfg.Endpoint = endpoint
		if _, err = New(cfg); err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
}
