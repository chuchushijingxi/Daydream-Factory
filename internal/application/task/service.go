// Package task owns provider-job use cases and execution, independently of business aggregates.
package task

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	"drama-agent/internal/domain/spaces"
	d "drama-agent/internal/domain/task"
)

type Service struct {
	Capacity          ports.TaskCapacity
	scope             *ports.SpaceScope
	trusted           *ports.SpaceScope
	resource          *spaces.Resource
	SpaceResolver     ports.SpaceResolver
	SpaceAccess       ports.SpaceAccess
	Store             ports.TaskStore
	Resolver          ports.TaskResolver
	Permit            ports.TaskSubmissionPermit
	Provider          ports.TaskProvider
	Results           ports.TaskResultStore
	Queue             ports.TaskQueue
	ReadOnly          bool
	Policy            d.Policy
	Lease             time.Duration
	SubmitConcurrency int
	QueryConcurrency  int
	SaveConcurrency   int
	ObserveTimeout    time.Duration
	SaveTimeout       time.Duration
	EvidenceTimeout   time.Duration
	CapacityRetry     time.Duration
	gatesOnce         sync.Once
	gates             [3]chan struct{}
}
type Command struct {
	RequirePermit bool            `json:"requirePermit,omitempty"`
	ScopeID       string          `json:"scopeId"`
	CommandID     string          `json:"commandId"`
	Operation     string          `json:"operation"`
	ModelID       string          `json:"modelId"`
	Input         json.RawMessage `json:"input"`
	ActiveKey     string          `json:"activeKey"`
	CallerContext json.RawMessage `json:"callerContext"`
}
type View struct {
	SpaceID        string      `json:"spaceId"`
	OwnerID        string      `json:"ownerId"`
	CreatorID      string      `json:"creatorId"`
	ID             string      `json:"id"`
	ScopeID        string      `json:"scopeId"`
	Revision       uint64      `json:"revision"`
	Operation      string      `json:"operation"`
	ModelID        string      `json:"modelId"`
	Status         d.Status    `json:"status"`
	ProviderTaskID *string     `json:"providerTaskId"`
	ProviderStatus *string     `json:"providerStatus"`
	NextRunAt      *time.Time  `json:"nextRunAt"`
	Result         *ResultView `json:"result"`
	Billing        d.Billing   `json:"billing"`
	ErrorCode      *string     `json:"errorCode"`
	Error          *string     `json:"error"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
}
type FileView struct {
	FileID          string   `json:"fileId,omitempty"`
	ThumbnailURL    string   `json:"thumbnailUrl,omitempty"`
	ID              string   `json:"id"`
	ContentType     string   `json:"contentType"`
	SizeBytes       int64    `json:"sizeBytes"`
	SHA256          string   `json:"sha256"`
	Width           int      `json:"width"`
	Height          int      `json:"height"`
	DurationSeconds *float64 `json:"durationSeconds"`
	ContentURL      string   `json:"contentUrl,omitempty"`
}
type ResultView struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Files []FileView      `json:"files"`
}
type Listing struct {
	Items      []View  `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// fileView 是输出文件的唯一 HTTP 投影；视频附带时长，其余留空。
func fileView(f d.Output) FileView {
	var seconds *float64
	if strings.HasPrefix(f.ContentType, "video/") {
		n := f.DurationSeconds
		seconds = &n
	}
	thumb := ""
	if f.FileID != "" {
		thumb = "/api/files/" + f.FileID + "/thumbnail"
	}
	return FileView{FileID: f.FileID, ThumbnailURL: thumb, ID: f.ID, ContentType: f.ContentType, SizeBytes: f.Blob.SizeBytes, SHA256: f.Blob.SHA256, Width: f.Width, Height: f.Height, DurationSeconds: seconds}
}

// viewOf 投影任务为 HTTP 只读视图；与业务 project 聚合无关，仅包内使用。
func viewOf(t d.Task) View {
	v := View{SpaceID: t.SpaceID, OwnerID: t.OwnerID, CreatorID: t.CreatorID, ID: t.ID, ScopeID: t.ScopeID, Revision: t.Revision, Operation: t.State.Operation, ModelID: t.State.ModelID, Status: t.Status, NextRunAt: t.NextRunAt, Billing: t.Billing(), ErrorCode: ptr(t.State.ErrorCode), Error: ptr(t.State.Error), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if remote := t.Remote(); remote != nil {
		v.ProviderTaskID = ptr(remote.ID)
	}
	if o := t.State.LastObservation; o != nil {
		v.ProviderStatus = ptr(o.Receipt.ProviderStatus)
	} else if sub := t.LatestSubmission(); sub != nil && sub.Receipt != nil {
		v.ProviderStatus = ptr(sub.Receipt.ProviderStatus)
	}
	if t.State.Result != nil {
		v.Result = &ResultView{Data: t.State.Result.Data, Files: []FileView{}}
		for _, f := range t.State.Result.Files {
			v.Result.Files = append(v.Result.Files, fileView(f))
		}
	}
	return v
}
func validID(v string) bool {
	return v != "" && len(v) <= 256 && strings.TrimSpace(v) == v && !strings.ContainsAny(v, "\x00\r\n")
}
func canonical(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("null"), nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fault.ErrInvalid
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, fault.ErrInvalid
	}
	return json.Marshal(v)
}
func (s *Service) Submit(ctx context.Context, c Command) (View, error) {
	scope, err := s.operationScope(ctx, spaces.Edit)
	if err != nil {
		return View{}, err
	}
	store, err := s.scopedStore(scope)
	if err != nil {
		return View{}, err
	}
	if s.ReadOnly {
		return View{}, fault.New("SERVICE_READ_ONLY", "当前服务只读")
	}
	if s.Store == nil || s.Queue == nil || s.Results == nil || s.Provider == nil || s.Resolver == nil {
		return View{}, fault.New("TASK_UNAVAILABLE", "任务执行资源未配置")
	}
	if !validID(c.ScopeID) || !validID(c.CommandID) || !validID(c.ModelID) || !validID(c.Operation) || len(c.ActiveKey) > 256 || len(c.Input) > 64<<20 || len(c.CallerContext) > 1<<20 {
		return View{}, fault.ErrInvalid
	}
	c.Input, err = canonical(c.Input)
	if err != nil || bytes.Equal(c.Input, []byte("null")) {
		return View{}, fault.ErrInvalid
	}
	c.CallerContext, err = canonical(c.CallerContext)
	if err != nil {
		return View{}, err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return View{}, err
	}
	h := sha256.Sum256(raw)
	digest := hex.EncodeToString(h[:])
	old, err := store.TaskByCommand(ctx, c.ScopeID, c.CommandID)
	if err == nil {
		if old.State.CommandDigest != digest {
			return View{}, fault.New("COMMAND_ID_CONFLICT", "同一命令对应不同请求")
		}
		return viewOf(old), nil
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return View{}, err
	}
	binding, err := s.Resolver.ResolveTask(ctx, c.ModelID, c.Operation)
	if err != nil {
		return View{}, err
	}
	// 校验与序列化不产生厂商调用；真实外发前会按同一请求再次持久化。
	if _, err = s.Provider.Prepare(ctx, binding, c.Input); err != nil {
		return View{}, err
	}
	policy := s.Policy
	if policy.PollMillis <= 0 || policy.RetryMillis <= 0 || policy.MaxSubmissions < 1 || policy.MaxSubmissions > 3 {
		return View{}, fault.ErrInvalid
	}
	now := time.Now().UTC()
	if policy.ObserveUntil.IsZero() {
		if s.ObserveTimeout <= 0 {
			return View{}, fault.ErrInvalid
		}
		policy.ObserveUntil = now.Add(s.ObserveTimeout)
	}
	if policy.SaveUntil.IsZero() {
		if s.SaveTimeout <= 0 {
			return View{}, fault.ErrInvalid
		}
		policy.SaveUntil = now.Add(s.SaveTimeout)
	}
	t := d.Task{SpaceID: scope.SpaceID, OwnerID: scope.UserID, CreatorID: scope.UserID, ID: identity.New("task"), ScopeID: c.ScopeID, CommandID: c.CommandID, Status: d.Queued, Snapshot: d.Snapshot{Input: c.Input, CallerContext: c.CallerContext}, State: d.State{RequirePermit: c.RequirePermit, Operation: c.Operation, ModelID: c.ModelID, Binding: binding, CommandDigest: digest, ActiveKey: c.ActiveKey, Policy: policy, Submissions: []d.Submission{}}}
	t, err = store.CreateTask(ctx, t)
	if err != nil {
		return View{}, err
	}
	// PG 受理是持久事实；发布失败时由恢复扫描补投，不回滚已接受任务。
	if err = s.Queue.PublishTasks(ctx, []string{t.ID}); err != nil {
		log.Printf("task %s notification deferred to recovery: %v", t.ID, err)
		return viewOf(t), nil
	}
	return viewOf(t), nil
}
func (s *Service) Get(ctx context.Context, scope, id string) (View, error) {
	t, err := s.readTask(ctx, id, spaces.Read)
	if err == nil && t.ScopeID != scope {
		return View{}, fault.ErrNotFound
	}
	return viewOf(t), err
}
func (s *Service) GetByCommand(ctx context.Context, scope, id string) (View, error) {
	accessScope, err := s.operationScope(ctx, spaces.Read)
	if err != nil {
		return View{}, err
	}
	store, err := s.scopedStore(accessScope)
	if err != nil {
		return View{}, err
	}
	t, err := store.TaskByCommand(ctx, scope, id)
	if err == nil {
		err = s.requireTask(ctx, t, spaces.Read)
	}
	return viewOf(t), err
}
func (s *Service) List(ctx context.Context, scope, operation, cursor string, limit int) (Listing, error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return Listing{}, fault.ErrInvalid
	}
	var c *ports.TaskCursor
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return Listing{}, fault.ErrInvalid
		}
		c = &ports.TaskCursor{}
		if json.Unmarshal(raw, c) != nil || c.ID == "" || c.At.IsZero() {
			return Listing{}, fault.ErrInvalid
		}
	}
	accessScope, err := s.operationScope(ctx, spaces.Read)
	if err != nil {
		return Listing{}, err
	}
	store, err := s.scopedStore(accessScope)
	if err != nil {
		return Listing{}, err
	}
	rows, err := store.ListTasks(ctx, scope, operation, c, limit+1)
	if err != nil {
		return Listing{}, err
	}
	out := Listing{Items: []View{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		raw, _ := json.Marshal(ports.TaskCursor{At: last.CreatedAt, ID: last.ID})
		v := base64.RawURLEncoding.EncodeToString(raw)
		out.NextCursor = &v
	}
	for _, t := range rows {
		out.Items = append(out.Items, viewOf(t))
	}
	return out, nil
}
func (s *Service) Cancel(ctx context.Context, scope, id string, revision uint64) (View, error) {
	if s.ReadOnly {
		return View{}, fault.New("SERVICE_READ_ONLY", "当前服务只读")
	}
	current, err := s.readTask(ctx, id, spaces.Edit)
	if err != nil {
		return View{}, err
	}
	if current.ScopeID != scope {
		return View{}, fault.ErrNotFound
	}
	store := s.Store
	if stores, ok := store.(ports.ScopedTasks); ok {
		store = stores.ForTaskSpace(current.SpaceID)
	}
	t, err := store.CancelTask(ctx, scope, id, revision)
	return viewOf(t), err
}
func (s *Service) File(ctx context.Context, scope, id, output string) ([]byte, FileView, error) {
	t, err := s.readTask(ctx, id, spaces.Read)
	if err != nil {
		return nil, FileView{}, err
	}
	if t.ScopeID != scope || t.State.Result == nil {
		return nil, FileView{}, fault.ErrNotFound
	}
	for _, f := range t.State.Result.Files {
		if f.ID != output {
			continue
		}
		b, e := s.Results.ReadTaskOutput(ctx, t.ID, f)
		if e != nil {
			return nil, FileView{}, e
		}
		return b, fileView(f), nil
	}
	return nil, FileView{}, fault.ErrNotFound
}

