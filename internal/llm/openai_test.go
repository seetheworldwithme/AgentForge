package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newFakeServer emits two text deltas then a tool_call then [DONE].
func newFakeServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		lines := []string{
			`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
			`data: {"choices":[{"delta":{"content":" world"}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}]}}]}`,
			`data: [DONE]`,
		}
		for _, l := range lines {
			_, _ = w.Write([]byte(l + "\n\n"))
			f.Flush()
		}
	}))
}

func TestChatStreamParsesTextAndToolCall(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.Close()

	c := NewOpenAIClient(Config{BaseURL: srv.URL, APIKey: "test", Model: "m"})
	ch, err := c.ChatStream(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var text strings.Builder
	var toolCalls []ToolCall
	for chunk := range ch {
		text.WriteString(chunk.Text)
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
	}
	if text.String() != "Hello world" {
		t.Errorf("text = %q, want %q", text.String(), "Hello world")
	}
	if len(toolCalls) != 1 || toolCalls[0].Name != "bash" {
		t.Errorf("toolCalls = %+v, want one bash call", toolCalls)
	}
}

// TestChatStreamAccumulatesFragmentedToolCall reproduces the real-world stream
// where a tool call's arguments arrive split across many delta chunks. The
// client must accumulate them per index and emit a single complete ToolCall;
// emitting per-chunk yields partial JSON ("unexpected end of JSON input").
func TestChatStreamAccumulatesFragmentedToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		lines := []string{
			// first chunk: name + id, empty args
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_42","type":"function","function":{"name":"bash","arguments":""}}]}}]}`,
			// subsequent chunks: only argument fragments, no id/name
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"command\": \"pwd\""}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}`,
			`data: [DONE]`,
		}
		for _, l := range lines {
			_, _ = w.Write([]byte(l + "\n\n"))
			f.Flush()
		}
	}))
	defer srv.Close()

	c := NewOpenAIClient(Config{BaseURL: srv.URL, APIKey: "test", Model: "m"})
	ch, err := c.ChatStream(context.Background(), []Message{{Role: RoleUser, Content: "where am I"}}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var toolCalls []ToolCall
	for chunk := range ch {
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls = %+v, want exactly 1 (fragments must be accumulated)", toolCalls)
	}
	tc := toolCalls[0]
	if tc.ID != "call_42" || tc.Name != "bash" {
		t.Errorf("toolCall = {ID:%q Name:%q}, want {call_42, bash}", tc.ID, tc.Name)
	}
	if tc.Args != `{"command": "pwd"}` {
		t.Errorf("Args = %q, want {\"command\": \"pwd\"}", tc.Args)
	}
}

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}],"usage":{"prompt_tokens":1}}`))
	}))
	defer srv.Close()

	c := NewOpenAIClient(Config{BaseURL: srv.URL, APIKey: "test", Model: "m"})
	vecs, err := c.Embed(context.Background(), []string{"hi"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 3 {
		t.Errorf("vecs = %+v, want 1 vector of dim 3", vecs)
	}
}
