package modelconfig

const MockProtocol = "mock-media"
const MockImageModelID = "mock-image"
const MockVideoModelID = "mock_video"

// 历史已受理任务使用未裁剪的内嵌样片；这些事实为其不可变的执行与计费保留，不作为新任务默认值。
const MockVideoDurationSeconds = 10.793333
const MockVideoBillingSeconds int64 = 11

// MockBehavior 是运营配置，按模型独立持久化；延迟须落在既有两分钟任务观察窗内。
type MockBehavior struct {
	DelaySeconds   int `json:"delaySeconds"`
	FailurePercent int `json:"failurePercent"`
}

func (b MockBehavior) Valid() bool {
	return b.DelaySeconds >= 0 && b.DelaySeconds <= 60 && b.FailurePercent >= 0 && b.FailurePercent <= 100
}

// MockExecution 对任务内核不透明：失败在受理前抽样并随绑定保存，worker 与轮询不再抽样。
type MockExecution struct {
	MockBehavior
	Fail bool `json:"fail"`
	// 由解析器为新视频任务冻结；历史已受理样片任务无此标记，须保留原输出与计费口径。
	VideoOptions bool `json:"videoOptions,omitempty"`
}

func IsMockModel(id, category string) bool {
	return (id == MockImageModelID && category == "image") || (id == MockVideoModelID && category == "video")
}

// MockOperation 是 Mock 模型类别与任务 operation 的唯一映射；
// 请求侧校验与启动侧推导共用同一份，新增类别不得两处分叉。
func MockOperation(category string) string {
	switch category {
	case "image":
		return "image.generate"
	case "video":
		return "video.generate"
	default:
		return ""
	}
}

func mockPreset() ModelConnection {
	return ModelConnection{ID: "mock", Name: "Mock 本地测试", Kind: "builtin", Protocol: MockProtocol, Models: []ConfiguredModel{
		{ID: MockImageModelID, ModelID: MockImageModelID, Category: "image", MockBehavior: &MockBehavior{}},
		{ID: MockVideoModelID, ModelID: MockVideoModelID, Category: "video", MockBehavior: &MockBehavior{}},
	}}
}
