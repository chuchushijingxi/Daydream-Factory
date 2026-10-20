package httpapi

import (
	"fmt"
	"net/http"
	"time"
)

// Business handlers subscribe through their application port; this common
// transport sends no content and uses the existing revocation-aware writer.
func (h *Handler) serveInvalidationEvents(w http.ResponseWriter, r *http.Request, updates <-chan struct{}, topic string) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	send := func(body string) error {
		_ = controller.SetWriteDeadline(time.Now().Add(h.StreamWriteTimeout))
		if _, err := fmt.Fprint(w, body); err != nil {
			return err
		}
		return controller.Flush()
	}
	defer controller.SetWriteDeadline(time.Time{})
	// Subscription precedes ready: reconnect always rereads authoritative GETs.
	if send("event: "+topic+".ready\ndata: {}\n\n") != nil {
		return
	}
	ticker := time.NewTicker(h.StreamHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case _, ok := <-updates:
			if !ok || send("event: "+topic+".changed\ndata: {}\n\n") != nil {
				return
			}
		case <-ticker.C:
			if send(": keepalive\n\n") != nil {
				return
			}
		}
	}
}
