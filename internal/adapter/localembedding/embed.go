package localembedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/domain/fault"
)

//go:embed embed.py
var worker string

// ModelFiles are the runtime files hashed into the index identity, in order;
// deploy/embedding/server.py hashes the same list.
var ModelFiles = []string{"model.onnx", "config.json", "tokenizer.json"}

type Embedder struct {
	python, model, identity string
	settings                appconfig.Evidence
}

func New(python, folder string, settings ...appconfig.Evidence) (*Embedder, error) {
	python, err := filepath.Abs(python)
	if err != nil {
		return nil, err
	}
	folder, err = filepath.Abs(folder)
	if err != nil {
		return nil, err
	}
	if info, e := os.Stat(python); e != nil || info.IsDir() {
		return nil, fmt.Errorf("local embedding Python is missing; see setup-local-embedding.py")
	}
	h := sha256.New()
	h.Write([]byte(worker))
	for _, name := range ModelFiles {
		f, e := os.Open(filepath.Join(folder, name))
		if e != nil {
			return nil, fmt.Errorf("local BGE model file %s is missing", name)
		}
		h.Write([]byte(name))
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return nil, e
		}
	}
	var options appconfig.Evidence
	if len(settings) > 0 {
		options = settings[0]
	}
	return &Embedder{settings: options, python: python, model: folder, identity: "bge-small-zh-v1.5:cls-l2:" + hex.EncodeToString(h.Sum(nil))}, nil
}

func (e *Embedder) Identity() string { return e.identity }
func (e *Embedder) Dimension() int   { return 512 }

// embedContext 派生执行上下文；超时与非超时两条路径都必须直接派生自父上下文。
// 不能先包一层再取消：已取消父上下文的子上下文生来就是取消状态，命令无法启动。
func embedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(parent, timeout)
	}
	return context.WithCancel(parent)
}

func (e *Embedder) Embed(ctx context.Context, texts []string, query bool) ([][]float32, error) {
	if len(texts) == 0 || len(texts) > 32 {
		return nil, fault.ErrInvalid
	}
	ctx, cancel := embedContext(ctx, e.settings.EmbeddingTimeout)
	defer cancel()
	raw, err := json.Marshal(map[string]any{"texts": texts, "query": query})
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, e.python, "-c", worker, e.model)
	// 工作进程不继承供应商凭证或模型端点等环境变量。
	cmd.Env = []string{"HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "HF_HUB_DISABLE_TELEMETRY=1", "TOKENIZERS_PARALLELISM=false", "CUDA_VISIBLE_DEVICES="}
	for _, name := range []string{"PATH", "TMPDIR", "LANG"} {
		if v, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+v)
		}
	}
	cmd.Stdin = bytes.NewReader(raw)
	cmd.WaitDelay = e.settings.ProcessWaitDelay
	var output, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostic
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// 离线工作进程不打印请求文本或环境值；保留真实诊断以便修复运行故障。
		return nil, fault.New("EVIDENCE_UNAVAILABLE", fmt.Sprintf("本地 BGE Embedding 执行失败: %v; %s", err, diagnostic.String()))
	}
	var result struct {
		Vectors [][]float32 `json:"vectors"`
		Error   string      `json:"error"`
	}
	if err = json.Unmarshal(output.Bytes(), &result); err != nil {
		return nil, fault.New("EVIDENCE_INDEX_INVALID", "本地 Embedding 响应无法解析")
	}
	if result.Error != "" {
		return nil, fault.New("EVIDENCE_CAPACITY_EXCEEDED", "本次检索输入超过本地 Embedding 容量；未截断或外发")
	}
	return result.Vectors, nil
}
