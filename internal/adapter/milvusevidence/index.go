// Package milvusevidence stores only a rebuildable vector index. Original
// paragraphs and project/task authority remain in ProjectStore.
package milvusevidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

const description = "drama-agent visual evidence v1: derived paragraph index"

type Index struct {
	endpoint, collection string
	embedder             ports.Embedder
	client               *http.Client
}

func New(endpoint, collection string, embedder ports.Embedder, settings ...appconfig.Evidence) (*Index, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" || collection == "" || embedder == nil || embedder.Dimension() <= 0 {
		return nil, fmt.Errorf("local Milvus endpoint, collection and embedder are required")
	}
	var options appconfig.Evidence
	if len(settings) > 0 {
		options = settings[0]
	}
	return &Index{endpoint: strings.TrimRight(endpoint, "/"), collection: collection, embedder: embedder, client: &http.Client{Timeout: options.RequestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (s *Index) call(ctx context.Context, path string, body map[string]any, data any) error {
	body["collectionName"] = s.collection
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.endpoint+"/v2/vectordb/"+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return fault.New("EVIDENCE_UNAVAILABLE", "Milvus "+path+": "+err.Error())
	}
	defer res.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return fault.New("EVIDENCE_UNAVAILABLE", "Milvus response incomplete or oversized")
	}
	var wire struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(raw, &wire) != nil || wire.Code == nil {
		return fault.New("EVIDENCE_UNAVAILABLE", fmt.Sprintf("Milvus %s HTTP %d or invalid response", path, res.StatusCode))
	}
	if *wire.Code != 0 {
		return fault.New("EVIDENCE_UNAVAILABLE", fmt.Sprintf("Milvus %s code %d: %s", path, *wire.Code, wire.Message))
	}
	if data != nil {
		if err = json.Unmarshal(wire.Data, data); err != nil {
			return fault.New("EVIDENCE_INDEX_INVALID", "Milvus returned invalid result data")
		}
	}
	return nil
}

func (s *Index) collectionReady(ctx context.Context) error {
	var has struct {
		Has bool `json:"has"`
	}
	if err := s.call(ctx, "collections/has", map[string]any{}, &has); err != nil {
		return err
	}
	if !has.Has {
		err := s.call(ctx, "collections/create", map[string]any{"description": description, "dimension": s.embedder.Dimension(), "metricType": "COSINE", "idType": "VarChar", "autoId": false, "primaryFieldName": "id", "vectorFieldName": "vector", "params": map[string]any{"max_length": "64", "consistencyLevel": "Strong"}}, nil)
		if err != nil {
			// 并发创建可接受的前提是下方实际校验到完全一致的归属与 schema。
			if check := s.call(ctx, "collections/has", map[string]any{}, &has); check != nil || !has.Has {
				return err
			}
		}
	}
	var info struct {
		Description string `json:"description"`
		Fields      []struct {
			Name   string                        `json:"name"`
			Type   string                        `json:"type"`
			Params []struct{ Key, Value string } `json:"params"`
		} `json:"fields"`
	}
	if err := s.call(ctx, "collections/describe", map[string]any{}, &info); err != nil {
		return err
	}
	valid := false
	for _, f := range info.Fields {
		if f.Name == "vector" && f.Type == "FloatVector" {
			for _, p := range f.Params {
				if p.Key == "dim" && p.Value == strconv.Itoa(s.embedder.Dimension()) {
					valid = true
				}
			}
		}
	}
	if info.Description != description || !valid {
		return fault.New("EVIDENCE_INDEX_INVALID", "Milvus collection ownership or embedding dimension mismatch")
	}
	return nil
}

type chunk struct {
	ID, SpanID, Text string
	Index            int
}

func (s *Index) document(doc ports.EvidenceDocument) (string, []chunk, error) {
	if doc.ProjectID == "" || doc.Source.Version == 0 || len(doc.Source.Spans) == 0 {
		return "", nil, fault.ErrInvalid
	}
	// 对完整实际内容连同源版本与编码器身份一起做哈希；导入 SHA 不可信时，
	// 不同的字节也不能复用旧索引。
	raw, err := json.Marshal(struct {
		Model, Project string
		Source         any
	}{s.embedder.Identity(), doc.ProjectID, doc.Source})
	if err != nil {
		return "", nil, err
	}
	h := sha256.Sum256(raw)
	scope := hex.EncodeToString(h[:])
	chunks := []chunk{}
	seen := map[string]bool{}
	for _, span := range doc.Source.Spans {
		if span.ID == "" || seen[span.ID] || !utf8.ValidString(span.Text) {
			return "", nil, fault.ErrInvalid
		}
		seen[span.ID] = true
		// 无损 UTF-8 窗口保护本地编码器的 token 容量；这里只是索引分块，
		// 读取工具始终返回完整段落。
		text := span.Text
		for part := 0; ; part++ {
			n := min(384, len(text))
			for n < len(text) && !utf8.RuneStart(text[n]) {
				n--
			}
			id := sha256.Sum256([]byte(scope + "\x00" + span.ID + "\x00" + strconv.Itoa(part)))
			chunks = append(chunks, chunk{hex.EncodeToString(id[:]), span.ID, text[:n], part})
			text = text[n:]
			if text == "" {
				break
			}
		}
	}
	return scope, chunks, nil
}

func filter(doc ports.EvidenceDocument, scope string) string {
	p, _ := json.Marshal(doc.ProjectID)
	h, _ := json.Marshal(scope)
	return "projectId == " + string(p) + " and sourceVersion == " + strconv.FormatUint(doc.Source.Version, 10) + " and sourceHash == " + string(h)
}

