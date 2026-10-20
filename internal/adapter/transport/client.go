package transport

import (
	"context"
	"drama-agent/internal/adapter/appconfig"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type requestObserverKey struct{}

// 外发前保存适配器实际序列化的正文；不接收包含凭证的请求头。
// 观察回调失败表示请求未发出，不能带着缺失的记账证据继续运输。
func WithRequestObserver(ctx context.Context, observe func([]byte) error) context.Context {
	return context.WithValue(ctx, requestObserverKey{}, observe)
}

func ObserveRequest(ctx context.Context, raw []byte) error {
	if observe, ok := ctx.Value(requestObserverKey{}).(func([]byte) error); ok {
		return observe(raw)
	}
	return nil
}

// 只有观察到连接建立失败，才在运输层重连一次。
// 一旦获得连接，POST 可能已到达供应商，不能在这里重发；响应中断的限次重试
// 由应用层负责，每次重试先独立保存调用意图和费用预留，未知费用不能释放。
func NewClient(settings ...appconfig.Models) *http.Client {
	var delay time.Duration
	if len(settings) > 0 {
		delay = settings[0].ConnectionRetryDelay
	}
	return &http.Client{Transport: &connectionTransport{base: http.DefaultTransport, retryDelay: delay}}
}

type connectionTransport struct {
	base       http.RoundTripper
	retryDelay time.Duration
}

type unsentError struct {
	cause   error
	network bool
}

func (e *unsentError) NetworkInterrupted() bool { return e.network }

func (e *unsentError) Error() string { return e.cause.Error() }

func (e *unsentError) Unwrap() error { return e.cause }

func (e *unsentError) RequestNotSent() bool { return true }

func (t *connectionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if _, ok := request.Context().Value(requestObserverKey{}).(func([]byte) error); ok {
		if request.GetBody == nil {
			return nil, &unsentError{cause: errors.New("model request body cannot be observed")}
		}
		body, err := request.GetBody()
		if err != nil {
			return nil, &unsentError{cause: err}
		}
		raw, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			return nil, &unsentError{cause: errors.New("model request observation failed")}
		}
		if err = ObserveRequest(request.Context(), raw); err != nil {
			return nil, &unsentError{cause: err}
		}
	}
	current := request
	for attempt := 0; ; attempt++ {
		var setup, acquired atomic.Bool
		trace := &httptrace.ClientTrace{
			DNSStart:          func(httptrace.DNSStartInfo) { setup.Store(true) },
			ConnectStart:      func(string, string) { setup.Store(true) },
			TLSHandshakeStart: func() { setup.Store(true) },
			GotConn:           func(httptrace.GotConnInfo) { acquired.Store(true) },
		}
		response, err := t.base.RoundTrip(current.WithContext(httptrace.WithClientTrace(current.Context(), trace)))
		// 固定版本的流式 SDK 不关闭非 2xx 正文。读尽后释放连接，同时保留原字节、
		// 状态及错误；成功流的关闭仍归调用者，不能提前中断正常响应。
		if response != nil && response.StatusCode >= 300 && response.Body != nil {
			response.Body = &errorResponseBody{ReadCloser: response.Body}
		}
		unsent := response == nil && err != nil && setup.Load() && !acquired.Load()
		if err == nil || response != nil || attempt != 0 || !setup.Load() || acquired.Load() || request.Context().Err() != nil || request.GetBody == nil || !temporaryConnectionError(err) {
			if unsent {
				return nil, &unsentError{cause: err, network: temporaryConnectionError(err)}
			}
			return response, err
		}
		select {
		case <-request.Context().Done():
			return nil, &unsentError{cause: request.Context().Err()}
		case <-time.After(t.retryDelay):
		}
		body, bodyErr := request.GetBody()
		if bodyErr != nil {
			return nil, &unsentError{cause: errors.Join(err, bodyErr)}
		}
		current = request.Clone(request.Context())
		current.Body = body
	}
}

type errorResponseBody struct {
	io.ReadCloser
	once     sync.Once
	closeErr error
}

func (b *errorResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		_ = b.Close()
	}
	return n, err
}

func (b *errorResponseBody) Close() error {
	b.once.Do(func() { b.closeErr = b.ReadCloser.Close() })
	return b.closeErr
}

func temporaryConnectionError(err error) bool {
	var networkError net.Error
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}
