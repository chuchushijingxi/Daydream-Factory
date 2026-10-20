package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

// FetchModelDirectory 只执行一次明确的目录 GET，不发起生成请求。
// 禁止重定向携带凭证到其他主机。
func FetchModelDirectory(ctx context.Context, key, endpoint, protocol string) ([]ports.RemoteModel, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/models", nil)
	if e != nil {
		return nil, fault.ErrInvalid
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	failed := func(message string) error { return fault.New("MODEL_DIRECTORY_FAILED", message) }
	if e != nil {
		return nil, failed("获取模型列表失败，请检查地址、网络和凭证；也可手动添加")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, failed("中转站未返回模型列表，请核对地址和凭证；也可手动添加")
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return nil, failed("模型列表读取失败或超过大小限制")
	}
	var body struct {
		Data *[]struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Data == nil || len(*body.Data) > 1000 {
		return nil, failed("接口未返回有效模型目录；可手动添加")
	}
	seen := map[string]bool{}
	models := []ports.RemoteModel{}
	for _, m := range *body.Data {
		if m.ID == "" || len(m.ID) > 200 || strings.TrimSpace(m.ID) != m.ID || strings.ContainsAny(m.ID, "\r\n\x00") {
			return nil, failed("模型目录包含无效ID；可手动添加")
		}
		if !seen[m.ID] {
			models = append(models, ports.RemoteModel{ModelID: m.ID})
			seen[m.ID] = true
		}
	}
	return models, nil
}
