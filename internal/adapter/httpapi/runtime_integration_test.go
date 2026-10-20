package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	projectapp "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/testsupport/redisruntime"
)

func TestProjectRedisStreamAndProgressCacheKeepOwnerBoundaries(t *testing.T) {
	store := openProjectContentPG(t)
	runtime := redisruntime.Connect(t)
	ctx := context.Background()
	owner, other := identity.New("stream_owner"), identity.New("stream_other")
	seedPostgresFixtureUser(t, owner, ports.RoleUser)
	seedPostgresFixtureUser(t, other, ports.RoleUser)
	p := d.Project{ID: identity.New("redis_project"), OwnerID: owner, Revision: 1, Title: "原项目", Runs: []d.Run{}}
	if err := store.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	cleanProjectContentRow(t, p)
	service := &projectapp.Service{Store: store}
	host, handler := httpapi.New(service, nil, t.TempDir(), 0)
	host.Runtime = runtime
	host.RequestsPerMinute = appconfig.Defaults().Redis.RequestsPerMinute
	host.IPRequestsPerMinute = appconfig.Defaults().Redis.IPRequestsPerMinute
	host.Authenticator = &accounts.Service{Store: store}
	host.StreamHeartbeat = 50 * time.Millisecond
	host.StreamWriteTimeout = time.Second
	server := httptest.NewServer(handler)
	defer server.Close()
	defer host.Shutdown(ctx)
	request := func(path, user string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		if user != "" {
			addSessionCookie(req, postgresFixtureToken(user, accounts.Workspace))
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	path := "/api/projects/" + p.ID
	for _, test := range []struct {
		user   string
		status int
	}{{"", 401}, {other, 404}} {
		resp := request(path+"/events", test.user)
		resp.Body.Close()
		if resp.StatusCode != test.status {
			t.Fatal("stream owner check", resp.StatusCode, test.status)
		}
	}
	read := func() map[string]any {
		resp := request(path, owner)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode)
		}
		var value map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if read()["title"] != "原项目" {
		t.Fatal("initial progress projection")
	}
	stream := request(path+"/events", owner)
	defer stream.Body.Close()
	if stream.StatusCode != 200 {
		t.Fatal("owner stream", stream.StatusCode)
	}
	events := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(stream.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				events <- strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") && line != "data: {}" {
				events <- "CONTENT_LEAK"
			}
		}
	}()
	wait := func(want string) {
		t.Helper()
		select {
		case got := <-events:
			if got != want {
				t.Fatal("stream frame", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("project update missing", want)
		}
	}
	wait("project.ready")
	p.Title = "已提交的项目"
	p.Revision++
	if err := store.Commit(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.FlushRuntimeEvents(ctx); err != nil {
		t.Fatal(err)
	}
	wait("project.changed")
	value := read()
	if value["title"] != p.Title || value["revision"] != float64(p.Revision) {
		t.Fatal("stale progress cache after durable commit", value["title"], value["revision"])
	}
}