func (s *Service) Thumbnail(ctx context.Context, scope, id, output string) ([]byte, error) {
	t, err := s.readTask(ctx, id, spaces.Read)
	if err != nil {
		return nil, err
	}
	if t.ScopeID != scope || t.Status != d.Succeeded || t.State.Result == nil {
		return nil, fault.ErrNotFound
	}
	for _, f := range t.State.Result.Files {
		if f.ID != output {
			continue
		}
		thumbnails, ok := s.Results.(ports.TaskOutputThumbnails)
		if !ok {
			return nil, fault.New("THUMBNAIL_UNAVAILABLE", "视频封面存储不可用")
		}
		return thumbnails.ReadTaskThumbnail(ctx, t.ID, f)
	}
	return nil, fault.ErrNotFound
}

// Input returns immutable caller input only after the same scope check as Get.
func (s *Service) Input(ctx context.Context, scope, id string) (d.Snapshot, error) {
	if _, err := s.Get(ctx, scope, id); err != nil {
		return d.Snapshot{}, err
	}
	return s.Store.TaskInput(ctx, id)
}

// Inputs 按 Input 的同一 scope/归属约束一次取回多个任务的不可变快照；
// 任一任务缺失保持与单条 Input 相同的 NOT_FOUND，仓储不支持批量时退回逐条。
func (s *Service) Inputs(ctx context.Context, scope string, ids []string) (map[string]d.Snapshot, error) {
	out := map[string]d.Snapshot{}
	if scope == "" || len(ids) == 0 {
		return out, nil
	}
	if store, ok := s.Store.(ports.TaskInputs); ok && s.scope == nil {
		raw, err := store.TaskInputs(ctx, scope, ids)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			snapshot, ok := raw[id]
			if !ok {
				return nil, fault.ErrNotFound
			}
			out[id] = snapshot
		}
		return out, nil
	}
	for _, id := range ids {
		snapshot, err := s.Input(ctx, scope, id)
		if err != nil {
			return nil, err
		}
		out[id] = snapshot
	}
	return out, nil
}

