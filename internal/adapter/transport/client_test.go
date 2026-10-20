package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRequestObservationFailurePreventsAnyExternalIO(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	ctx := WithRequestObserver(context.Background(), func(raw []byte) error {
		if string(raw) != `{"messages":[]}` {
			t.Fatal("serialized request changed")
		}
		return errors.New("commit unavailable")
	})
	request, _ := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader(`{"messages":[]}`))
	_, err := NewClient().Do(request)
	var unsent interface{ RequestNotSent() bool }
	if err == nil || !errors.As(err, &unsent) || !unsent.RequestNotSent() || calls.Load() != 0 {
		t.Fatalf("observation failure escaped I/O barrier: calls=%d err=%v", calls.Load(), err)
	}
	var network interface{ NetworkInterrupted() bool }
	if !errors.As(err, &network) || network.NetworkInterrupted() {
		t.Fatal("request commit failure mislabeled as retriable network interruption")
	}
}

func TestLargeRequestObservationKeepsCompleteBodyBeforeIO(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"` + strings.Repeat("原文", 1<<19) + `"}]}`
	observed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !observed {
			t.Error("request sent before durable observer")
		}
		got, err := io.ReadAll(r.Body)
		if err != nil || string(got) != body {
			t.Error("large request body changed")
		}
	}))
	defer server.Close()
	ctx := WithRequestObserver(context.Background(), func(raw []byte) error {
		if string(raw) != body {
			return errors.New("observed body is incomplete")
		}
		observed = true
		return nil
	})
	request, _ := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader(body))
	response, err := NewClient().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestReconnectBeforeSendingProducesOneIdenticalPOST(t *testing.T) {
	closed := closedAddress(t)
	var received, dials atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || string(body) != "request-body" {
			t.Error("request changed during reconnect")
		}
		w.Write([]byte("known-result"))
	}))
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if dials.Add(1) == 1 {
			return (&net.Dialer{}).DialContext(ctx, network, closed)
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: &connectionTransport{base: transport}}
	request, _ := http.NewRequest("POST", server.URL, strings.NewReader("request-body"))
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "known-result" || dials.Load() != 2 || received.Load() != 1 {
		t.Fatalf("dials=%d POSTs=%d err=%v", dials.Load(), received.Load(), err)
	}
}

func TestUnknownAfterPOSTNeverReconnectsOrResends(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "response_headers", true: "response_body"}[partial], func(t *testing.T) {
			var received atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				io.Copy(io.Discard, r.Body)
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				if partial {
					io.WriteString(connection, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\npartial")
				}
				connection.Close()
			}))
			defer server.Close()
			transport := &http.Transport{}
			defer transport.CloseIdleConnections()
			request, _ := http.NewRequest("POST", server.URL, strings.NewReader("may-be-billed"))
			response, err := (&http.Client{Transport: &connectionTransport{base: transport}}).Do(request)
			if partial && err == nil {
				_, err = io.ReadAll(response.Body)
				response.Body.Close()
			}
			if err == nil || received.Load() != 1 {
				t.Fatalf("unknown resent or swallowed: POSTs=%d err=%v", received.Load(), err)
			}
			var unsent interface{ RequestNotSent() bool }
			if errors.As(err, &unsent) {
				t.Fatal("sent request incorrectly labeled unsent")
			}
		})
	}
}

func TestConnectionAttemptsBoundedAndCancellationStopsReconnect(t *testing.T) {
	closed := closedAddress(t)
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var dials atomic.Int32
		transport := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dials.Add(1)
			connection, err := (&net.Dialer{}).DialContext(ctx, network, closed)
			if canceled {
				cancel()
			}
			return connection, err
		}}
		request, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:1", strings.NewReader("body"))
		_, err := (&http.Client{Transport: &connectionTransport{base: transport}}).Do(request)
		cancel()
		want := int32(2)
		if canceled {
			want = 1
		}
		if err == nil || dials.Load() != want || canceled && !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled=%v dials=%d err=%v", canceled, dials.Load(), err)
		}
		var unsent interface{ RequestNotSent() bool }
		if !errors.As(err, &unsent) || !unsent.RequestNotSent() {
			t.Fatal("confirmed setup failure lacks unsent proof")
		}
	}
}

func closedAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
