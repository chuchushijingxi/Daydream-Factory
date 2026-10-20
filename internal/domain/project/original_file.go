package project

// OriginalFile 保留既有归档剧本的对象坐标；调用者不能提供这些键。
// 新项目引用独立上传模块的 SourceFileID。
type OriginalFile struct {
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"objectKey"`
	SizeBytes   int64  `json:"sizeBytes"`
	ContentType string `json:"contentType"`
}
