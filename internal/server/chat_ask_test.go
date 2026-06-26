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
)

// fakeAskServer 与 fakeToolCallServer 同构：title 调用发标题；首次 agent 调用发一个
// ask_user tool_call（阻塞等用户回答）；其后的 agent 调用发纯文本回答。
func fakeAskServer() *httptest.Server {
	var chatCalls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		isAgent := bytes.Contains(body, []byte(`"tools"`))
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		var lines []string
		switch {
		case !isAgent:
			lines = []string{
				`data: {"choices":[{"delta":{"content":"提问测试"}}]}`,
				`data: [DONE]`,
			}
		case atomic.AddInt32(&chatCalls, 1) == 1:
			lines = []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"ask_user","arguments":"{\"question\":\"用哪个方案?\",\"options\":[{\"label\":\"方案A\"},{\"label\":\"方案B\"}]}"}}]}}]}`,
				`data: [DONE]`,
			}
		default:
			lines = []string{
				`data: {"choices":[{"delta":{"content":"好的，按方案A执行"}}]}`,
				`data: [DONE]`,
			}
		}
		for _, l := range lines {
			_, _ = w.Write([]byte(l + "\n\n"))
			f.Flush()
		}
	}))
}

// TestChatAskUserFlow 端到端验证 ask_user 链路：模型调用 ask_user → SSE 发 ask_user_req →
// 前端 POST /api/agent/ask 回传选择 → Asker 解除阻塞 → tool_result 回传模型 → 模型给出
// 最终答复。并断言 assistant 的 tool_call turn 与 tool result 已持久化。
func TestChatAskUserFlow(t *testing.T) {
	fake := fakeAskServer()
	defer fake.Close()

	db, _ := store.Open(t.TempDir() + "/ask.db")
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
	asker := tools.NewAsker()
	// 空工具集即可：ask_user 是 agent 特判工具，不走 Engine.Execute。
	engine := tools.NewEngine(tools.NewRegistry(), gate)
	router := NewRouter(Deps{DB: db, Gate: gate, Asker: asker, Engine: engine})
	srv := httptest.NewServer(router)
	defer srv.Close()

	chatResp, err := http.Post(srv.URL+"/api/sessions/sess_1/chat",
		"application/json", strings.NewReader(`{"message":"帮我选","tools_enabled":true}`))
	if err != nil {
		t.Fatalf("chat post: %v", err)
	}
	defer chatResp.Body.Close()

	// 流式读 SSE：收到 ask_user_req 即解析 request_id 并回传用户选择「方案A」。
	sc := bufio.NewScanner(chatResp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var sawAskReq, sawToolResult, sawFinal, sawDone bool
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ask_user_req") {
			sawAskReq = true
		}
		// ask_user_req 的 data 行带 request_id 与 question；tool_call 事件只有 call_id，不会误命中。
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, `"request_id":"`) && strings.Contains(line, `"question"`) {
			id := extractJSON(line, "request_id")
			if id != "" {
				cresp, cerr := http.Post(srv.URL+"/api/agent/ask",
					"application/json",
					strings.NewReader(`{"request_id":"`+id+`","selection":"方案A","other":"","canceled":false}`))
				if cerr != nil {
					t.Fatalf("ask post: %v", cerr)
				}
				if cresp.StatusCode != http.StatusOK {
					t.Fatalf("ask status = %d", cresp.StatusCode)
				}
				cresp.Body.Close()
			}
		}
		if strings.Contains(line, "用户选择：方案A") {
			sawToolResult = true
		}
		if strings.Contains(line, "按方案A执行") {
			sawFinal = true
		}
		if strings.HasPrefix(line, "event: done") {
			sawDone = true
		}
	}

	if !sawAskReq {
		t.Error("missing ask_user_req event")
	}
	if !sawToolResult {
		t.Error("missing tool_result echoing user selection")
	}
	if !sawFinal {
		t.Error("missing final assistant answer")
	}
	if !sawDone {
		t.Error("missing done event")
	}

	// 持久化断言：ask_user 的 assistant tool-call turn 与 tool result 均入库。
	msgs, err := db.ListMessages("sess_1")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	var sawPersistedCall, sawPersistedResult bool
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.ToolCalls, `"id":"call_1"`) {
			sawPersistedCall = true
		}
		if m.Role == "tool" && m.ToolCallID == "call_1" && strings.Contains(m.Content, "方案A") {
			sawPersistedResult = true
		}
	}
	if !sawPersistedCall {
		t.Fatalf("assistant ask_user tool_call not persisted: %+v", msgs)
	}
	if !sawPersistedResult {
		t.Fatalf("tool result not persisted: %+v", msgs)
	}
}
