package modelconfig

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"math/big"

	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/task"
)

func (c *Service) ResolveTask(ctx context.Context, id, operation string) (d.Binding, error) {
	cfg, err := c.Snapshot(ctx)
	if err != nil {
		return d.Binding{}, err
	}
	for _, conn := range cfg.Connections {
		for _, m := range conn.Models {
			if m.ID != id {
				continue
			}
			if !m.Enabled {
				return d.Binding{}, fault.ErrModelUnavailable
			}
			if conn.ID != "mock" || conn.Kind != "builtin" || conn.Protocol != md.MockProtocol || !md.IsMockModel(m.ModelID, m.Category) {
				return d.Binding{}, fault.New("TASK_PROTOCOL_UNSUPPORTED", "真实媒体厂商任务尚未接入；当前仅支持本地Mock")
			}
			expected := md.MockOperation(m.Category)
			if expected == "" {
				return d.Binding{}, fault.ErrModelUnavailable
			}
			if operation != expected {
				return d.Binding{}, fault.ErrInvalid
			}
			if m.MockBehavior == nil || !m.MockBehavior.Valid() {
				return d.Binding{}, fault.ErrInvalid
			}
			execution := md.MockExecution{MockBehavior: *m.MockBehavior, Fail: m.MockBehavior.FailurePercent == 100, VideoOptions: m.Category == "video"}
			if m.MockBehavior.FailurePercent > 0 && m.MockBehavior.FailurePercent < 100 {
				reader := c.MockRandom
				if reader == nil {
					reader = rand.Reader
				}
				draw, e := rand.Int(reader, big.NewInt(100))
				if e != nil {
					return d.Binding{}, fault.New("TASK_UNAVAILABLE", "本地模拟随机源不可用")
				}
				execution.Fail = draw.Int64() < int64(m.MockBehavior.FailurePercent)
			}
			parameters, e := json.Marshal(execution)
			if e != nil {
				return d.Binding{}, e
			}
			return d.Binding{ProviderConfig: parameters, ConnectionID: conn.ID, Protocol: conn.Protocol, ModelID: m.ID, RequestModelID: m.ModelID, AccountScope: "local-mock", Free: true}, nil
		}
	}
	return d.Binding{}, fault.ErrModelUnavailable
}
