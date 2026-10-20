package project

import (
	"encoding/json"
	"time"

	"drama-agent/internal/domain/billing"
	"drama-agent/internal/domain/credits"
)

const CatalogExtractionContract = "asset-preparation-v1"

// 历史记录保持可读；旧合同不再执行。
const HistoricalCatalogContract = "catalog-agent-v3"

func (r *Run) ExecutionContract() string {
	if r.Contract == CatalogExtractionContract && r.Stage == "visualPrompt" {
		return VisualContract
	}
	return r.Contract
}

func (p *Project) LatestCatalogRun() *Run {
	for i := len(p.Runs) - 1; i >= 0; i-- {
		r := &p.Runs[i]
		if r.SourceVersion == p.ScriptSource.Version && (r.Contract == CatalogExtractionContract || r.Contract == HistoricalCatalogContract) {
			return r
		}
	}
	return nil
}

func (p *Project) CatalogReady() bool {
	r := p.LatestCatalogRun()
	return r != nil && (r.Status == RunCompleted || r.Contract == CatalogExtractionContract && r.Stage == "visualPrompt")
}

type Call struct {
	// 与调用意图同存，用于重启时按原表示解释来源；空值保留旧调用。
	SourceReferenceFormat string          `json:"sourceReferenceFormat,omitempty"`
	ConnectionID          string          `json:"connectionId,omitempty"`
	RequestModelID        string          `json:"requestModelId,omitempty"`
	RequestID             string          `json:"requestId,omitempty"`
	NotSent               bool            `json:"notSent,omitempty"`
	CompletedAt           *time.Time      `json:"completedAt,omitempty"`
	ID                    string          `json:"id"`
	Stage                 string          `json:"stage"`
	Start                 int             `json:"start"`
	End                   int             `json:"end"`
	Status                CallStatus      `json:"status"`
	Request               json.RawMessage `json:"request,omitempty"`
	Response              string          `json:"response,omitempty"`
	Text                  string          `json:"text,omitempty"`
	Reasoning             string          `json:"reasoning,omitempty"`
	// 原生签名内容作为不透明字符串保存，防止 jsonb 重写运输表示。
	// 恢复时由供应商适配器读回，领域层不解析签名或思考。
	ProviderState    string                `json:"providerState,omitempty"`
	Actions          []Action              `json:"actions,omitempty"`
	FinishReason     string                `json:"finishReason,omitempty"`
	Usage            billing.Usage         `json:"usage"`
	ReservedMicroCNY int64                 `json:"reservedMicroCny"`
	CostMicroCNY     int64                 `json:"costMicroCny"`
	Pricing          *billing.TokenPricing `json:"pricing,omitempty"`
	ErrorCode        string                `json:"errorCode,omitempty"`
	Error            string                `json:"error,omitempty"`
	Before           []Asset               `json:"before"`
	After            []Asset               `json:"after,omitempty"`
	CreatedAt        time.Time             `json:"createdAt"`
}

// Action 是持久化工具回执，状态/结果与目录同次提交。
// 恢复时只执行尚未采用的动作，不能把模型自述当作保存成功。
type Action struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Arguments string       `json:"arguments"`
	Status    ActionStatus `json:"status"`
	Result    string       `json:"result,omitempty"`
}

type Run struct {
	EndedAt *time.Time      `json:"endedAt,omitempty"`
	Credit  *credits.Charge `json:"credit,omitempty"`
	// 仅新项目首次解析及其明确重试携带；索引完成时原子接续资产任务。
	ContinueAssetPreparation bool `json:"continueAssetPreparation,omitempty"`

	StoryboardScope *StoryboardScope `json:"storyboardScope,omitempty"`
	ID              string           `json:"id"`
	Contract        string           `json:"contract"`
	SourceVersion   uint64           `json:"sourceVersion"`
	Status          RunStatus        `json:"status"`
	Stage           string           `json:"stage"`
	Cursor          int              `json:"cursor"`
	End             int              `json:"end,omitempty"`
	Calls           []Call           `json:"calls"`
	ErrorCode       string           `json:"errorCode,omitempty"`
	Error           string           `json:"error,omitempty"`
	CreatedAt       time.Time        `json:"createdAt"`
	InputRevision   uint64           `json:"inputRevision,omitempty"`
	Model           string           `json:"model,omitempty"`
	BudgetMicroCNY  int64            `json:"budgetMicroCny,omitempty"`
	Targets         []VisualTarget   `json:"targets,omitempty"`
	Target          *VisualTarget    `json:"target,omitempty"`
}

const OutlineContract = "outline-agent-v1"

const VisualContract = "visual-prompt-agent-v1"

const EmptyVisualError = "P3 已完成，但角色、场景、道具总数为 0，没有可生成视觉提示词的资产。"

type ActionRef struct {
	RunID    string `json:"runId"`
	CallID   string `json:"callId"`
	ActionID string `json:"actionId"`
}

func KnownContract(c string) bool {
	return c == CatalogExtractionContract || c == OutlineContract || c == VisualContract || c == EpisodeIndexContract || c == StoryboardContract
}

func (p *Project) LatestRunByContract(contract string) *Run {
	for i := len(p.Runs) - 1; i >= 0; i-- {
		r := &p.Runs[i]
		if r.Contract == contract && (contract == OutlineContract || r.SourceVersion == p.ScriptSource.Version) {
			return r
		}
	}
	return nil
}

func (p *Project) FailedTask() *Run {
	for _, c := range []string{OutlineContract, CatalogExtractionContract} {
		r := p.LatestRunByContract(c)
		if c == CatalogExtractionContract {
			r = p.LatestCatalogRun()
		}
		if r != nil && r.Status == "failed" && !(r.Contract == CatalogExtractionContract && r.Stage == "visualPrompt" && len(p.Assets) > 0) {
			return r
		}
	}
	for i := range p.Runs {
		r := &p.Runs[i]
		if r.Contract == VisualContract && r.SourceVersion == p.ScriptSource.Version && r.Target == nil && len(r.Targets) == 0 && r.Status == "failed" {
			return r
		}
	}
	return nil
}

func (p *Project) HasCancelledTask() bool {
	for _, c := range []string{OutlineContract, CatalogExtractionContract} {
		r := p.LatestRunByContract(c)
		if c == CatalogExtractionContract {
			r = p.LatestCatalogRun()
		}
		if r != nil && r.Status == "cancelled" && !(r.Contract == CatalogExtractionContract && r.Stage == "visualPrompt" && len(p.Assets) > 0) {
			return true
		}
	}
	return false
}

func (p *Project) Active() bool {
	for _, r := range p.Runs {
		if r.Status == "running" {
			return true
		}
	}
	return false
}

func (p *Project) Asset(id string) *Asset {
	for i := range p.Assets {
		if p.Assets[i].ID == id {
			return &p.Assets[i]
		}
	}
	return nil
}

// 视觉任务始终序列化冻结范围，空目录失败也显式 targets=[]。
// 其他任务保留原字段投影，避免结构重构改变历史合同。
func (r Run) MarshalJSON() ([]byte, error) {
	type plain Run
	if r.ExecutionContract() == VisualContract {
		return json.Marshal(struct {
			plain
			Targets []VisualTarget `json:"targets"`
		}{plain(r), append([]VisualTarget{}, r.Targets...)})
	}
	return json.Marshal(plain(r))
}
