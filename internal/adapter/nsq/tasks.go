// Package nsq transports task IDs; PostgreSQL remains authoritative.
package nsq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	gonsq "github.com/nsqio/go-nsq"
)

type Queue struct {
	Address, Topic, Channel string
	Concurrency             int
	publishSlots            chan struct{}
	closed                  atomic.Bool
	config                  *gonsq.Config
	settings                appconfig.NSQ
}

var _ ports.TaskQueue = (*Queue)(nil)

func New(settings appconfig.NSQ) (*Queue, error) {
	address, topic, channel, concurrency := settings.Address, settings.Topic, settings.Channel, settings.Concurrency
	host, _, err := net.SplitHostPort(address)
	if err != nil || (host != "localhost" && net.ParseIP(host) == nil) || !gonsq.IsValidTopicName(topic) || !gonsq.IsValidChannelName(channel) || strings.HasSuffix(topic, "#ephemeral") || strings.HasSuffix(channel, "#ephemeral") || concurrency < 1 || settings.PublishConcurrency < 1 {
		return nil, fmt.Errorf("invalid task NSQ configuration")
	}
	cfg := gonsq.NewConfig()
	cfg.DialTimeout = settings.DialTimeout
	cfg.ReadTimeout = settings.ReadTimeout
	cfg.WriteTimeout = settings.WriteTimeout
	cfg.HeartbeatInterval = settings.HeartbeatInterval
	if err = cfg.Validate(); err != nil {
		return nil, err
	}
	return &Queue{Address: address, Topic: topic, Channel: channel, Concurrency: concurrency, config: cfg, publishSlots: make(chan struct{}, settings.PublishConcurrency), settings: settings}, nil
}

func (q *Queue) Close() { q.closed.Store(true) }
func (q *Queue) PublishTasks(ctx context.Context, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bodies := make([][]byte, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("empty task ID")
		}
		body, err := json.Marshal(struct {
			TaskID string `json:"taskId"`
		}{id})
		if err != nil {
			return err
		}
		bodies = append(bodies, body)
	}
	if len(bodies) == 0 {
		return nil
	}
	select {
	case q.publishSlots <- struct{}{}:
		defer func() { <-q.publishSlots }()
	default:
		return fmt.Errorf("task notification capacity reached; PG recovery will publish")
	}
	if q.closed.Load() {
		return gonsq.ErrStopped
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 每批使用一条有界连接。go-nsq 1.1.0 的路由器在最后一次连接读取前就
	// 释放 WaitGroup，跨断线/重连复用同一生产者会竞态；绝不复用同一生产者。
	p, err := gonsq.NewProducer(q.Address, q.config)
	if err != nil {
		return err
	}
	p.SetLogger(log.Default(), gonsq.LogLevelError)
	defer p.Stop()
	return p.MultiPublish(q.Topic, bodies)
}
func (q *Queue) ConsumeTasks(ctx context.Context, handle func(context.Context, string) error) error {
	cfg := gonsq.NewConfig()
	cfg.MaxInFlight = q.Concurrency
	cfg.ReadTimeout = q.settings.ReadTimeout
	cfg.WriteTimeout = q.settings.WriteTimeout
	cfg.HeartbeatInterval = q.settings.HeartbeatInterval
	cfg.MsgTimeout = q.settings.MessageTimeout
	cfg.MaxAttempts = 0
	cfg.DefaultRequeueDelay = q.settings.RequeueDelay
	cfg.DialTimeout = q.settings.DialTimeout
	cfg.LookupdPollInterval = q.settings.ReconnectInterval // also controls direct NSQD reconnect delay
	c, err := gonsq.NewConsumer(q.Topic, q.Channel, cfg)
	if err != nil {
		return err
	}
	c.SetLogger(log.Default(), gonsq.LogLevelError)
	c.AddConcurrentHandlers(gonsq.HandlerFunc(func(m *gonsq.Message) error {
		var in struct {
			TaskID string `json:"taskId"`
		}
		if json.Unmarshal(m.Body, &in) != nil || in.TaskID == "" {
			log.Print("discard invalid task notification")
			return nil
		}
		action, cancel := context.WithTimeout(ctx, q.settings.ActionTimeout)
		defer cancel()
		return handle(action, in.TaskID)
	}), q.Concurrency)
	if err = c.ConnectToNSQD(q.Address); err != nil {
		c.Stop()
		return err
	}
	<-ctx.Done()
	c.Stop()
	select {
	case <-c.StopChan:
		return nil
	case <-time.After(q.settings.StopTimeout):
		return fmt.Errorf("task consumer shutdown timed out")
	}
}
