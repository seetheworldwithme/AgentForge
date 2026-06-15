package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
)

// TestOpenAIProvider_StreamsDeltas 验证 SSE 逐 token 推送到 OnDelta，
// 且 Response.Message.Content 为完整文本。
func TestOpenAIProvider_StreamsDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-fake" {
			t.Errorf("missing/invalid auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// 模拟两帧 SSE：分片输出 "你好"
		chunks := []string{`{"choices":[{"delta":{"content":"你"}}]}`,
			`{"choices":[{"delta":{"content":"好"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`}
		for _, c := range chunks {
			w.Write([]byte("data: " + c + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "sk-fake", "gpt-4o-mini")
	var collected string
	resp, err := p.ChatStream(context.Background(), Request{
		Messages: []conversation.Message{{Role: conversation.RoleUser, Content: "hi"}},
		OnDelta:  func(s string) { collected += s },
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if collected != "你好" {
		t.Errorf("deltas got %q, want 你好", collected)
	}
	if resp.Message.Content != "你好" {
		t.Errorf("message content got %q, want 你好", resp.Message.Content)
	}
	if resp.Message.Role != conversation.RoleAssistant {
		t.Errorf("role got %s, want assistant", resp.Message.Role)
	}
}

// TestOpenAIProvider_AccumulatesToolCalls 验证分片 tool_calls delta 被正确累积。
func TestOpenAIProvider_AccumulatesToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_system_info","arguments":"{\"q\":\""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"info\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		}
		for _, f := range frames {
			w.Write([]byte("data: " + f + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "sk-fake", "gpt-4o-mini")
	resp, err := p.ChatStream(context.Background(), Request{
		Tools:   []ToolDef{{Name: "get_system_info", Description: "x", Schema: []byte(`{}`)}},
		OnDelta: func(string) {}, // 走流式路径以累积 tool_calls delta
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "get_system_info" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
	if string(tc.Args) != `{"q":"info"}` {
		t.Errorf("accumulated args got %q", string(tc.Args))
	}
}

// TestOpenAIProvider_ApiError 验证 401 返回明确错误。
func TestOpenAIProvider_ApiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "Invalid API key"},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "sk-fake", "gpt-4o-mini")
	_, err := p.ChatStream(context.Background(), Request{OnDelta: func(string) {}})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

// TestOpenAIProvider_NonStreamingFallback 验证 OnDelta==nil 时走非流式。
func TestOpenAIProvider_NonStreamingFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if stream, ok := req["stream"].(bool); ok && stream {
			t.Error("non-streaming request should not set stream:true")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "完整回复"},
					"finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "sk-fake", "gpt-4o-mini")
	resp, err := p.ChatStream(context.Background(), Request{
		Messages: []conversation.Message{{Role: conversation.RoleUser, Content: "hi"}},
		// OnDelta 故意不设
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Message.Content != "完整回复" {
		t.Errorf("got %q", resp.Message.Content)
	}
}

// TestOpenAIProvider_ForwardsToolResultMessage 验证 RoleTool 消息的
// tool_call_id 和 name 被正确映射到线上格式（OpenAI 要求 tool 角色消息必须带 tool_call_id，否则 400）。
func TestOpenAIProvider_ForwardsToolResultMessage(t *testing.T) {
	var sentBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "ok"},
					"finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "sk-fake", "gpt-4o-mini")
	_, err := p.ChatStream(context.Background(), Request{
		Messages: []conversation.Message{
			{Role: conversation.RoleUser, Content: "查一下"},
			{Role: conversation.RoleAssistant, ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "get_system_info", Args: json.RawMessage(`{}`)},
			}},
			{Role: conversation.RoleTool, Content: "OS: Windows", ToolCallID: "call_1", Name: "get_system_info"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	// 出站 body 必须包含 tool_call_id（OpenAI 强制要求）。
	bodyStr := string(sentBody)
	if !strings.Contains(bodyStr, `"tool_call_id":"call_1"`) {
		t.Errorf("outbound body missing tool_call_id; got %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"role":"tool"`) {
		t.Errorf("outbound body missing tool role; got %s", bodyStr)
	}
}