// derive 不复制执行并发门；派生实例只服务当前用例，后台执行使用原实例。
func (s *Service) derive() *Service {
	return &Service{Store: s.Store, Resolver: s.Resolver, SpaceResolver: s.SpaceResolver, SpaceAccess: s.SpaceAccess, scope: s.scope, trusted: s.trusted, resource: s.resource,
		Capacity: s.Capacity, Permit: s.Permit, Provider: s.Provider, Results: s.Results, Queue: s.Queue, ReadOnly: s.ReadOnly, Policy: s.Policy, Lease: s.Lease,
		SubmitConcurrency: s.SubmitConcurrency, QueryConcurrency: s.QueryConcurrency, SaveConcurrency: s.SaveConcurrency,
		ObserveTimeout: s.ObserveTimeout, SaveTimeout: s.SaveTimeout, EvidenceTimeout: s.EvidenceTimeout, CapacityRetry: s.CapacityRetry}
}
func (s *Service) ForScope(scope ports.SpaceScope) *Service {
	out := s.derive()
	out.scope = &scope
	out.trusted = nil
	return out
}

// ForSpace 仅供后台恢复已接受任务，空间来自持久记录。
func (s *Service) ForSpace(space string) *Service {
	out := s.derive()
	out.scope = nil
	out.resource = nil
	out.trusted = &ports.SpaceScope{SpaceID: space}
	if store, ok := s.Store.(ports.ScopedTasks); ok {
		out.Store = store.ForTaskSpace(space)
	} else {
		out.Store = deniedTasks{TaskStore: s.Store}
	}
	return out
}

