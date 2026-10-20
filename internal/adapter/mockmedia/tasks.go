package mockmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/application/validation"
	"drama-agent/internal/domain/fault"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/task"
)

type TaskProvider struct{ Media appconfig.Media }
type TaskInput struct {
	Resolution      string  `json:"resolution,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Prompt          string  `json:"prompt"`
	AspectRatio     string  `json:"aspectRatio"`
	DelayMS         int     `json:"delayMs,omitempty"`
	ForceFail       bool    `json:"forceFail,omitempty"`
}

var _ ports.TaskProvider = TaskProvider{}

func (TaskProvider) Prepare(ctx context.Context, b d.Binding, raw json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.Protocol != md.MockProtocol || (b.RequestModelID != md.MockImageModelID && b.RequestModelID != md.MockVideoModelID) {
		return nil, fault.New("TASK_PROTOCOL_UNSUPPORTED", "尚未接入该厂商任务协议")
	}
	var in TaskInput
	if validation.DecodeStrict(raw, &in) != nil || strings.TrimSpace(in.Prompt) == "" || len(in.Prompt) > 20000 || in.DelayMS < 0 || in.DelayMS > 10000 {
		return nil, fault.ErrInvalid
	}
	execution, err := frozenExecution(b)
	if err != nil || execution.VideoOptions && b.RequestModelID != md.MockVideoModelID || len(b.ProviderConfig) > 0 && (in.DelayMS != 0 || in.ForceFail) {
		return nil, fault.ErrInvalid
	}
	profile := md.OfficialMediaProfile("mock", b.RequestModelID)
	if b.RequestModelID == md.MockVideoModelID && !execution.VideoOptions {
		// An old accepted task keeps the exact sample it was promised. New
		// bindings are always marked by ResolveTask; this is not a public option.
		profile.Resolutions = []md.MediaResolution{{ID: "1080p"}}
		profile.Durations = []float64{md.MockVideoDurationSeconds}
		profile.DefaultDuration = md.MockVideoDurationSeconds
	}
	options := md.MediaOptions{Resolution: in.Resolution, DurationSeconds: in.DurationSeconds}
	if options.Resolution == "" && options.DurationSeconds == 0 {
		options = profile.Defaults()
	}
	if !profile.Supports(options) {
		return nil, fault.ErrInvalid
	}
	in.Resolution, in.DurationSeconds = options.Resolution, options.DurationSeconds
	if b.RequestModelID == md.MockImageModelID {
		if _, _, ok := imageSize(in.AspectRatio); !ok {
			return nil, fault.ErrInvalid
		}
	} else if in.AspectRatio != "" {
		return nil, fault.ErrInvalid
	}
	return json.Marshal(in)
}
func frozenExecution(b d.Binding) (md.MockExecution, error) {
	var execution md.MockExecution
	if len(b.ProviderConfig) > 0 && (validation.DecodeStrict(b.ProviderConfig, &execution) != nil || !execution.Valid()) {
		return execution, fault.ErrInvalid
	}
	return execution, nil
}
func free() d.Billing { z := int64(0); return d.Billing{Status: "known", ReservedMicroCNY: &z} }
func (TaskProvider) Submit(ctx context.Context, b d.Binding, submission string, request json.RawMessage) (d.Receipt, error) {
	if err := ctx.Err(); err != nil {
		raw, _ := json.Marshal(map[string]string{"notSent": err.Error()})
		return d.Receipt{Outcome: "not_sent", Raw: raw, Billing: free()}, nil
	}
	var in TaskInput
	if err := json.Unmarshal(request, &in); err != nil {
		return d.Receipt{}, err
	}
	// 远端引用持久保存在任务里；该模拟厂商不依赖 worker 进程内存。
	// Bindings without providerConfig belong to already accepted legacy tasks.
	// Their immutable delay/failure facts remain valid after retiring management APIs.
	delay, fail := time.Duration(in.DelayMS)*time.Millisecond, in.ForceFail
	if len(b.ProviderConfig) > 0 {
		var execution md.MockExecution
		if validation.DecodeStrict(b.ProviderConfig, &execution) != nil || !execution.Valid() {
			return d.Receipt{}, fault.ErrInvalid
		}
		delay, fail = time.Duration(execution.DelaySeconds)*time.Second, execution.Fail
	}
	remote := d.Remote{ID: "mock-" + submission, ReadyAt: time.Now().UTC().Add(delay), ForceFail: fail}
	raw, _ := json.Marshal(map[string]any{"taskId": remote.ID, "readyAt": remote.ReadyAt, "forceFail": remote.ForceFail, "status": "accepted"})
	return d.Receipt{Outcome: "accepted", Raw: raw, Remote: &remote, Billing: free(), ProviderStatus: "accepted"}, nil
}
func (TaskProvider) Query(ctx context.Context, b d.Binding, remote d.Remote) (d.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return d.Receipt{}, err
	}
	if b.Protocol != md.MockProtocol || !strings.HasPrefix(remote.ID, "mock-submission_") {
		return d.Receipt{}, fault.ErrInvalid
	}
	r := d.Receipt{Billing: free(), ProviderStatus: "running", Outcome: "pending"}
	if !time.Now().Before(remote.ReadyAt) {
		if remote.ForceFail {
			r.Outcome = "failed"
			r.ProviderStatus = "failed"
			r.ErrorCode = "MOCK_GENERATION_FAILED"
			r.Error = "本次任务按模型配置的失败概率模拟生成失败"
		} else {
			sum := sha256.Sum256([]byte(remote.ID))
			id := "media_" + hex.EncodeToString(sum[:16])
			r.Outcome = "completed"
			r.ProviderStatus = "succeeded"
			r.Result = &d.Description{Files: []d.SourceFile{{ID: id, Source: remote.ID}}}
		}
	}
	raw, _ := json.Marshal(map[string]any{"taskId": remote.ID, "status": r.ProviderStatus, "error": r.Error, "result": r.Result})
	r.Raw = raw
	return r, nil
}
func (p TaskProvider) Fetch(ctx context.Context, b d.Binding, file d.SourceFile, input json.RawMessage) (ports.MediaResult, error) {
	if !strings.HasPrefix(file.Source, "mock-submission_") {
		return ports.MediaResult{}, fault.ErrInvalid
	}
	prepared, err := p.Prepare(ctx, b, input)
	if err != nil {
		return ports.MediaResult{}, err
	}
	var in TaskInput
	if err = validation.DecodeStrict(prepared, &in); err != nil {
		return ports.MediaResult{}, err
	}
	execution, err := frozenExecution(b)
	if err != nil {
		return ports.MediaResult{}, err
	}
	if b.RequestModelID == md.MockVideoModelID && !execution.VideoOptions {
		return ports.MediaResult{Data: bytes.Clone(sampleVideo), ContentType: "video/mp4", Width: 1920, Height: 1080, DurationSeconds: md.MockVideoDurationSeconds}, nil
	}
	return (Generator{Settings: p.Media}).Generate(ctx, ports.MediaRequest{ModelID: b.RequestModelID, Prompt: in.Prompt, AspectRatio: in.AspectRatio, Resolution: in.Resolution, DurationSeconds: in.DurationSeconds})
}
