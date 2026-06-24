package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
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

// TestChatSendsUserOnce 验证正常发送时，发给 LLM 的消息序列里本轮 user 消息只出现一次。
// 回归保护：此前 handler 把 user append 进 history，agent 又用 UserMessage 再 append 一条，
// 导致模型收到两条重复的 user 消息；现在统一由 agent.UserMessage 追加。
func TestChatSendsUserOnce(t *testing.T) {
	var gotMessages []map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, err := io.ReadAll(r.Body); err == nil {
			var body struct {
				Messages []map[string]any `json:"messages"`
			}
			if json.Unmarshal(b, &body) == nil {
				gotMessages = body.Messages
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		for _, l := range []string{
			`data: {"choices":[{"delta":{"content":"ok"}}]}`,
			`data: [DONE]`,
		} {
			_, _ = w.Write([]byte(l + "\n\n"))
			f.Flush()
		}
	}))
	defer fake.Close()

	db, _ := store.Open(t.TempDir() + "/user-once.db")
	defer db.Close()
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

	userCount := 0
	for _, m := range gotMessages {
		if m["role"] == "user" {
			userCount++
		}
	}
	if userCount != 1 {
		t.Fatalf("user messages sent to LLM = %d, want 1 (no duplicate); messages=%+v",
			userCount, gotMessages)
	}
}
