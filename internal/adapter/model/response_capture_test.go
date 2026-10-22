package model

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type failingCloseBody struct {
	io.Reader
	err    error
	closed int
}

func (b *failingCloseBody) Close() error { b.closed++; return b.err }
func TestResponseCaptureRetainsCloseFailureAndClosesOnce(t *testing.T) {
	failure := errors.New("injected response close failure")
	body := &failingCloseBody{Reader: bytes.NewReader(nil), err: failure}
	capture := &responseCapture{}
	reader := &capturedBody{ReadCloser: body, capture: capture}
	if err := reader.Close(); !errors.Is(err, failure) || !errors.Is(capture.bodyErr, failure) {
		t.Fatal(err, capture.bodyErr)
	}
	if err := reader.Close(); !errors.Is(err, failure) || body.closed != 1 {
		t.Fatal("Close repeated or swallowed error", err, body.closed)
	}
}
func TestResponseCaptureLimitsRemainderRead(t *testing.T) {
	unread := bytes.NewReader(bytes.Repeat([]byte(" "), (64<<10)+20))
	body := &failingCloseBody{Reader: unread}
	capture := &responseCapture{}
	reader := &capturedBody{ReadCloser: body, capture: capture}
	if err := reader.Close(); err == nil || capture.bodyErr == nil || unread.Len() != 19 || body.closed != 1 {
		t.Fatal("tail check was unbounded or accepted oversized remainder", err, unread.Len(), body.closed)
	}
}
