package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
)

// fakeOpenAIServer streams a couple of text deltas then [DONE].
func fakeOpenAIServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		lines := []string{
			`data: {"choices":[{"delta":{"content":"Hello "}}]}`,
			`data: {"choices":[{"delta":{"content":"world"}}]}`,
			`data: [DONE]`,
		}
		for _, l := range lines {
			_, _ = w.Write([]byte(l + "\n\n"))
			f.Flush()
		}
	}))
}

func TestChatEndToEndSSE(t *testing.T) {
	fake := fakeOpenAIServer(t)
	defer fake.Close()

	db, _ := store.Open(t.TempDir() + "\\e2e.db")
	defer db.Close()

	// seed a provider + session pointing at the fake LLM server
	now := time.Now().UTC().Format(time.RFC3339)
	db.CreateProvider(store.Provider{
		ID: "prov_1", Name: "fake", BaseURL: fake.URL, APIKey: "k",
		ChatModel: "m", IsDefault: true, CreatedAt: now, UpdatedAt: now,
	})
	db.CreateSession(store.Session{
		ID: "sess_1", Title: "t", ProviderID: "prov_1", ToolsEnabled: 0,
		CreatedAt: now, UpdatedAt: now,
	})

	gate := tools.NewGate()
	engine := tools.NewEngine(tools.NewRegistry(), gate)
	router := NewRouter(Deps{DB: db, Gate: gate, Engine: engine})

	// POST /api/sessions/sess_1/chat
	req, _ := http.NewRequest(http.MethodPost, "/api/sessions/sess_1/chat",
		strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("chat handler did not return within 5s")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: started") {
		t.Errorf("missing started event:\n%s", body)
	}
	if !strings.Contains(body, "Hello ") || !strings.Contains(body, "world") {
		t.Errorf("missing streamed deltas:\n%s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("missing done event:\n%s", body)
	}

	// message persistence: user + assistant messages stored
	msgs, _ := db.ListMessages("sess_1")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hi" {
		t.Errorf("user msg = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || !strings.Contains(msgs[1].Content, "Hello") {
		t.Errorf("assistant msg = %+v", msgs[1])
	}

	// keep bufio referenced (used conceptually by SSE parser in llm pkg)
	_ = bufio.NewReader
	_ = context.Background
}
