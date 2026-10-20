package model_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	modeladapter "drama-agent/internal/adapter/model"
)

func TestExplicitModelDirectoryTransport(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer dummy-directory" {
			t.Error("wrong method/path/key")
		}
		io.WriteString(w, `{"data":[{"id":"text-id","owned_by":"some-provider"},{"id":"image-id"},{"id":"text-id"}]}`)
	}))
	defer server.Close()
	models, e := modeladapter.FetchModelDirectory(context.Background(), "dummy-directory", server.URL+"/v1", "openai-chat")
	if e != nil || len(models) != 2 || requests.Load() != 1 || models[1].ModelID != "image-id" {
		t.Fatal(models, e)
	}
}

func TestDirectoryRejectsRedirectAndInvalidSuccessBody(t *testing.T) {
	var forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer destination.Close()
	for _, body := range []string{"redirect", `{"choices":[]}`, `{"data":[{"id":""}]}`, `{"data":null}`, `{"data":[]} trailing`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if body == "redirect" {
				http.Redirect(w, r, destination.URL, 302)
				return
			}
			io.WriteString(w, body)
		}))
		_, e := modeladapter.FetchModelDirectory(context.Background(), "dummy-directory", server.URL, "openai-chat")
		server.Close()
		if e == nil {
			t.Fatal("invalid directory accepted", body)
		}
	}
	if forwarded.Load() != 0 {
		t.Fatal("key forwarded to redirect")
	}
}
