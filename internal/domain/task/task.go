// Package task describes durable provider jobs without project or asset dependencies.
package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Unknown   Status = "unknown"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

func (s Status) Terminal() bool { return s == Succeeded || s == Failed || s == Cancelled }
func CanTransition(from, to Status) bool {
	if from == to {
		return true
	}
	if from.Terminal() {
		return false
	}
	switch from {
	case Queued:
		return to == Running || to == Failed || to == Cancelled
	case Running:
		return to == Succeeded || to == Failed || to == Unknown || to == Cancelled
	case Unknown:
		return to == Running || to == Failed || to == Cancelled
	}
	return false
}

var ErrTransition = errors.New("invalid task transition")

func (t *Task) Transition(to Status) error {
	if !CanTransition(t.Status, to) {
		return ErrTransition
	}
	if to.Terminal() && t.State.EndedAt == nil {
		ended := time.Now().UTC()
		t.State.EndedAt = &ended
	}
	t.Status = to
	return nil
}

type Snapshot struct {
	Input         json.RawMessage `json:"input"`
	CallerContext json.RawMessage `json:"callerContext"`
}
type Binding struct {
	// ProviderConfig freezes provider-specific configuration and decisions.
	// The task kernel persists it without interpreting provider semantics.
	ProviderConfig json.RawMessage `json:"providerConfig,omitempty"`
	ConnectionID   string          `json:"connectionId"`
	Protocol       string          `json:"protocol"`
	ModelID        string          `json:"modelId"`
	RequestModelID string          `json:"requestModelId"`
	Endpoint       string          `json:"endpoint,omitempty"`
	AccountScope   string          `json:"accountScope,omitempty"`
	Credential     []byte          `json:"credential,omitempty"`
	Free           bool            `json:"free"`
}
type Policy struct {
	PollMillis     int64     `json:"pollMillis"`
	RetryMillis    int64     `json:"retryMillis"`
	ObserveUntil   time.Time `json:"observeUntil"`
	SaveUntil      time.Time `json:"saveUntil"`
	MaxSubmissions int       `json:"maxSubmissions"`
}
type Billing struct {
	Status           string `json:"status"`
	KnownMicroCNY    int64  `json:"knownMicroCny"`
	ReservedMicroCNY *int64 `json:"reservedMicroCny"`
}
type Remote struct {
	ID        string    `json:"id"`
	ReadyAt   time.Time `json:"readyAt,omitempty"`
	ForceFail bool      `json:"forceFail,omitempty"`
}
type SourceFile struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}
type Description struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Files []SourceFile    `json:"files"`
}
type Blob struct {
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"objectKey"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}
type Output struct {
	FileID          string  `json:"fileId,omitempty"`
	ID              string  `json:"id"`
	Blob            Blob    `json:"blob"`
	ContentType     string  `json:"contentType"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	DurationSeconds float64 `json:"durationSeconds"`
}
type Result struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Files []Output        `json:"files"`
}

// Receipt 是类型化的运输/协议事实，不是第二套任务状态机。
type Receipt struct {
	Outcome        string          `json:"outcome"` // accepted, pending, completed, failed, unknown, not_sent
	Raw            json.RawMessage `json:"raw,omitempty"`
	RawObject      *Blob           `json:"rawObject,omitempty"`
	Remote         *Remote         `json:"remote,omitempty"`
	Result         *Description    `json:"result,omitempty"`
	Usage          json.RawMessage `json:"usage,omitempty"`
	Billing        Billing         `json:"billing"`
	ProviderStatus string          `json:"providerStatus"`
	ErrorCode      string          `json:"errorCode,omitempty"`
	Error          string          `json:"error,omitempty"`
	RetryMillis    int64           `json:"retryMillis,omitempty"`
}

// 提交事实取值持久保存在 payload 中，是 worker 推进、费用合并与迟到证据
// 共用的持久化合同；字符串只在此定义，包外比较与写入必须使用这些常量。
const (
	DispositionPrepared    = "prepared"
	DispositionMayHaveSent = "may_have_sent"
	DispositionAccepted    = "accepted"
	DispositionImmediate   = "immediate"
	DispositionRejected    = "rejected"
	DispositionNotSent     = "not_sent"
)

// Disposition 把外发结果归类为提交事实；是全部消费者共用的唯一推导，不得分叉。
func (r Receipt) Disposition() string {
	switch r.Outcome {
	case "accepted":
		return DispositionAccepted
	case "completed":
		return DispositionImmediate
	case "failed":
		return DispositionRejected
	case "not_sent":
		return DispositionNotSent
	default:
		return DispositionMayHaveSent
	}
}

type Submission struct {
	ID            string          `json:"id"`
	Disposition   string          `json:"disposition"`
	Request       json.RawMessage `json:"request,omitempty"`
	RequestObject *Blob           `json:"requestObject,omitempty"`
	StartedAt     time.Time       `json:"startedAt"`
	Receipt       *Receipt        `json:"receipt,omitempty"`
}
type Observation struct {
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Receipt Receipt   `json:"receipt"`
}

func (o Observation) Special() bool {
	return o.Receipt.Outcome == "completed" || o.Receipt.Outcome == "failed" || o.Receipt.Outcome == "unknown" || o.Receipt.Billing.KnownMicroCNY > 0 || len(o.Receipt.Usage) > 0
}

type State struct {
	RequirePermit        bool          `json:"requirePermit,omitempty"`
	EndedAt              *time.Time    `json:"endedAt,omitempty"`
	Operation            string        `json:"operation"`
	ModelID              string        `json:"modelId"`
	CommandDigest        string        `json:"commandDigest"`
	ActiveKey            string        `json:"activeKey,omitempty"`
	Binding              Binding       `json:"binding"`
	Policy               Policy        `json:"policy"`
	Submissions          []Submission  `json:"submissions"`
	LastObservation      *Observation  `json:"lastObservation,omitempty"`
	RetainedObservations []Observation `json:"retainedObservations,omitempty"`
	Result               *Result       `json:"result,omitempty"`
	ErrorCode            string        `json:"errorCode,omitempty"`
	Error                string        `json:"error,omitempty"`
}
type Task struct {
	OwnerID    string
	ID         string
	ScopeID    string
	CommandID  string
	Status     Status
	Revision   uint64
	NextRunAt  *time.Time
	LeaseToken string
	LeaseUntil *time.Time
	Snapshot   Snapshot
	State      State
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (t Task) LatestSubmission() *Submission {
	if len(t.State.Submissions) == 0 {
		return nil
	}
	return &t.State.Submissions[len(t.State.Submissions)-1]
}
func (t Task) Remote() *Remote {
	s := t.LatestSubmission()
	if s != nil && s.Receipt != nil {
		return s.Receipt.Remote
	}
	return nil
}
func (t Task) Description() *Description {
	if o := t.State.LastObservation; o != nil && o.Receipt.Outcome == "completed" {
		return o.Receipt.Result
	}
	s := t.LatestSubmission()
	if s != nil && s.Receipt != nil && s.Receipt.Outcome == "completed" {
		return s.Receipt.Result
	}
	return nil
}
func (t *Task) Observe(o Observation) {
	if previous := t.State.LastObservation; previous != nil && previous.ID == o.ID {
		return
	}
	if previous := t.State.LastObservation; previous != nil && previous.At.After(o.At) {
		if o.Special() {
			t.retain(o)
		}
		return
	}
	if previous := t.State.LastObservation; previous != nil && previous.Special() {
		t.retain(*previous)
	}
	t.State.LastObservation = &o
}
func (t *Task) retain(o Observation) {
	// 迟到观察只补必要事实；累计费用未变时不生成新快照。
	key := func(r Receipt) []byte {
		r.Raw = nil
		r.RawObject = nil
		r.ProviderStatus = ""
		r.RetryMillis = 0
		raw, _ := json.Marshal(r)
		return raw
	}
	for _, old := range t.State.RetainedObservations {
		if bytes.Equal(key(old.Receipt), key(o.Receipt)) {
			return
		}
	}
	t.State.RetainedObservations = append(t.State.RetainedObservations, o)
}
func (t Task) Billing() Billing {
	z := int64(0)
	if t.State.Binding.Free {
		return Billing{Status: "known", ReservedMicroCNY: &z}
	}
	b := Billing{Status: "pending"}
	if len(t.State.Submissions) == 0 {
		return b
	}
	b.Status = "known"
	for _, s := range t.State.Submissions {
		if s.Receipt == nil {
			if s.Disposition == DispositionMayHaveSent {
				b.Status = "unknown"
			} else if b.Status != "unknown" {
				b.Status = "pending"
			}
			continue
		}
		b.KnownMicroCNY += s.Receipt.Billing.KnownMicroCNY
		if s.Receipt.Billing.Status == "unknown" {
			b.Status = "unknown"
		} else if s.Receipt.Billing.Status != "known" && b.Status != "unknown" {
			b.Status = "pending"
		}
	}
	return b
}