func (s *Index) count(ctx context.Context, condition string) (int, error) {
	var data []map[string]int
	err := s.call(ctx, "entities/query", map[string]any{"filter": condition, "outputFields": []string{"count(*)"}, "consistencyLevel": "Strong"}, &data)
	if err != nil {
		return 0, err
	}
	if len(data) != 1 {
		return 0, fault.New("EVIDENCE_INDEX_INVALID", "Milvus count missing")
	}
	n, ok := data[0]["count(*)"]
	if !ok || n < 0 {
		return 0, fault.New("EVIDENCE_INDEX_INVALID", "Milvus count invalid")
	}
	return n, nil
}

func (s *Index) vectors(ctx context.Context, texts []string, query bool) ([][]float32, error) {
	v, err := s.embedder.Embed(ctx, texts, query)
	if err != nil {
		return nil, err
	}
	if len(v) != len(texts) {
		return nil, fault.New("EVIDENCE_INDEX_INVALID", "embedding result count mismatch")
	}
	for _, row := range v {
		if len(row) != s.embedder.Dimension() {
			return nil, fault.New("EVIDENCE_INDEX_INVALID", "embedding dimension mismatch")
		}
		var norm float64
		for _, x := range row {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				return nil, fault.New("EVIDENCE_INDEX_INVALID", "embedding is not finite")
			}
			norm += float64(x) * float64(x)
		}
		if norm == 0 {
			return nil, fault.New("EVIDENCE_INDEX_INVALID", "zero embedding")
		}
	}
	return v, nil
}

func (s *Index) Ensure(ctx context.Context, doc ports.EvidenceDocument) error {
	// 即使已有完整索引，每个视觉任务也要在文本调用前验证本地编码器能
	// 实际运行。固定探针不读取项目原文，不落库、不修改模型或索引。
	if _, err := s.vectors(ctx, []string{"原文检索运行检查"}, true); err != nil {
		return err
	}
	return s.ensureIndex(ctx, doc)
}

func (s *Index) ensureIndex(ctx context.Context, doc ports.EvidenceDocument) error {
	scope, chunks, err := s.document(doc)
	if err != nil {
		return err
	}
	if err = s.collectionReady(ctx); err != nil {
		return err
	}
	n, err := s.count(ctx, filter(doc, scope))
	if err != nil {
		return err
	}
	if n == len(chunks) {
		return nil
	}
	for start := 0; start < len(chunks); start += 32 {
		end := min(start+32, len(chunks))
		texts := []string{}
		for _, c := range chunks[start:end] {
			texts = append(texts, c.Text)
		}
		vectors, e := s.vectors(ctx, texts, false)
		if e != nil {
			return e
		}
		rows := []map[string]any{}
		for i, c := range chunks[start:end] {
			rows = append(rows, map[string]any{"id": c.ID, "vector": vectors[i], "projectId": doc.ProjectID, "sourceVersion": doc.Source.Version, "sourceHash": scope, "spanId": c.SpanID, "chunkIndex": c.Index})
		}
		var written struct {
			Count int `json:"upsertCount"`
		}
		if e = s.call(ctx, "entities/upsert", map[string]any{"data": rows}, &written); e != nil {
			return e
		}
		if written.Count != len(rows) {
			return fault.New("EVIDENCE_INDEX_INVALID", "Milvus upsert was incomplete")
		}
	}
	n, err = s.count(ctx, filter(doc, scope))
	if err != nil {
		return err
	}
	if n != len(chunks) {
		return fault.New("EVIDENCE_INDEX_INVALID", "complete source index is not visible")
	}
	return nil
}

func (s *Index) Search(ctx context.Context, doc ports.EvidenceDocument, query string, limit, offset int) ([]ports.EvidenceHit, bool, error) {
	if strings.TrimSpace(query) == "" || limit < 1 || limit > 20 || offset < 0 || offset+limit+1 >= 16384 {
		return nil, false, fault.ErrInvalid
	}
	// 搜索本身会执行真实查询向量化，无需重复运行任务启动用的固定探针。
	if err := s.ensureIndex(ctx, doc); err != nil {
		return nil, false, err
	}
	scope, _, err := s.document(doc)
	if err != nil {
		return nil, false, err
	}
	v, err := s.vectors(ctx, []string{query}, true)
	if err != nil {
		return nil, false, err
	}
	var rows []struct {
		SpanID        string  `json:"spanId"`
		ChunkIndex    int     `json:"chunkIndex"`
		Distance      float64 `json:"distance"`
		ProjectID     string  `json:"projectId"`
		SourceVersion uint64  `json:"sourceVersion"`
		SourceHash    string  `json:"sourceHash"`
	}
	err = s.call(ctx, "entities/search", map[string]any{"data": v, "annsField": "vector", "filter": filter(doc, scope), "limit": limit + 1, "offset": offset, "outputFields": []string{"spanId", "chunkIndex", "projectId", "sourceVersion", "sourceHash"}, "searchParams": map[string]any{"metricType": "COSINE"}, "consistencyLevel": "Strong"}, &rows)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := []ports.EvidenceHit{}
	for _, r := range rows {
		if r.ProjectID != doc.ProjectID || r.SourceVersion != doc.Source.Version || r.SourceHash != scope || r.ChunkIndex < 0 || math.IsNaN(r.Distance) || math.IsInf(r.Distance, 0) {
			return nil, false, fault.New("EVIDENCE_INDEX_INVALID", "Milvus hit crosses source/project isolation")
		}
		out = append(out, ports.EvidenceHit{SpanID: r.SpanID, ChunkIndex: r.ChunkIndex, Score: r.Distance})
	}
	return out, more, nil
}
