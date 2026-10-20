package localembedding

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
)

func TestLocalBGEOfflineLearnedEmbeddingsAndNoTruncation(t *testing.T) {
	if os.Getenv("DRAMA_TEST_LOCAL_EMBEDDING") != "1" {
		t.Skip("explicit local free model opt-in")
	}
	// 与生产装配一样显式配置超时，避免只覆盖零超时路径。
	e, err := New("../../../.data/evidence-embedding/runtime/bin/python", "../../../.data/evidence-embedding/model", appconfig.Evidence{EmbeddingTimeout: 2 * time.Minute, ProcessWaitDelay: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	docs, err := e.Embed(context.Background(), []string{"她摘下帽子，眉毛左侧的旧伤清晰可见。", "庭院里的花盆刚刚浇过水。"}, false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := e.Embed(context.Background(), []string{"人物左眉有什么疤痕"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 1 || len(docs) != 2 || len(q[0]) != 512 || len(docs[0]) != 512 {
		t.Fatal("wrong encoder dimensions")
	}
	sim := func(v []float32) float64 {
		var sum float64
		for i, x := range v {
			sum += float64(x * q[0][i])
		}
		return sum
	}
	if sim(docs[0]) <= sim(docs[1]) {
		t.Fatalf("fictional semantic probe failed: scar=%f garden=%f", sim(docs[0]), sim(docs[1]))
	}
	if _, err = e.Embed(context.Background(), []string{strings.Repeat("伤", 600)}, true); err == nil || !strings.Contains(err.Error(), "EVIDENCE_CAPACITY_EXCEEDED") {
		t.Fatal("encoder truncated overlong query", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = e.Embed(ctx, []string{"不会外发"}, true); err == nil {
		t.Fatal("cancel ignored")
	}
	t.Logf("offline BGE512 semantic probe: scar=%f garden=%f; no external generation", sim(docs[0]), sim(docs[1]))
}

func TestLocalEmbeddingMissingResourcesAreExplicit(t *testing.T) {
	if _, err := New("missing-python", t.TempDir()); err == nil {
		t.Fatal("missing runtime accepted")
	}
}
