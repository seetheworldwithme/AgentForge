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
