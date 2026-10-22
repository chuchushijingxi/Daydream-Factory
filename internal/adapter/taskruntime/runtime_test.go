package taskruntime_test

import (
	"context"
	"drama-agent/internal/adapter/mediatools"
	"drama-agent/internal/application/ports"
	d "drama-agent/internal/domain/task"
	"testing"
	"time"

	"drama-agent/internal/adapter/mockmedia"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/adapter/taskruntime"
	testconfig "drama-agent/internal/testsupport/config"
)

func TestAPIAndWorkerAssemblyUsesExplicitSettings(t *testing.T) {
	c := testconfig.Example()
	c.Tasks.Lease, c.Tasks.PollInterval = 27*time.Second, 1700*time.Millisecond
	c.Tasks.ScanBatch, c.Tasks.ScanMaxBatches = 17, 2
	c.Tasks.SubmitConcurrency, c.Tasks.QueryConcurrency, c.Tasks.SaveConcurrency = 2, 3, 1
	c.Media.FFprobe = "/explicit/tools/ffprobe"
	c.Media.FFmpeg = "/explicit/tools/ffmpeg"
	c.NSQ.Topic, c.NSQ.Channel, c.NSQ.Concurrency = "configured_topic", "configured_channel", 7
	r, q, err := taskruntime.New(c, nil, nil, nil, assemblyCapacity{})
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if q.Topic != c.NSQ.Topic || q.Channel != c.NSQ.Channel || q.Concurrency != 7 || r.Batch != 17 || r.MaxBatches != 2 || r.ScanInterval != c.Tasks.ScanInterval {
		t.Fatal("queue/recovery settings ignored")
	}
	s := r.Service
	if s.Lease != c.Tasks.Lease || s.Policy.PollMillis != 1700 || s.Policy.MaxSubmissions != c.Tasks.MaxSubmissions || s.ObserveTimeout != c.Tasks.ObserveTimeout || s.SaveTimeout != c.Tasks.SaveTimeout || s.EvidenceTimeout != c.Tasks.EvidenceTimeout || s.CapacityRetry != c.Tasks.CapacityRetry {
		t.Fatal("policy settings ignored")
	}
	if s.SubmitConcurrency != 2 || s.QueryConcurrency != 3 || s.SaveConcurrency != 1 {
		t.Fatal("stage settings ignored")
	}
	// 未配置媒体服务地址时，核对与 Mock 编码使用本机工具并沿用配置的工具路径。
	if v, ok := s.Results.(*s3objects.TaskResults).Videos.(mediatools.Local); !ok || v.Settings.FFprobe != c.Media.FFprobe {
		t.Fatal("media tool setting ignored")
	}
	if e, ok := s.Provider.(mockmedia.TaskProvider).Encoder.(mediatools.Local); !ok || e.Settings.FFmpeg != c.Media.FFmpeg {
		t.Fatal("mock encoding tool setting ignored")
	}
}

type assemblyCapacity struct{}

func (assemblyCapacity) AcquireTaskCapacity(context.Context, d.Binding, string, int) (ports.Lease, bool, error) {
	panic("assembly fixture must not execute")
}
