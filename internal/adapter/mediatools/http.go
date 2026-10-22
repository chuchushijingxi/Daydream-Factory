package mediatools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

// 媒体服务的 HTTP 约定只在本文件定义一次，服务端（Handler）与客户端（Remote）共用。
const (
	thumbnailPath = "/v1/thumbnail"
	probePath     = "/v1/video/probe"
	encodePath    = "/v1/video/encode"
	HealthPath    = "/healthz"
	widthHeader   = "X-Media-Width"
	heightHeader  = "X-Media-Height"
	invalidCode   = "INVALID_INPUT"
)

type failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type probeResult struct {
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	DurationSeconds float64 `json:"durationSeconds"`
}

// Handler 是媒体服务的 HTTP 入口：只在 Compose 内网提供，不直接面向浏览器。
func Handler(tools Tools) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("POST "+thumbnailPath, func(w http.ResponseWriter, r *http.Request) {
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		options := ports.ThumbnailOptions{MaxWidth: intParam(r, "maxWidth"), MaxHeight: intParam(r, "maxHeight")}
		image, err := tools.Generate(r.Context(), raw, options)
		if err != nil {
			writeFailure(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set(widthHeader, strconv.Itoa(image.Width))
		w.Header().Set(heightHeader, strconv.Itoa(image.Height))
		_, _ = w.Write(image.Data)
	})
	mux.HandleFunc("POST "+probePath, func(w http.ResponseWriter, r *http.Request) {
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		width, height, seconds, err := tools.ProbeVideo(r.Context(), raw)
		if err != nil {
			writeFailure(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(probeResult{Width: width, Height: height, DurationSeconds: seconds})
	})
	mux.HandleFunc("POST "+encodePath, func(w http.ResponseWriter, r *http.Request) {
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		seconds, err := strconv.ParseFloat(r.URL.Query().Get("seconds"), 64)
		if err != nil {
			writeFailure(w, fault.ErrInvalid)
			return
		}
		out, err := tools.EncodeVideo(r.Context(), raw, intParam(r, "width"), intParam(r, "height"), seconds)
		if err != nil {
			writeFailure(w, err)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(out)
	})
	return mux
}

func intParam(r *http.Request, name string) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return 0
	}
	return v
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxMediaBytes+1))
	if err != nil || len(raw) > MaxMediaBytes {
		writeFailure(w, fault.New("MEDIA_TOO_LARGE", "媒体文件超过 128 MB 上限"))
		return nil, false
	}
	return raw, true
}

// writeFailure 原样传回业务错误码，客户端据此还原为同一个错误。
func writeFailure(w http.ResponseWriter, err error) {
	status, body := http.StatusInternalServerError, failure{Code: "MEDIA_FAILED", Message: "媒体处理失败"}
	var f *fault.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		status, body = http.StatusGatewayTimeout, failure{Code: "MEDIA_TIMEOUT", Message: "媒体处理超时"}
	case errors.Is(err, fault.ErrInvalid):
		status, body = http.StatusBadRequest, failure{Code: invalidCode, Message: "媒体处理参数无效"}
	case errors.As(err, &f):
		status, body = http.StatusUnprocessableEntity, failure{Code: f.Code, Message: f.Message}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Remote 通过 Compose 内网调用独立媒体服务。
type Remote struct {
	endpoint string
	client   *http.Client
}

func NewRemote(endpoint string) *Remote {
	return &Remote{endpoint: endpoint, client: &http.Client{}}
}

func (r *Remote) call(ctx context.Context, path string, query url.Values, body []byte) ([]byte, http.Header, error) {
	if len(body) > MaxMediaBytes {
		return nil, nil, fault.New("MEDIA_TOO_LARGE", "媒体文件超过 128 MB 上限")
	}
	target := r.endpoint + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fault.New("MEDIA_UNAVAILABLE", fmt.Sprintf("媒体服务不可用：%v", err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxMediaBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fault.New("MEDIA_UNAVAILABLE", fmt.Sprintf("媒体服务响应中断：%v", err))
	}
	if resp.StatusCode != http.StatusOK {
		var f failure
		if json.Unmarshal(raw, &f) != nil || f.Code == "" {
			return nil, nil, fault.New("MEDIA_UNAVAILABLE", fmt.Sprintf("媒体服务返回 %d", resp.StatusCode))
		}
		if f.Code == invalidCode {
			return nil, nil, fault.ErrInvalid
		}
		if f.Code == "MEDIA_TIMEOUT" && ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fault.New(f.Code, f.Message)
	}
	if len(raw) > MaxMediaBytes {
		return nil, nil, fault.New("MEDIA_FAILED", "媒体服务响应超过大小上限")
	}
	return raw, resp.Header, nil
}

func (r *Remote) Generate(ctx context.Context, raw []byte, options ports.ThumbnailOptions) (ports.ThumbnailImage, error) {
	query := url.Values{"maxWidth": {strconv.Itoa(options.MaxWidth)}, "maxHeight": {strconv.Itoa(options.MaxHeight)}}
	data, header, err := r.call(ctx, thumbnailPath, query, raw)
	if err != nil {
		return ports.ThumbnailImage{}, err
	}
	width, werr := strconv.Atoi(header.Get(widthHeader))
	height, herr := strconv.Atoi(header.Get(heightHeader))
	if werr != nil || herr != nil || width < 1 || height < 1 || width > options.MaxWidth || height > options.MaxHeight || len(data) == 0 || http.DetectContentType(data) != "image/webp" {
		return ports.ThumbnailImage{}, fault.New("THUMBNAIL_FAILED", "缩略图格式或实际尺寸核对失败")
	}
	return ports.ThumbnailImage{Data: data, Width: width, Height: height}, nil
}

func (r *Remote) ProbeVideo(ctx context.Context, raw []byte) (int, int, float64, error) {
	data, _, err := r.call(ctx, probePath, nil, raw)
	if err != nil {
		return 0, 0, 0, err
	}
	var p probeResult
	if json.Unmarshal(data, &p) != nil || p.Width < 1 || p.Height < 1 || p.DurationSeconds <= 0 {
		return 0, 0, 0, fault.New("TASK_RESULT_INVALID", "视频核对结果无效")
	}
	return p.Width, p.Height, p.DurationSeconds, nil
}

func (r *Remote) EncodeVideo(ctx context.Context, input []byte, width, height int, seconds float64) ([]byte, error) {
	query := url.Values{"width": {strconv.Itoa(width)}, "height": {strconv.Itoa(height)}, "seconds": {strconv.FormatFloat(seconds, 'f', -1, 64)}}
	data, _, err := r.call(ctx, encodePath, query, input)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fault.New("MOCK_ENCODING_FAILED", "本地 Mock 视频编码结果大小无效")
	}
	return data, nil
}
