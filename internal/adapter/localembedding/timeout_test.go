package localembedding

import (
	"context"
	"os"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
)

// 生产装配按 cmd/server/evidence.go 传入完整 Evidence 配置；历史测试只做两参
// 构造，EmbeddingTimeout 为零，覆盖不到超时分支。本用例锁住该真实路径。
func TestLocalBGEEmbeddingWithProductionTimeoutSetting(t *testing.T) {
	if os.Getenv("DRAMA_TEST_LOCAL_EMBEDDING") != "1" {
		t.Skip("explicit local free model opt-in")
	}
	e, err := New("../../../.data/evidence-embedding/runtime/bin/python", "../../../.data/evidence-embedding/model", appconfig.Evidence{EmbeddingTimeout: 2 * time.Minute, ProcessWaitDelay: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.Embed(context.Background(), []string{"人物左眉有什么疤痕"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 1 || len(v[0]) != 512 {
		t.Fatal("wrong encoder dimensions")
	}
}