// ForTrustedScope 用于内部创建任务，发起人与空间均已由业务持久事实确定。
func (s *Service) ForTrustedScope(scope ports.SpaceScope) *Service {
	out := s.derive()
	out.scope = nil
	out.trusted = &scope
	out.resource = nil
	return out
}

// ForResource 指定调用方已读取的容器；共享权限随容器，不由任务发起人推断。
func (s *Service) ForResource(resource spaces.Resource) *Service {
	out := s.derive()
	out.resource = &resource
	return out
}
func (s *Service) operationScope(ctx context.Context, need spaces.Access) (ports.SpaceScope, error) {
	if s.scope == nil {
		if s.trusted != nil {
			return *s.trusted, nil
		}
		return ports.SpaceScope{}, nil
	}
	if s.resource != nil {
		return ports.RequireSpaceAccess(ctx, s.SpaceAccess, *s.scope, *s.resource, need)
	}
	scope, err := ports.ResolveSpace(ctx, s.SpaceResolver, *s.scope)
	if err == nil && need == spaces.Edit && scope.ReadOnly {
		err = fault.New("TEAM_READ_ONLY", "团队已只读")
	}
	return scope, err
}
func (s *Service) scopedStore(scope ports.SpaceScope) (ports.TaskStore, error) {
	if s.scope == nil && s.trusted == nil {
		return s.Store, nil
	}
	if store, ok := s.Store.(interface {
		ForTaskScope(ports.SpaceScope) ports.TaskStore
	}); ok {
		return store.ForTaskScope(scope), nil
	}
	return nil, fault.New("AUTH_REQUIRED", "任务空间存储不可用")
}
func (s *Service) requireTask(ctx context.Context, t d.Task, need spaces.Access) error {
	if s.scope == nil {
		return nil
	}
	resource := spaces.Resource{SpaceID: t.SpaceID, OwnerID: t.OwnerID, Kind: "media", ID: t.ID}
	if s.resource != nil {
		if s.resource.SpaceID != t.SpaceID || s.resource.Kind == spaces.ProjectResource && t.ScopeID != "project:"+s.resource.ID {
			return fault.ErrNotFound
		}
		resource = *s.resource
	}
	_, err := ports.RequireSpaceAccess(ctx, s.SpaceAccess, *s.scope, resource, need)
	return err
}
func (s *Service) readTask(ctx context.Context, id string, need spaces.Access) (d.Task, error) {
	t, err := s.Store.Task(ctx, id)
	if err == nil {
		err = s.requireTask(ctx, t, need)
	}
	if err != nil {
		return d.Task{}, err
	}
	return t, nil
}

type deniedTasks struct{ ports.TaskStore }

func (deniedTasks) CreateTask(context.Context, d.Task) (d.Task, error) {
	return d.Task{}, fault.New("AUTH_REQUIRED", "任务空间存储不可用")
}
func (deniedTasks) Task(context.Context, string) (d.Task, error) {
	return d.Task{}, fault.New("AUTH_REQUIRED", "任务空间存储不可用")
}
func (deniedTasks) TaskByCommand(context.Context, string, string) (d.Task, error) {
	return d.Task{}, fault.New("AUTH_REQUIRED", "任务空间存储不可用")
}
func (deniedTasks) ListTasks(context.Context, string, string, *ports.TaskCursor, int) ([]d.Task, error) {
	return nil, fault.New("AUTH_REQUIRED", "任务空间存储不可用")
}
func (deniedTasks) CancelTask(context.Context, string, string, uint64) (d.Task, error) {
	return d.Task{}, fault.New("AUTH_REQUIRED", "任务空间存储不可用")
}
