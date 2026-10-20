package appconfig

import "time"

const MailSubmissionTimeout = 15 * time.Second
const CreditRecoveryInterval = 15 * time.Second
const MockVideoEncodeTimeout = 45 * time.Second

const (
	StreamHeartbeat            = 2 * time.Second
	StreamWriteTimeout         = 5 * time.Second
	StreamListenConnectTimeout = 5 * time.Second
)

// Defaults集中内部运行参数。YAML解码连接、Redis运行限额及四个任务并发/轮询字段，
// 这些默认值不需要部署条目；相对资源路径以配置文件所在目录为基准。
func Defaults() Config {
	return Config{
		StartupTimeout: 15 * time.Second, ShutdownTimeout: 20 * time.Second,
		HTTP:     HTTP{Address: "127.0.0.1:8787", DemoDir: "../demo", ReadHeaderTimeout: 5 * time.Second},
		Database: Database{MaxConnections: 8},
		Redis:    Redis{Address: "127.0.0.1:6379", Namespace: "drama:local", DialTimeout: 2 * time.Second, OperationTimeout: 2 * time.Second, Lease: 30 * time.Second, RequestsPerMinute: 1200, IPRequestsPerMinute: 12000, ProviderConcurrency: 8, ProviderRequestsPerMinute: 120},
		Models:   Models{KeyFile: "../.data/model-config.key", DirectoryTimeout: 10 * time.Second, ConnectionRetryDelay: 100 * time.Millisecond},
		Storage:  Storage{Region: "us-east-1", PathStyle: true, HeaderTimeout: 30 * time.Second, ReadTimeout: 60 * time.Second, UploadTimeout: 90 * time.Second, TaskWriteTimeout: 60 * time.Second},
		Evidence: Evidence{
			Endpoint: "http://127.0.0.1:19530", Collection: "drama_visual_evidence_v1",
			Python: "../.data/evidence-embedding/runtime/bin/python", ModelDir: "../.data/evidence-embedding/model",
			RequestTimeout: 90 * time.Second, EmbeddingTimeout: 2 * time.Minute, ProcessWaitDelay: 5 * time.Second,
		},
		Media: Media{FFmpeg: "ffmpeg", FFprobe: "ffprobe", CWebP: "cwebp", DWebP: "dwebp", ThumbnailTimeout: 30 * time.Second, ProbeTimeout: 15 * time.Second},
		NSQ: NSQ{
			Topic: "drama.tasks", Channel: "workers", Concurrency: 32, PublishConcurrency: 8,
			DialTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
			HeartbeatInterval: 2 * time.Second, MessageTimeout: time.Minute, RequeueDelay: time.Second,
			ReconnectInterval: time.Second, ActionTimeout: 90 * time.Second, StopTimeout: 12 * time.Second,
		},
		Tasks: Tasks{
			SubmitConcurrency: 8, QueryConcurrency: 16, SaveConcurrency: 4,
			Lease: 15 * time.Second, PollInterval: time.Second, RetryInterval: time.Second, MaxSubmissions: 3,
			ObserveTimeout: 2 * time.Minute, SaveTimeout: 5 * time.Minute, EvidenceTimeout: 10 * time.Second, CapacityRetry: 100 * time.Millisecond,
			ScanInterval: time.Second, ScanBatch: 100, ScanMaxBatches: 4, ScanTimeout: 5 * time.Second, RestartInterval: 2 * time.Second,
		},
		Project: Project{ReceiptTimeout: 15 * time.Second, TransportRetryInterval: time.Second},
		Uploads: Uploads{CommitCheckTimeout: 10 * time.Second, CleanupTimeout: 15 * time.Second},
	}
}
