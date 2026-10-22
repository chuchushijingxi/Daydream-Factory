// Package appconfig reads deployment settings once, before resources are assembled.
// It has no environment fallback and never returns credential values in errors.
package appconfig

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	gonsq "github.com/nsqio/go-nsq"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Environment     string        `yaml:"environment"`
	Payment         Payment       `yaml:"payment"`
	ReadOnly        bool          `yaml:"-"`
	StartupTimeout  time.Duration `yaml:"-"`
	ShutdownTimeout time.Duration `yaml:"-"`
	HTTP            HTTP          `yaml:"-"`
	Database        Database      `yaml:"database"`
	Redis           Redis         `yaml:"redis"`
	Models          Models        `yaml:"-"`
	Storage         Storage       `yaml:"storage"`
	Evidence        Evidence      `yaml:"-"`
	Media           Media         `yaml:"-"`
	NSQ             NSQ           `yaml:"nsq"`
	Tasks           Tasks         `yaml:"tasks"`
	Project         Project       `yaml:"-"`
	Uploads         Uploads       `yaml:"-"`
}
type HTTP struct {
	Address           string        `yaml:"-"`
	DemoDir           string        `yaml:"-"`
	ReadHeaderTimeout time.Duration `yaml:"-"`
}
type Database struct {
	URL            string `yaml:"url"`
	MaxConnections int32  `yaml:"-"`
}
type Redis struct {
	Address                   string        `yaml:"address"`
	Password                  string        `yaml:"password"`
	Namespace                 string        `yaml:"namespace"`
	DialTimeout               time.Duration `yaml:"dialTimeout"`
	OperationTimeout          time.Duration `yaml:"operationTimeout"`
	Lease                     time.Duration `yaml:"lease"`
	RequestsPerMinute         int           `yaml:"requestsPerMinute"`
	IPRequestsPerMinute       int           `yaml:"ipRequestsPerMinute"`
	ProviderConcurrency       int           `yaml:"providerConcurrency"`
	ProviderRequestsPerMinute int           `yaml:"providerRequestsPerMinute"`
}
type Models struct {
	KeyFile              string        `yaml:"-"`
	DirectoryTimeout     time.Duration `yaml:"-"`
	ConnectionRetryDelay time.Duration `yaml:"-"`
}
type Storage struct {
	Endpoint         string        `yaml:"endpoint"`
	Region           string        `yaml:"region"`
	Bucket           string        `yaml:"bucket"`
	AccessKey        string        `yaml:"accessKey"`
	SecretKey        string        `yaml:"secretKey"`
	SessionToken     string        `yaml:"sessionToken"`
	PathStyle        bool          `yaml:"pathStyle"`
	HeaderTimeout    time.Duration `yaml:"-"`
	ReadTimeout      time.Duration `yaml:"-"`
	UploadTimeout    time.Duration `yaml:"-"`
	TaskWriteTimeout time.Duration `yaml:"-"`
}
type Evidence struct {
	Endpoint         string        `yaml:"-"`
	Collection       string        `yaml:"-"`
	Python           string        `yaml:"-"`
	ModelDir         string        `yaml:"-"`
	RequestTimeout   time.Duration `yaml:"-"`
	EmbeddingTimeout time.Duration `yaml:"-"`
	ProcessWaitDelay time.Duration `yaml:"-"`
}
type Media struct {
	FFmpeg           string        `yaml:"-"`
	FFprobe          string        `yaml:"-"`
	CWebP            string        `yaml:"-"`
	DWebP            string        `yaml:"-"`
	ThumbnailTimeout time.Duration `yaml:"-"`
	ProbeTimeout     time.Duration `yaml:"-"`
}
type NSQ struct {
	Address            string        `yaml:"address"`
	Topic              string        `yaml:"topic"`
	Channel            string        `yaml:"channel"`
	Concurrency        int           `yaml:"-"`
	PublishConcurrency int           `yaml:"-"`
	DialTimeout        time.Duration `yaml:"-"`
	ReadTimeout        time.Duration `yaml:"-"`
	WriteTimeout       time.Duration `yaml:"-"`
	HeartbeatInterval  time.Duration `yaml:"-"`
	MessageTimeout     time.Duration `yaml:"-"`
	RequeueDelay       time.Duration `yaml:"-"`
	ReconnectInterval  time.Duration `yaml:"-"`
	ActionTimeout      time.Duration `yaml:"-"`
	StopTimeout        time.Duration `yaml:"-"`
}
type Tasks struct {
	SubmitConcurrency int           `yaml:"submitConcurrency"`
	QueryConcurrency  int           `yaml:"queryConcurrency"`
	SaveConcurrency   int           `yaml:"saveConcurrency"`
	Lease             time.Duration `yaml:"-"`
	PollInterval      time.Duration `yaml:"pollInterval"`
	RetryInterval     time.Duration `yaml:"-"`
	MaxSubmissions    int           `yaml:"-"`
	ObserveTimeout    time.Duration `yaml:"-"`
	SaveTimeout       time.Duration `yaml:"-"`
	EvidenceTimeout   time.Duration `yaml:"-"`
	CapacityRetry     time.Duration `yaml:"-"`
	ScanInterval      time.Duration `yaml:"-"`
	ScanBatch         int           `yaml:"-"`
	ScanMaxBatches    int           `yaml:"-"`
	ScanTimeout       time.Duration `yaml:"-"`
	RestartInterval   time.Duration `yaml:"-"`
}
type Project struct {
	ReceiptTimeout         time.Duration `yaml:"-"`
	TransportRetryInterval time.Duration `yaml:"-"`
}
type Uploads struct {
	CommitCheckTimeout time.Duration `yaml:"-"`
	CleanupTimeout     time.Duration `yaml:"-"`
}

