package localembedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/domain/fault"
)

// Remote 调用独立检索服务（deploy/embedding/server.py）。服务加载与本机模式同一份
// embed.py 和模型，并报告同样算法的身份，切换模式不会让已有索引失效。
type Remote struct {
	endpoint, identity string
	client             *http.Client
	settings           appconfig.Evidence
}

const identityPrefix = "bge-small-zh-v1.5:cls-l2:"

// NewRemote 读取服务报告的模型身份；服务尚未就绪时返回错误，由检索的懒加载在下次使用时重试。
func NewRemote(endpoint string, settings appconfig.Evidence) (*Remote, error) {
	r := &Remote{endpoint: strings.TrimRight(endpoint, "/"), client: &http.Client{}, settings: settings}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint+"/v1/identity", nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding service unavailable: %w", err)
	}
	defer resp.Body.Close()
	var info struct {
		Identity  string `json:"identity"`
		Dimension int    `json:"dimension"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&info) != nil ||
		!strings.HasPrefix(info.Identity, identityPrefix) || info.Dimension != 512 {
		return nil, fmt.Errorf("embedding service returned an invalid identity")
	}
	r.identity = info.Identity
	return r, nil
}

func (r *Remote) Identity() string { return r.identity }
func (r *Remote) Dimension() int   { return 512 }

func (r *Remote) Embed(ctx context.Context, texts []string, query bool) ([][]float32, error) {
	if len(texts) == 0 || len(texts) > 32 {
		return nil, fault.ErrInvalid
	}
	ctx, cancel := embedContext(ctx, r.settings.EmbeddingTimeout)
	defer cancel()
	raw, err := json.Marshal(map[string]any{"texts": texts, "query": query})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/v1/embed", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fault.New("EVIDENCE_UNAVAILABLE", fmt.Sprintf("检索服务不可用：%v", err))
	}
	defer resp.Body.Close()
	var result struct {
		Vectors [][]float32 `json:"vectors"`
		Error   string      `json:"error"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&result); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fault.New("EVIDENCE_INDEX_INVALID", "检索服务响应无法解析")
	}
	if resp.StatusCode == http.StatusBadRequest {
		return nil, fault.ErrInvalid
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fault.New("EVIDENCE_UNAVAILABLE", fmt.Sprintf("检索服务返回 %d", resp.StatusCode))
	}
	if result.Error != "" {
		return nil, fault.New("EVIDENCE_CAPACITY_EXCEEDED", "本次检索输入超过本地 Embedding 容量；未截断或外发")
	}
	if len(result.Vectors) != len(texts) {
		return nil, fault.New("EVIDENCE_INDEX_INVALID", "检索服务返回的向量数量不符")
	}
	for _, vector := range result.Vectors {
		if len(vector) != 512 {
			return nil, fault.New("EVIDENCE_INDEX_INVALID", "检索服务返回的向量维度不符")
		}
	}
	return result.Vectors, nil
}
