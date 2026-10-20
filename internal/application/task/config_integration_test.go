package task_test

import (
	"context"
	"drama-agent/internal/testsupport/redisruntime"
	"testing"
	"time"

	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/adapter/taskruntime"
	"drama-agent/internal/domain/identity"
	testconfig "drama-agent/internal/testsupport/config"
)

func TestConfiguredPolicyIsFrozenAcrossRestart(t *testing.T) {
	fixtureService, store := fixture(t)
	objects := fixtureService.Results.(*s3objects.TaskResults).Objects
	cfg := testconfig.Example()
	cfg.Tasks.PollInterval, cfg.Tasks.RetryInterval = 1700*time.Millisecond, 2300*time.Millisecond
	cfg.Tasks.ObserveTimeout, cfg.Tasks.SaveTimeout, cfg.Tasks.MaxSubmissions = 3*time.Minute, 6*time.Minute, 2
	r, q, err := taskruntime.New(cfg, store, objects, resolver{}, redisruntime.ConnectDatabase(t, cfg.Database.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	r.Service.Queue = &noQueue{}
	cmd := command(identity.New("config_scope"), "image", 0)
	started := time.Now()
	v := start(t, r.Service, cmd)
	saved, err := r.Service.Store.Task(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := saved.State.Policy
	if p.PollMillis != 1700 || p.RetryMillis != 2300 || p.MaxSubmissions != 2 || p.ObserveUntil.Before(started.Add(3*time.Minute)) || p.ObserveUntil.After(time.Now().Add(3*time.Minute)) || p.SaveUntil.Before(started.Add(6*time.Minute)) {
		t.Fatal("configured policy was not persisted")
	}
	cfg.Tasks.PollInterval, cfg.Tasks.MaxSubmissions = 3100*time.Millisecond, 3
	restarted, q2, err := taskruntime.New(cfg, store, objects, resolver{}, redisruntime.ConnectDatabase(t, cfg.Database.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer q2.Close()
	restarted.Service.Queue = &noQueue{}
	again := start(t, restarted.Service, cmd)
	read, err := restarted.Service.Store.Task(context.Background(), again.ID)
	if err != nil || again.ID != v.ID || read.State.Policy != p {
		t.Fatal("restart reset an existing task policy", err)
	}
	cmd.CommandID = identity.New("config_command")
	newTask := start(t, restarted.Service, cmd)
	read, err = restarted.Service.Store.Task(context.Background(), newTask.ID)
	if err != nil || read.State.Policy.PollMillis != 3100 || read.State.Policy.MaxSubmissions != 3 {
		t.Fatal("new task ignored the changed configuration", err)
	}
}