func Load(filename string) (Config, error) {
	cfg := Defaults()
	f, err := os.Open(filename)
	if err != nil {
		return cfg, fmt.Errorf("cannot open configuration file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return cfg, fmt.Errorf("configuration must be a private regular file (chmod 600), at most 1 MiB")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return cfg, fmt.Errorf("cannot read configuration file")
	}
	return DecodeDocument(raw, filename)
}

// DecodeDocument is also used by the controlled legacy-mail importer after it
// removes that one obsolete section. It is the same strict runtime parser.
func DecodeDocument(raw []byte, filename string) (Config, error) {
	cfg := Defaults()
	if len(raw) > 1<<20 {
		return Config{}, fmt.Errorf("configuration document is too large")
	}
	d := yaml.NewDecoder(bytes.NewReader(raw))
	d.KnownFields(true)
	if d.Decode(&cfg) != nil || d.Decode(new(any)) != io.EOF {
		return Config{}, fmt.Errorf("invalid configuration YAML or unknown field; compare config/app.example.yaml")
	}
	base, err := filepath.Abs(filepath.Dir(filename))
	if err != nil {
		return Config{}, fmt.Errorf("cannot resolve configuration directory")
	}
	for _, path := range []*string{&cfg.HTTP.DemoDir, &cfg.Models.KeyFile, &cfg.Evidence.Python, &cfg.Evidence.ModelDir} {
		if !filepath.IsAbs(*path) {
			*path = filepath.Join(base, *path)
		}
	}
	for _, path := range []*string{&cfg.Media.FFmpeg, &cfg.Media.FFprobe, &cfg.Media.CWebP, &cfg.Media.DWebP} {
		if strings.ContainsAny(*path, "/\\") && !filepath.IsAbs(*path) {
			*path = filepath.Join(base, *path)
		}
	}
	cfg.resolvePaymentPaths(base)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"database.url", c.Database.URL}, {"nsq.address", c.NSQ.Address}, {"storage.endpoint", c.Storage.Endpoint},
		{"redis.address", c.Redis.Address}, {"redis.namespace", c.Redis.Namespace},
		{"storage.region", c.Storage.Region}, {"storage.bucket", c.Storage.Bucket}, {"storage.accessKey", c.Storage.AccessKey}, {"storage.secretKey", c.Storage.SecretKey},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("configuration %s is required", field.name)
		}
	}
	if _, _, err := net.SplitHostPort(c.Redis.Address); err != nil || strings.ContainsAny(c.Redis.Namespace, "{}*?[] \r\n") || len(c.Redis.Namespace) > 96 || c.Redis.Lease < 3*time.Second || c.Redis.DialTimeout <= 0 || c.Redis.OperationTimeout <= 0 || c.Redis.RequestsPerMinute <= 0 || c.Redis.IPRequestsPerMinute <= 0 || c.Redis.ProviderConcurrency <= 0 || c.Redis.ProviderRequestsPerMinute <= 0 {
		return fmt.Errorf("invalid Redis connection, namespace, timeouts or limits")
	}
	if c.Tasks.SubmitConcurrency <= 0 || c.Tasks.QueryConcurrency <= 0 || c.Tasks.SaveConcurrency <= 0 {
		return fmt.Errorf("configuration task concurrency must be positive")
	}
	if c.Tasks.PollInterval < time.Millisecond {
		return fmt.Errorf("configuration tasks.pollInterval must be at least 1ms")
	}
	u, err := url.Parse(c.Database.URL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" {
		return fmt.Errorf("configuration database.url requires a PostgreSQL URL")
	}
	u, err = url.Parse(c.Storage.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || (u.Path != "" && u.Path != "/") || u.Fragment != "" {
		return fmt.Errorf("configuration storage.endpoint requires an S3 HTTP(S) origin")
	}
	host, _, err := net.SplitHostPort(c.NSQ.Address)
	if err != nil || (host != "localhost" && net.ParseIP(host) == nil) {
		return fmt.Errorf("configuration nsq.address requires localhost or an IP and port")
	}
	if !gonsq.IsValidTopicName(c.NSQ.Topic) || !gonsq.IsValidChannelName(c.NSQ.Channel) || strings.HasSuffix(c.NSQ.Topic, "#ephemeral") || strings.HasSuffix(c.NSQ.Channel, "#ephemeral") {
		return fmt.Errorf("configuration nsq.topic/channel must be persistent names")
	}

	return c.validatePayment()
}
