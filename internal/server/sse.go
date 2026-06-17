package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// SSEWriter wraps an http.ResponseWriter to emit Server-Sent Events. Emit is
// safe for concurrent callers: the chat handler streams the main reply and the
// concurrent title generation through the same writer, so writes are serialized.
type SSEWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
}

func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &SSEWriter{w: w, flusher: f}, true
}

// Emit writes one SSE event: "event: <event>\ndata: <json>\n\n". The mutex
// serializes the write+flush so concurrent emitters never interleave frames.
func (s *SSEWriter) Emit(event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte(fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error()))
	}
	s.mu.Lock()
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, string(b))
	s.flusher.Flush()
	s.mu.Unlock()
}
