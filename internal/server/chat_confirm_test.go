package server

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
	"github.com/agent-rust/core/internal/tools/builtin"
)

// fakeToolCallServer serves two concurrent callers through one URL: the agent
// loop (requests carry "tools") and the concurrent title generator (requests
// carry no tools). Title-gen now overlaps the reply, so we must route by the
// request body — a global call counter would let the title request steal the
// agent's tool_call response and the confirm flow would never trigger.
func fakeToolCallServer(t *testing.T) *httptest.Server {
	var chatCalls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		isAgent := bytes.Contains(body, []byte(`"tools"`))
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		var lines []string
		switch {
		case !isAgent:
			// title-generation call: a short plain title.
			lines = []string{
				`data: {"choices":[{"delta":{"content":"测试标题"}}]}`,
				`data: [DONE]`,
			}
		case atomic.AddInt32(&chatCalls, 1) == 1:
			// first agent call: a bash tool_call.
			lines = []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo hi\"}"}}]}}]}`,
				`data: [DONE]`,
			}
		default:
			// subsequent agent call (after the tool result): plain answer.
			lines = []string{
				`data: {"choices":[{"delta":{"content":"all done"}}]}`,
				`data: [DONE]`,
			}
		}
		for _, l := range lines {
			_, _ = w.Write([]byte(l + "\n\n"))
			f.Flush()
		}
	}))
}

// TestChatConfirmFlow verifies that a dangerous tool call emits a
// confirm_req SSE event, and that resolving it via POST /api/tools/confirm
// unblocks the tool so the chat completes with a tool_result + final answer.
//
// This is the regression test for the bug where Gate.SetEmitter was never
// wired to the chat SSE stream, so confirm_req was never emitted and the
// gate blocked until the chat timeout.
//
// To avoid the data race inherent in polling httptest.ResponseRecorder.Body
// (which is not safe for concurrent read while the handler writes), this
// drives the router through a real httptest.Server and streams the SSE
// response the same way the React frontend does.
func TestChatConfirmFlow(t *testing.T) {
	fake := fakeToolCallServer(t)
	defer fake.Close()

	db, _ := store.Open(t.TempDir() + "\\confirm.db")
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	db.CreateProvider(store.Provider{
		ID: "prov_1", Name: "fake", BaseURL: fake.URL, APIKey: "k",
		ChatModel: "m", IsDefault: true, CreatedAt: now, UpdatedAt: now,
	})
	db.CreateSession(store.Session{
		ID: "sess_1", Title: "t", ProviderID: "prov_1", ToolsEnabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})

	gate := tools.NewGate()
	engine := tools.NewEngine(
		tools.NewRegistry(builtin.Bash{}), gate,
	)
	router := NewRouter(Deps{DB: db, Gate: gate, Engine: engine})
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Start the chat as a streaming POST. The handler will block inside the
	// bash tool on a confirm_req until we resolve it below.
	chatResp, err := http.Post(srv.URL+"/api/sessions/sess_1/chat",
		"application/json", strings.NewReader(`{"message":"run it","tools_enabled":true}`))
	if err != nil {
		t.Fatalf("chat post: %v", err)
	}
	defer chatResp.Body.Close()

	// Stream the SSE response line by line. When confirm_req arrives, parse
	// its request_id and resolve it; keep collecting the rest of the stream.
	sc := bufio.NewScanner(chatResp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var sawConfirmReq, sawToolResult, sawAllDone, sawDone, sawTitle bool
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: confirm_req") {
			sawConfirmReq = true
		}
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, `"request_id":"`) {
			// extract request_id and resolve it (allow once)
			id := extractJSON(line, "request_id")
			if id != "" {
				cresp, cerr := http.Post(srv.URL+"/api/tools/confirm",
					"application/json",
					strings.NewReader(`{"request_id":"`+id+`","decision":"allow","remember":"never"}`))
				if cerr != nil {
					t.Fatalf("confirm post: %v", cerr)
				}
				if cresp.StatusCode != http.StatusOK {
					t.Fatalf("confirm status = %d", cresp.StatusCode)
				}
				cresp.Body.Close()
			}
		}
		if strings.Contains(line, "tool_result") {
			sawToolResult = true
		}
		if strings.Contains(line, "all done") {
			sawAllDone = true
		}
		if strings.HasPrefix(line, "event: done") {
			sawDone = true
		}
		if strings.HasPrefix(line, "event: title") {
			sawTitle = true
		}
	}

	if !sawConfirmReq {
		t.Error("missing confirm_req event")
	}
	if !sawTitle {
		t.Error("missing title event (first turn must auto-generate a title)")
	}
	if !sawToolResult {
		t.Error("missing tool_result event")
	}
	if !sawAllDone {
		t.Error("missing final assistant answer 'all done'")
	}
	if !sawDone {
		t.Error("missing done event")
	}

	msgs, err := db.ListMessages("sess_1")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	var sawPersistedAssistantToolCall bool
	var sawPersistedToolResult bool
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.ToolCalls, `"id":"call_1"`) {
			sawPersistedAssistantToolCall = true
		}
		if m.Role == "tool" && m.ToolCallID == "call_1" && strings.Contains(m.Content, "hi") {
			sawPersistedToolResult = true
		}
	}
	if !sawPersistedAssistantToolCall {
		t.Fatalf("assistant tool_calls not persisted in messages: %+v", msgs)
	}
	if !sawPersistedToolResult {
		t.Fatalf("tool result not persisted in messages: %+v", msgs)
	}
}

// extractJSON pulls a simple "key":"value" string field out of a JSON line.
func extractJSON(line, key string) string {
	needle := `"` + key + `":"`
	i := strings.Index(line, needle)
	if i < 0 {
		return ""
	}
	start := i + len(needle)
	end := strings.Index(line[start:], `"`)
	if end < 0 {
		return ""
	}
	return line[start : start+end]
}
