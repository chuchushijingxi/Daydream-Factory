package scriptimport

import d "drama-agent/internal/domain/project"

type Importer struct{}

// Parse 完整保留原文段落，只处理文件格式与稳定来源 ID，不推断剧情边界。
func (Importer) Parse(filename string, raw []byte, version uint64) (d.ScriptSource, error) {
	return Parse(filename, raw, version)
}
